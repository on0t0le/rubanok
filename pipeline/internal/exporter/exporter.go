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

// Output is the top-level structure of companies.json.gz.
type Output struct {
	Companies []Company `json:"companies"`
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

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outputDir, err)
	}

	if err := writeCompaniesGZ(companies, filepath.Join(outputDir, "companies.json.gz")); err != nil {
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

func writeCompaniesGZ(companies []Company, path string) error {
	data, err := json.Marshal(Output{Companies: companies})
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
		Version:     now.Format("2006-01-02"),
		Records:     count,
		GeneratedAt: now.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
