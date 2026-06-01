package importer

import (
	"compress/gzip"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const openFoodFactsURL = "https://static.openfoodfacts.org/data/en.openfoodfacts.org.products.csv.gz"

type BrandEntry struct {
	Brand string `json:"brand"`
	Owner string `json:"owner"`
}

// ImportBrandsFromJSONPath reads a local JSON brand mapping file.
func ImportBrandsFromJSONPath(conn *sql.DB, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var entries []BrandEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return insertBrands(conn, entries, "manual")
}

// ImportBrandsFromOpenFoodFacts streams the OFF products CSV (gzip-compressed).
// Does not write the file to disk — streams directly into SQLite.
func ImportBrandsFromOpenFoodFacts(conn *sql.DB) error {
	resp, err := http.Get(openFoodFactsURL)
	if err != nil {
		return fmt.Errorf("download off: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("off HTTP %d", resp.StatusCode)
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	return parseOFFCSV(conn, gr)
}

func parseOFFCSV(conn *sql.DB, r io.Reader) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true // OFF CSV has imperfect quoting

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	idx := csvIndex(header)

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO raw_brands (brand_name, company_name, source) VALUES (?, ?, 'open_food_facts')`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	seen := make(map[string]bool)
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip malformed rows without aborting
		}

		owner := csvField(row, idx, "owner_imported")
		if owner == "" {
			continue
		}

		brandsField := csvField(row, idx, "brands")
		if brandsField == "" {
			continue
		}

		for _, brand := range strings.Split(brandsField, ",") {
			brand = strings.TrimSpace(brand)
			if brand == "" {
				continue
			}
			key := brand + "\x00" + owner
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, err := stmt.Exec(brand, owner); err != nil {
				return fmt.Errorf("insert %q: %w", brand, err)
			}
		}
	}
	return tx.Commit()
}

func insertBrands(conn *sql.DB, entries []BrandEntry, source string) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO raw_brands (brand_name, company_name, source) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.Brand == "" || e.Owner == "" {
			continue
		}
		if _, err := stmt.Exec(e.Brand, e.Owner, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}
