package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const offAPIURL = "https://world.openfoodfacts.org/api/v2/search?fields=brands,brand_owner&page_size=1000&page=%d"

// offBarcodeRE strips trailing barcode suffixes like " (0074819091009)" from brand_owner values.
var offBarcodeRE = regexp.MustCompile(`\s*\([0-9]+\)\s*$`)

var offClient = &http.Client{Timeout: 30 * time.Second}

type BrandEntry struct {
	Brand string `json:"brand"`
	Owner string `json:"owner"`
}

type offSearchResponse struct {
	Count      int `json:"count"`
	Page       int `json:"page"`
	PageCount  int `json:"page_count"`
	PageSize   int `json:"page_size"`
	Products   []struct {
		Brands     string `json:"brands"`
		BrandOwner string `json:"brand_owner"`
	} `json:"products"`
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

// ImportBrandsFromOpenFoodFacts paginates the OFF v2 API collecting brand→owner pairs.
// Stops after maxPages pages or when no more results. Non-fatal: logs WARN on API failure.
func ImportBrandsFromOpenFoodFacts(conn *sql.DB) error {
	const maxPages = 500

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
	inserted := 0

	for page := 1; page <= maxPages; page++ {
		products, totalPages, err := fetchOFFPage(page)
		if err != nil {
			fmt.Printf("WARN: OFF API page %d: %v\n", page, err)
			break
		}
		if len(products) == 0 {
			break
		}

		for _, p := range products {
			owner := offBarcodeRE.ReplaceAllString(strings.TrimSpace(p.BrandOwner), "")
			if owner == "" {
				continue
			}
			for _, brand := range strings.Split(p.Brands, ",") {
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
				inserted++
			}
		}

		if page >= totalPages {
			break
		}
	}

	fmt.Printf("OFF API: inserted %d brand→owner pairs\n", inserted)
	return tx.Commit()
}

func fetchOFFPage(page int) ([]struct{ Brands, BrandOwner string }, int, error) {
	resp, err := offClient.Get(fmt.Sprintf(offAPIURL, page))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result offSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}

	prods := make([]struct{ Brands, BrandOwner string }, len(result.Products))
	for i, p := range result.Products {
		prods[i].Brands = p.Brands
		prods[i].BrandOwner = p.BrandOwner
	}
	return prods, result.PageCount, nil
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
