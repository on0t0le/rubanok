package importer

import (
	"database/sql"
	"fmt"
)

const (
	prefixMinLen   = 4
	prefixMaxLen   = 9
	prefixMinCount = 3
	prefixMinPct   = 0.95
)

// DeriveBarcodePrefixes analyses raw_barcodes and writes GS1 manufacturer
// prefixes to barcode_prefixes. A prefix (length 4–9) is written only when
// ≥95% of barcodes sharing that prefix map to the same brand and at least 3
// such barcodes exist.
func DeriveBarcodePrefixes(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT code, brand FROM raw_barcodes`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// counts[prefix][brand] = occurrences
	counts := make(map[string]map[string]int)
	for rows.Next() {
		var code, brand string
		if err := rows.Scan(&code, &brand); err != nil {
			return err
		}
		for l := prefixMinLen; l <= prefixMaxLen && l < len(code); l++ {
			p := code[:l]
			if counts[p] == nil {
				counts[p] = make(map[string]int)
			}
			counts[p][brand]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	type entry struct{ prefix, brand string }
	var reliable []entry
	for prefix, brandCounts := range counts {
		total, topCount := 0, 0
		topBrand := ""
		for brand, cnt := range brandCounts {
			total += cnt
			if cnt > topCount {
				topCount = cnt
				topBrand = brand
			}
		}
		if total >= prefixMinCount && float64(topCount)/float64(total) >= prefixMinPct {
			reliable = append(reliable, entry{prefix, topBrand})
		}
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM barcode_prefixes`); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO barcode_prefixes (prefix, brand) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range reliable {
		if _, err := stmt.Exec(e.prefix, e.brand); err != nil {
			return fmt.Errorf("insert prefix %s: %w", e.prefix, err)
		}
	}

	fmt.Printf("barcode prefixes: derived %d reliable prefixes\n", len(reliable))
	return tx.Commit()
}
