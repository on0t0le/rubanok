package importer

import (
	"compress/gzip"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// openSanctionsURL is the UA NSDC sanctions list from OpenSanctions (free tier).
// ⚠️ Confirm this URL is freely accessible before running in production.
// See: https://www.opensanctions.org/docs/bulk/
const openSanctionsURL = "https://data.opensanctions.org/datasets/latest/ua_nsdc/targets.simple.csv"

// ImportOpenSanctions downloads the OpenSanctions UA NSDC dataset and imports it.
func ImportOpenSanctions(conn *sql.DB) error {
	resp, err := http.Get(openSanctionsURL)
	if err != nil {
		return fmt.Errorf("download opensanctions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opensanctions HTTP %d", resp.StatusCode)
	}
	return parseOpenSanctionsCSV(conn, resp.Body)
}

// ImportOpenSanctionsFromPath imports from a local file (plain CSV or .gz).
func ImportOpenSanctionsFromPath(conn *sql.DB, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gr.Close()
		r = gr
	}
	return parseOpenSanctionsCSV(conn, r)
}

func parseOpenSanctionsCSV(conn *sql.DB, r io.Reader) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // allow variable column count

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

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO raw_opensanctions (id, name, aliases, country, sanctioned_ua)
		VALUES (?, ?, ?, ?, 1)
	`)
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

		id := csvField(row, idx, "id")
		name := csvField(row, idx, "name")
		if id == "" || name == "" {
			continue
		}

		aliases := pipesToJSON(csvField(row, idx, "aliases"))
		country := csvField(row, idx, "countries")

		if _, err := stmt.Exec(id, name, aliases, country); err != nil {
			return fmt.Errorf("insert %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// csvIndex maps CSV header names to column indices.
func csvIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}

// csvField returns the trimmed value at named column, or "" if not found.
func csvField(row []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// pipesToJSON converts "A|B|C" to a JSON array string: ["A","B","C"]
func pipesToJSON(s string) string {
	if s == "" {
		return "[]"
	}
	var items []string
	for _, p := range strings.Split(s, "|") {
		p = strings.TrimSpace(p)
		if p != "" {
			items = append(items, p)
		}
	}
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, item := range items {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"`)
		b.WriteString(strings.ReplaceAll(item, `"`, `\"`))
		b.WriteString(`"`)
	}
	b.WriteString("]")
	return b.String()
}
