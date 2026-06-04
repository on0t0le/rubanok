package importer

import (
	"compress/gzip"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var barcodeCSVClient = &http.Client{Timeout: 30 * time.Minute}

var barcodeSources = []struct{ label, url string }{
	{"Open Food Facts", "https://static.openfoodfacts.org/data/en.openfoodfacts.org.products.csv.gz"},
	{"Open Beauty Facts", "https://static.openbeautyfacts.org/data/en.openbeautyfacts.org.products.csv.gz"},
	{"Open Products Facts", "https://static.openproductsfacts.org/data/en.openproductsfacts.org.products.csv.gz"},
}

// ImportBarcodesFromAllSources imports barcodes from Open Food Facts,
// Open Beauty Facts, and Open Products Facts. The brand set is built once
// and reused across all sources; duplicates are skipped (first source wins).
func ImportBarcodesFromAllSources(conn *sql.DB) error {
	if err := ImportBarcodesFromJSONPath(conn, "data/barcodes.json"); err != nil {
		fmt.Printf("WARN: static barcodes: %v\n", err)
	}

	brandSet, err := buildBrandSet(conn)
	if err != nil {
		return fmt.Errorf("build brand set: %w", err)
	}
	if len(brandSet) == 0 {
		fmt.Println("barcodes: no brands in companies table, skipping")
		return nil
	}
	for _, src := range barcodeSources {
		if err := importBarcodesFromURL(conn, src.url, brandSet, src.label); err != nil {
			return fmt.Errorf("%s: %w", src.label, err)
		}
	}
	return DeriveBarcodePrefixes(conn)
}

// ImportBarcodesFromOpenFoodFacts imports barcodes from Open Food Facts only.
func ImportBarcodesFromOpenFoodFacts(conn *sql.DB) error {
	brandSet, err := buildBrandSet(conn)
	if err != nil {
		return fmt.Errorf("build brand set: %w", err)
	}
	if len(brandSet) == 0 {
		return nil
	}
	src := barcodeSources[0]
	return importBarcodesFromURL(conn, src.url, brandSet, src.label)
}

func importBarcodesFromURL(conn *sql.DB, url string, brandSet map[string]string, label string) error {
	resp, err := barcodeCSVClient.Get(url)
	if err != nil {
		return fmt.Errorf("fetch CSV: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch CSV: HTTP %d", resp.StatusCode)
	}
	return importBarcodesFromReader(conn, resp.Body, brandSet, label)
}

func importBarcodesFromReader(conn *sql.DB, r io.Reader, brandSet map[string]string, label string) error {
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
		return fmt.Errorf("%s CSV missing required columns: code=%d brands=%d", label, codeIdx, brandsIdx)
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
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				continue
			}
			return fmt.Errorf("read CSV row: %w", err)
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
				res, err := stmt.Exec(code, original)
				if err != nil {
					return fmt.Errorf("insert barcode %q: %w", code, err)
				}
				if n, _ := res.RowsAffected(); n > 0 {
					inserted++
				}
				break
			}
		}
	}

	fmt.Printf("%s: inserted %d barcode→brand pairs\n", label, inserted)
	return tx.Commit()
}

type staticBarcodeEntry struct {
	Code  string `json:"code"`
	Brand string `json:"brand"`
}

// ImportBarcodesFromJSONPath loads a static barcode→brand mapping file.
func ImportBarcodesFromJSONPath(conn *sql.DB, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var entries []staticBarcodeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO raw_barcodes (code, brand) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if e.Code == "" || e.Brand == "" {
			continue
		}
		if _, err := stmt.Exec(e.Code, e.Brand); err != nil {
			return err
		}
	}
	fmt.Printf("static barcodes: loaded %d entries from %s\n", len(entries), path)
	return tx.Commit()
}

// buildBrandSet returns a map of lowercase name → display name built from
// both the brands lists and the company names themselves, so products labelled
// with a parent company name (e.g. "PepsiCo") are matched even when no
// individual brand name appears in the OFF brands field.
func buildBrandSet(conn *sql.DB) (map[string]string, error) {
	rows, err := conn.Query(`SELECT name, brands FROM companies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var name string
		var brandsJSON *string
		if err := rows.Scan(&name, &brandsJSON); err != nil {
			return nil, err
		}
		if name != "" {
			result[strings.ToLower(name)] = name
		}
		if brandsJSON != nil && *brandsJSON != "[]" {
			var brands []string
			if err := json.Unmarshal([]byte(*brandsJSON), &brands); err == nil {
				for _, b := range brands {
					if b != "" {
						result[strings.ToLower(b)] = b
					}
				}
			}
		}
	}
	return result, rows.Err()
}
