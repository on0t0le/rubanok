package merger

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"pipeline/internal/importer"
	"pipeline/internal/normalize"
)

// Override forces a KSE company name to match a specific OpenSanctions entity,
// bypassing fuzzy scoring.
type Override struct {
	KSEName           string `json:"kse_name"`
	OpenSanctionsName string `json:"opensanctions_name"`
}

type osEntity struct {
	id           string
	name         string
	aliases      string
	country      string
	sanctionedUA int
	normName     string
}

type kseRow struct {
	name        string
	status      string
	industry    string
	lastUpdated string
}

var consumerIndustries = map[string]bool{
	"Consumer Staples":        true,
	"Consumer Discretionary":  true,
}

func isConsumer(industry string) bool {
	if industry == "" {
		return true
	}
	return consumerIndustries[industry]
}

var fetchPersonNames = importer.FetchPersonNames

type brandPair struct {
	brand string
	owner string
}

// Merge reads raw tables, fuzzy-matches entities, writes to companies table.
// overrides may be nil.
func Merge(conn *sql.DB, overrides []Override) error {
	osList, err := loadOpenSanctions(conn)
	if err != nil {
		return fmt.Errorf("load opensanctions: %w", err)
	}
	kseList, err := loadKSE(conn)
	if err != nil {
		return fmt.Errorf("load kse: %w", err)
	}
	brandPairs, err := loadBrands(conn)
	if err != nil {
		return fmt.Errorf("load brands: %w", err)
	}

	// Pre-compute sorted trigram sets for all company norms once (avoids re-hashing in hot loops).
	type normTrigrams struct {
		norm   string
		sorted string
		tgrams map[string]bool
	}
	osTrigs := make([]normTrigrams, len(osList))
	for i, e := range osList {
		s := normalize.SortTokens(e.normName)
		osTrigs[i] = normTrigrams{norm: e.normName, sorted: s, tgrams: normalize.Trigrams(s)}
	}

	// Collect all company norm names (OS + KSE) for fuzzy brand resolution.
	allNorms := make([]string, 0, len(osList)+len(kseList))
	for _, e := range osList {
		allNorms = append(allNorms, e.normName)
	}
	for _, k := range kseList {
		allNorms = append(allNorms, normalize.Company(k.name))
	}
	brands := resolveBrands(brandPairs, allNorms)

	// Detect person-brands via Wikidata and exclude them from output.
	kseNames := make([]string, len(kseList))
	for i, k := range kseList {
		kseNames[i] = k.name
	}
	excludeSet, err := fetchPersonNames(kseNames)
	if err != nil {
		fmt.Printf("WARN: person detection failed: %v — no persons excluded\n", err)
		excludeSet = map[string]bool{}
	}

	// build override map: normalized KSE name → normalized OS name
	overrideMap := make(map[string]string, len(overrides))
	for _, o := range overrides {
		overrideMap[normalize.Company(o.KSEName)] = normalize.Company(o.OpenSanctionsName)
	}

	// index OS entities by normalized name for O(1) override lookup
	osNormIndex := make(map[string]*osEntity, len(osList))
	for i := range osList {
		osNormIndex[osList[i].normName] = &osList[i]
	}

	// match KSE rows → OS entities
	kseMatched := make(map[string]*kseRow) // os.normName → kse row
	var kseOnly []*kseRow

	for i := range kseList {
		k := &kseList[i]
		normKSE := normalize.Company(k.name)

		// manual override takes priority
		if targetNorm, ok := overrideMap[normKSE]; ok {
			if e, found := osNormIndex[targetNorm]; found {
				kseMatched[e.normName] = k
			} else {
				fmt.Printf("WARN: override target for KSE %q not found in OpenSanctions data\n", k.name)
				kseOnly = append(kseOnly, k)
			}
			continue
		}

		// fuzzy match against all OS entities (pre-computed trigrams avoid re-hashing)
		kseSorted := normalize.SortTokens(normKSE)
		kseTgrams := normalize.Trigrams(kseSorted)
		bestScore := 0
		var bestEntity *osEntity
		for j := range osTrigs {
			score := normalize.SimilarityFromSets(kseTgrams, osTrigs[j].tgrams)
			if score > bestScore {
				bestScore = score
				bestEntity = &osList[j]
			}
		}

		switch {
		case bestScore >= 85 && bestEntity != nil:
			if prev, exists := kseMatched[bestEntity.normName]; exists {
				fmt.Printf("WARN: KSE collision on OS %q — %q overwrites %q\n",
					bestEntity.name, k.name, prev.name)
			}
			kseMatched[bestEntity.normName] = k
		case bestScore >= 60 && bestEntity != nil:
			fmt.Printf("REVIEW: KSE %q ↔ OS %q (score %d, below threshold)\n",
				k.name, bestEntity.name, bestScore)
			kseOnly = append(kseOnly, k)
		default:
			kseOnly = append(kseOnly, k)
		}
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO companies
			(id, name, aliases, russia_status, sanctioned_ua, decree, sanction_date, brands, sources)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	slugSeen := make(map[string]string) // slug → company name

	// write OS entities (with or without a KSE match)
	for _, e := range osList {
		if excludeSet[e.name] {
			continue
		}
		russiaStatus := "Unknown"
		sourcesSlice := []string{"OpenSanctions"}

		if k, matched := kseMatched[e.normName]; matched {
			russiaStatus = k.status
			sourcesSlice = append(sourcesSlice, "KSE")
		}

		brandsSlice := brands[e.normName]
		if brandsSlice == nil {
			brandsSlice = []string{}
		}

		id := slugify(e.name)
		if id == "" {
			id = e.id
		}
		if prev, exists := slugSeen[id]; exists {
			fmt.Printf("WARN: slug collision %q — %q will overwrite %q\n", id, e.name, prev)
		}
		slugSeen[id] = e.name

		if _, err := stmt.Exec(
			id, e.name, e.aliases,
			russiaStatus, e.sanctionedUA,
			nil, nil,
			marshalJSON(brandsSlice), marshalJSON(sourcesSlice),
		); err != nil {
			return fmt.Errorf("insert %s: %w", e.name, err)
		}
	}

	// write KSE-only companies (no OS match found)
	for i, k := range kseOnly {
		if excludeSet[k.name] {
			continue
		}
		normK := normalize.Company(k.name)
		brandsSlice := brands[normK]
		if brandsSlice == nil {
			brandsSlice = []string{}
		}

		id := slugify(k.name)
		if id == "" {
			id = fmt.Sprintf("kse-%d", i)
		}
		if prev, exists := slugSeen[id]; exists {
			fmt.Printf("WARN: slug collision %q — %q will overwrite %q\n", id, k.name, prev)
		}
		slugSeen[id] = k.name

		if _, err := stmt.Exec(
			id, k.name, "[]",
			k.status, 0,
			nil, nil,
			marshalJSON(brandsSlice), marshalJSON([]string{"KSE"}),
		); err != nil {
			return fmt.Errorf("insert kse-only %s: %w", k.name, err)
		}
	}

	return tx.Commit()
}

// resolveBrands fuzzy-matches brand owner names to company normalized names
// and returns a map of companyNorm → []brand suitable for the merger lookup.
// Owners that normalize to "" or score below 70 are skipped.
func resolveBrands(pairs []brandPair, companyNorms []string) map[string][]string {
	normSet := make(map[string]bool, len(companyNorms))
	for _, n := range companyNorms {
		normSet[n] = true
	}

	// Pre-compute trigrams for the static company norm list once.
	type cnEntry struct {
		norm   string
		tgrams map[string]bool
	}
	cnTrigs := make([]cnEntry, len(companyNorms))
	for i, cn := range companyNorms {
		cnTrigs[i] = cnEntry{norm: cn, tgrams: normalize.Trigrams(normalize.SortTokens(cn))}
	}

	ownerToCompany := make(map[string]string)
	for _, p := range pairs {
		normOwner := normalize.Company(p.owner)
		if normOwner == "" {
			continue
		}
		if _, already := ownerToCompany[normOwner]; already {
			continue
		}
		if normSet[normOwner] {
			ownerToCompany[normOwner] = normOwner
			continue
		}
		// prefix match: "mondelez" matches "mondelez nabisco"
		// minimum 5 chars to avoid single-letter false positives (e.g. "p" matching "procter gamble")
		if len(normOwner) >= 5 {
			prefixMatch := ""
			for _, cn := range companyNorms {
				if strings.HasPrefix(cn, normOwner) || strings.HasPrefix(normOwner, cn) {
					if len(cn) > len(prefixMatch) {
						prefixMatch = cn
					}
				}
			}
			if prefixMatch != "" {
				ownerToCompany[normOwner] = prefixMatch
				continue
			}
		}
		ownerSorted := normalize.SortTokens(normOwner)
		ownerTgrams := normalize.Trigrams(ownerSorted)
		bestScore := 0
		bestNorm := ""
		for _, ce := range cnTrigs {
			score := normalize.SimilarityFromSets(ownerTgrams, ce.tgrams)
			if score > bestScore {
				bestScore = score
				bestNorm = ce.norm
			}
		}
		if bestScore >= 70 {
			ownerToCompany[normOwner] = bestNorm
		} else {
			fmt.Printf("WARN: no brand owner match for %q (best score %d)\n", p.owner, bestScore)
		}
	}

	seen := make(map[string]map[string]bool)
	result := make(map[string][]string)
	for _, p := range pairs {
		normOwner := normalize.Company(p.owner)
		if normOwner == "" {
			continue
		}
		companyNorm, ok := ownerToCompany[normOwner]
		if !ok {
			continue
		}
		if seen[companyNorm] == nil {
			seen[companyNorm] = make(map[string]bool)
		}
		if !seen[companyNorm][p.brand] {
			seen[companyNorm][p.brand] = true
			result[companyNorm] = append(result[companyNorm], p.brand)
		}
	}
	return result
}

func loadOpenSanctions(conn *sql.DB) ([]osEntity, error) {
	rows, err := conn.Query(`
		SELECT id, name,
		       COALESCE(aliases, '[]'),
		       COALESCE(country, ''),
		       sanctioned_ua
		FROM raw_opensanctions
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []osEntity
	for rows.Next() {
		var e osEntity
		if err := rows.Scan(&e.id, &e.name, &e.aliases, &e.country, &e.sanctionedUA); err != nil {
			return nil, err
		}
		e.normName = normalize.Company(e.name)
		result = append(result, e)
	}
	return result, rows.Err()
}

func loadKSE(conn *sql.DB) ([]kseRow, error) {
	rows, err := conn.Query(`
		SELECT company_name, COALESCE(status, 'Unknown'), COALESCE(industry, ''), COALESCE(last_updated, '')
		FROM raw_kse
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []kseRow
	for rows.Next() {
		var k kseRow
		if err := rows.Scan(&k.name, &k.status, &k.industry, &k.lastUpdated); err != nil {
			return nil, err
		}
		if isConsumer(k.industry) {
			result = append(result, k)
		}
	}
	return result, rows.Err()
}

// loadBrands returns all raw brand→owner pairs from raw_brands.
func loadBrands(conn *sql.DB) ([]brandPair, error) {
	rows, err := conn.Query(`SELECT brand_name, company_name FROM raw_brands`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []brandPair
	for rows.Next() {
		var bp brandPair
		if err := rows.Scan(&bp.brand, &bp.owner); err != nil {
			return nil, err
		}
		result = append(result, bp)
	}
	return result, rows.Err()
}

type wsrwRow struct {
	slug   string
	name   string
	status string
	brands []string
}

// MergeWSRW matches raw_wsrw companies against existing companies table entries.
// Matches update the sources list; unmatched entries are inserted as new companies.
func MergeWSRW(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT slug, name, status, COALESCE(brands, '') FROM raw_wsrw`)
	if err != nil {
		return fmt.Errorf("load raw_wsrw: %w", err)
	}
	defer rows.Close()
	var wsrwList []wsrwRow
	for rows.Next() {
		var w wsrwRow
		var brandsCSV string
		if err := rows.Scan(&w.slug, &w.name, &w.status, &brandsCSV); err != nil {
			return err
		}
		if brandsCSV != "" {
			for _, b := range strings.Split(brandsCSV, ",") {
				b = strings.TrimSpace(b)
				if b != "" {
					w.brands = append(w.brands, b)
				}
			}
		}
		wsrwList = append(wsrwList, w)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Load existing companies for fuzzy matching.
	type existingCompany struct {
		id       string
		name     string
		normName string
		sources  []string
		brands   []string
	}
	compRows, err := conn.Query(`SELECT id, name, COALESCE(sources, '[]'), COALESCE(brands, '[]') FROM companies`)
	if err != nil {
		return fmt.Errorf("load companies: %w", err)
	}
	defer compRows.Close()
	var existing []existingCompany
	for compRows.Next() {
		var ec existingCompany
		var sourcesJSON, brandsJSON string
		if err := compRows.Scan(&ec.id, &ec.name, &sourcesJSON, &brandsJSON); err != nil {
			return err
		}
		json.Unmarshal([]byte(sourcesJSON), &ec.sources)
		json.Unmarshal([]byte(brandsJSON), &ec.brands)
		ec.normName = normalize.Company(ec.name)
		existing = append(existing, ec)
	}
	if err := compRows.Err(); err != nil {
		return err
	}

	// Pre-compute trigrams for existing company names.
	type normEntry struct {
		idx    int
		sorted string
		tgrams map[string]bool
	}
	normEntries := make([]normEntry, len(existing))
	for i, ec := range existing {
		s := normalize.SortTokens(ec.normName)
		normEntries[i] = normEntry{idx: i, sorted: s, tgrams: normalize.Trigrams(s)}
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	updateStmt, err := tx.Prepare(`UPDATE companies SET russia_status = ?, sources = ?, brands = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	insertStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO companies (id, name, aliases, russia_status, sanctioned_ua, brands, sources)
		VALUES (?, ?, '[]', ?, 0, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	updated, inserted := 0, 0
	slugSeen := make(map[string]bool)
	for _, w := range wsrwList {
		normW := normalize.Company(w.name)
		wSorted := normalize.SortTokens(normW)
		wTgrams := normalize.Trigrams(wSorted)

		bestScore, bestIdx := 0, -1
		for _, ne := range normEntries {
			score := normalize.SimilarityFromSets(wTgrams, ne.tgrams)
			if score > bestScore {
				bestScore = score
				bestIdx = ne.idx
			}
		}

		if bestScore >= 70 && bestIdx >= 0 {
			// Update existing company: set/update russia_status and add WSRW to sources.
			ec := existing[bestIdx]
			sources := ec.sources
			if !containsStr(sources, "WSRW") {
				sources = append(sources, "WSRW")
			}
			// Merge brands: add WSRW brands not already in the list.
			brands := ec.brands
			for _, b := range w.brands {
				if !containsStr(brands, b) {
					brands = append(brands, b)
				}
			}
			if _, err := updateStmt.Exec(w.status, marshalJSON(sources), marshalJSON(brands), ec.id); err != nil {
				return fmt.Errorf("update %s: %w", ec.id, err)
			}
			updated++
		} else {
			// Insert as new company.
			id := w.slug
			if slugSeen[id] {
				id = fmt.Sprintf("wsrw-%s", w.slug)
			}
			slugSeen[id] = true
			brandsJSON := marshalJSON(w.brands)
			if w.brands == nil {
				brandsJSON = "[]"
			}
			if _, err := insertStmt.Exec(id, w.name, w.status, brandsJSON, marshalJSON([]string{"WSRW"})); err != nil {
				return fmt.Errorf("insert wsrw %s: %w", w.slug, err)
			}
			inserted++
		}
	}

	fmt.Printf("WSRW merge: %d updated, %d inserted\n", updated, inserted)
	return tx.Commit()
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastWasSep := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastWasSep = false
		} else if !lastWasSep && b.Len() > 0 {
			b.WriteRune('-')
			lastWasSep = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func marshalJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
