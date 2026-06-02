package merger

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

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

	// Collect all company norm names (OS + KSE) for fuzzy brand resolution.
	allNorms := make([]string, 0, len(osList)+len(kseList))
	for _, e := range osList {
		allNorms = append(allNorms, e.normName)
	}
	for _, k := range kseList {
		allNorms = append(allNorms, normalize.Company(k.name))
	}
	brands := resolveBrands(brandPairs, allNorms)

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

		// fuzzy match against all OS entities
		bestScore := 0
		var bestEntity *osEntity
		for j := range osList {
			score := normalize.TokenSortRatio(normKSE, osList[j].normName)
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
		bestScore := 0
		bestNorm := ""
		for _, cn := range companyNorms {
			score := normalize.TokenSortRatio(normOwner, cn)
			if score > bestScore {
				bestScore = score
				bestNorm = cn
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
