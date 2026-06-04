package exporter

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Company is one record in the output JSON.
type Company struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	RussiaStatus string   `json:"russia_status"`
	SanctionedUA bool     `json:"sanctioned_ua"`
	Decree       *string  `json:"decree"`
	SanctionDate *string  `json:"sanction_date"`
	Brands       []string `json:"brands"`
	Sources      []string `json:"sources"`
}

// BarcodeEntry maps a product barcode to a brand name in the output dataset.
type BarcodeEntry struct {
	Code  string `json:"code"`
	Brand string `json:"brand"`
}

// BarcodePrefixEntry maps a GS1 manufacturer prefix to a brand name.
type BarcodePrefixEntry struct {
	Prefix string `json:"prefix"`
	Brand  string `json:"brand"`
}

// Output is the top-level structure of companies.json.gz.
type Output struct {
	Companies       []Company            `json:"companies"`
	Barcodes        []BarcodeEntry       `json:"barcodes"`
	BarcodePrefixes []BarcodePrefixEntry `json:"barcode_prefixes"`
}

// Version is the structure of version.json.
type Version struct {
	Version     string `json:"version"`
	Records     int    `json:"records"`
	GeneratedAt string `json:"generated_at"`
}

// Export writes companies.json.gz and version.json to outputDir.
func Export(conn *sql.DB, outputDir string) error {
	companies, err := loadCompanies(conn)
	if err != nil {
		return fmt.Errorf("load companies: %w", err)
	}
	barcodes, err := loadBarcodes(conn)
	if err != nil {
		return fmt.Errorf("load barcodes: %w", err)
	}
	prefixes, err := loadBarcodePrefixes(conn)
	if err != nil {
		return fmt.Errorf("load barcode prefixes: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outputDir, err)
	}

	if err := writeCompaniesGZ(companies, barcodes, prefixes, filepath.Join(outputDir, "companies.json.gz")); err != nil {
		return fmt.Errorf("write companies.json.gz: %w", err)
	}
	if err := writeVersion(len(companies), filepath.Join(outputDir, "version.json")); err != nil {
		return fmt.Errorf("write version.json: %w", err)
	}
	return nil
}

func loadCompanies(conn *sql.DB) ([]Company, error) {
	rows, err := conn.Query(`
		SELECT id, name,
		       COALESCE(aliases, '[]'),
		       COALESCE(russia_status, 'Unknown'),
		       sanctioned_ua,
		       decree, sanction_date,
		       COALESCE(brands, '[]'),
		       COALESCE(sources, '[]')
		FROM companies
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Company
	for rows.Next() {
		var c Company
		var aliasesJSON, brandsJSON, sourcesJSON string
		var sanctionedUA int
		var decree, sanctionDate sql.NullString

		if err := rows.Scan(
			&c.ID, &c.Name,
			&aliasesJSON, &c.RussiaStatus, &sanctionedUA,
			&decree, &sanctionDate,
			&brandsJSON, &sourcesJSON,
		); err != nil {
			return nil, err
		}

		c.SanctionedUA = sanctionedUA == 1
		if decree.Valid && decree.String != "" {
			c.Decree = &decree.String
		}
		if sanctionDate.Valid && sanctionDate.String != "" {
			c.SanctionDate = &sanctionDate.String
		}

		// parse JSON arrays stored as strings in SQLite
		if err := json.Unmarshal([]byte(aliasesJSON), &c.Aliases); err != nil {
			c.Aliases = []string{}
		}
		if err := json.Unmarshal([]byte(brandsJSON), &c.Brands); err != nil {
			c.Brands = []string{}
		}
		if err := json.Unmarshal([]byte(sourcesJSON), &c.Sources); err != nil {
			c.Sources = []string{}
		}

		result = append(result, c)
	}
	return result, rows.Err()
}

func loadBarcodes(conn *sql.DB) ([]BarcodeEntry, error) {
	rows, err := conn.Query(`SELECT code, brand FROM raw_barcodes ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BarcodeEntry
	for rows.Next() {
		var e BarcodeEntry
		if err := rows.Scan(&e.Code, &e.Brand); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func loadBarcodePrefixes(conn *sql.DB) ([]BarcodePrefixEntry, error) {
	rows, err := conn.Query(`SELECT prefix, brand FROM barcode_prefixes ORDER BY prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []BarcodePrefixEntry
	for rows.Next() {
		var e BarcodePrefixEntry
		if err := rows.Scan(&e.Prefix, &e.Brand); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func writeCompaniesGZ(companies []Company, barcodes []BarcodeEntry, prefixes []BarcodePrefixEntry, path string) error {
	out := Output{Companies: companies, Barcodes: barcodes, BarcodePrefixes: prefixes}
	if out.Barcodes == nil {
		out.Barcodes = []BarcodeEntry{}
	}
	if out.BarcodePrefixes == nil {
		out.BarcodePrefixes = []BarcodePrefixEntry{}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	if _, err := gw.Write(data); err != nil {
		return err
	}
	return gw.Close()
}

func writeVersion(count int, path string) error {
	now := time.Now().UTC()
	v := Version{
		Version:     now.Format(time.RFC3339),
		Records:     count,
		GeneratedAt: now.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
