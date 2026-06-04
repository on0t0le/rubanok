package importer

import (
	"compress/gzip"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const offProductsCSVURL = "https://static.openfoodfacts.org/data/en.openfoodfacts.org.products.csv.gz"

// ImportBarcodesFromOpenFoodFacts downloads the OFF products TSV and inserts
// barcode→brand pairs for brands present in the companies table.
func ImportBarcodesFromOpenFoodFacts(conn *sql.DB) error {
	resp, err := http.Get(offProductsCSVURL)
	if err != nil {
		return fmt.Errorf("fetch OFF products CSV: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch OFF products CSV: HTTP %d", resp.StatusCode)
	}
	return importBarcodesFromReader(conn, resp.Body)
}

// importBarcodesFromReader reads a gzip-compressed TSV from r.
// The TSV must have a header row with at least "code" and "brands" columns.
// "brands" is a comma-separated list of brand names per row.
func importBarcodesFromReader(conn *sql.DB, r io.Reader) error {
	brandSet, err := buildBrandSet(conn)
	if err != nil {
		return fmt.Errorf("build brand set: %w", err)
	}
	if len(brandSet) == 0 {
		fmt.Println("barcodes: no brands in companies table, skipping")
		return nil
	}

	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	cr := csv.NewReader(gr)
	cr.Comma = '\t'
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read CSV header: %w", err)
	}
	codeIdx, brandsIdx := -1, -1
	for i, h := range header {
		switch strings.TrimSpace(h) {
		case "code":
			codeIdx = i
		case "brands":
			brandsIdx = i
		}
	}
	if codeIdx < 0 || brandsIdx < 0 {
		return fmt.Errorf("OFF CSV missing required columns: code=%d brands=%d", codeIdx, brandsIdx)
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO raw_barcodes (code, brand) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	inserted := 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip malformed rows
		}
		if codeIdx >= len(row) || brandsIdx >= len(row) {
			continue
		}
		code := strings.TrimSpace(row[codeIdx])
		if code == "" {
			continue
		}
		for _, brand := range strings.Split(row[brandsIdx], ",") {
			brand = strings.TrimSpace(brand)
			if brand == "" {
				continue
			}
			if original, ok := brandSet[strings.ToLower(brand)]; ok {
				if _, err := stmt.Exec(code, original); err != nil {
					return fmt.Errorf("insert barcode %q: %w", code, err)
				}
				inserted++
				break // one brand match per barcode row is enough
			}
		}
	}

	fmt.Printf("OFF barcodes: inserted %d barcode→brand pairs\n", inserted)
	return tx.Commit()
}

// buildBrandSet returns a map of lowercase brand name → original brand name
// built from the brands JSON arrays stored in the companies table.
func buildBrandSet(conn *sql.DB) (map[string]string, error) {
	rows, err := conn.Query(`SELECT brands FROM companies WHERE brands IS NOT NULL AND brands != '[]'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var brandsJSON string
		if err := rows.Scan(&brandsJSON); err != nil {
			return nil, err
		}
		var brands []string
		if err := json.Unmarshal([]byte(brandsJSON), &brands); err != nil {
			continue
		}
		for _, b := range brands {
			if b != "" {
				result[strings.ToLower(b)] = b
			}
		}
	}
	return result, rows.Err()
}
