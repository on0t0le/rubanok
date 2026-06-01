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
	lastUpdated string
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
	brands, err := loadBrands(conn)
	if err != nil {
		return fmt.Errorf("load brands: %w", err)
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

		if _, err := stmt.Exec(
			slugify(e.name), e.name, e.aliases,
			russiaStatus, e.sanctionedUA,
			nil, nil,
			marshalJSON(brandsSlice), marshalJSON(sourcesSlice),
		); err != nil {
			return fmt.Errorf("insert %s: %w", e.name, err)
		}
	}

	// write KSE-only companies (no OS match found)
	for _, k := range kseOnly {
		normK := normalize.Company(k.name)
		brandsSlice := brands[normK]
		if brandsSlice == nil {
			brandsSlice = []string{}
		}
		if _, err := stmt.Exec(
			slugify(k.name), k.name, "[]",
			k.status, 0,
			nil, nil,
			marshalJSON(brandsSlice), marshalJSON([]string{"KSE"}),
		); err != nil {
			return fmt.Errorf("insert kse-only %s: %w", k.name, err)
		}
	}

	return tx.Commit()
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
		SELECT company_name, COALESCE(status, 'Unknown'), COALESCE(last_updated, '')
		FROM raw_kse
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []kseRow
	for rows.Next() {
		var k kseRow
		if err := rows.Scan(&k.name, &k.status, &k.lastUpdated); err != nil {
			return nil, err
		}
		result = append(result, k)
	}
	return result, rows.Err()
}

// loadBrands returns normalized company name → []brand names
func loadBrands(conn *sql.DB) (map[string][]string, error) {
	rows, err := conn.Query(`SELECT brand_name, company_name FROM raw_brands`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string][]string)
	for rows.Next() {
		var brand, company string
		if err := rows.Scan(&brand, &company); err != nil {
			return nil, err
		}
		key := normalize.Company(company)
		m[key] = append(m[key], brand)
	}
	return m, rows.Err()
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			chars := []rune(b.String())
			if chars[len(chars)-1] != '-' {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func marshalJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
