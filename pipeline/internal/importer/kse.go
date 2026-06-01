package importer

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// kseURL is the KSE Leave Russia tracker CSV download.
// ⚠️ Confirm this URL before running in production — check https://leave-russia.org
const kseURL = "https://leave-russia.org/export/csv"

// ImportKSE downloads the KSE Leave Russia dataset and imports it.
func ImportKSE(conn *sql.DB) error {
	resp, err := http.Get(kseURL)
	if err != nil {
		return fmt.Errorf("download kse: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kse HTTP %d", resp.StatusCode)
	}
	return parseKSECSV(conn, resp.Body)
}

// ImportKSEFromPath imports from a local CSV file (used in tests).
func ImportKSEFromPath(conn *sql.DB, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return parseKSECSV(conn, f)
}

func parseKSECSV(conn *sql.DB, r io.Reader) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	fmt.Printf("KSE CSV headers: %v\n", header)
	idx := csvIndex(header)

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO raw_kse (company_name, status, last_updated) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read row: %w", err)
		}

		name := csvField(row, idx, "Company")
		if name == "" {
			continue
		}
		status := normalizeKSEStatus(csvField(row, idx, "Status"))
		updated := csvField(row, idx, "LastUpdated")

		if _, err := stmt.Exec(name, status, updated); err != nil {
			return fmt.Errorf("insert %s: %w", name, err)
		}
	}
	return tx.Commit()
}

func normalizeKSEStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "exited", "left":
		return "Exited"
	case "suspended", "paused":
		return "Suspended"
	case "reduced", "reduced operations":
		return "Reduced Operations"
	case "operating", "continues":
		return "Operating"
	default:
		return "Unknown"
	}
}
