package importer

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var yaleClient = &http.Client{Timeout: 30 * time.Second}

const yaleURL = "https://som.yale.edu/story/2022/over-1000-companies-have-curtailed-operations-russia-some-remain"

// ImportKSE downloads the Yale SOM Leave Russia tracker and imports it.
func ImportKSE(conn *sql.DB) error {
	resp, err := yaleClient.Get(yaleURL)
	if err != nil {
		return fmt.Errorf("download yale: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yale HTTP %d", resp.StatusCode)
	}
	return parseYaleHTML(conn, resp.Body)
}

// ImportKSEFromPath imports from a local HTML file (used in tests).
func ImportKSEFromPath(conn *sql.DB, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return parseYaleHTML(conn, f)
}

func parseYaleHTML(conn *sql.DB, r io.Reader) error {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return fmt.Errorf("parse html: %w", err)
	}

	// Collect all tables whose headers include "Name".
	var tables []*goquery.Selection
	doc.Find("table").Each(func(_ int, s *goquery.Selection) {
		s.Find("th").Each(func(_ int, th *goquery.Selection) {
			if strings.TrimSpace(th.Text()) == "Name" {
				tables = append(tables, s)
			}
		})
	})
	if len(tables) == 0 {
		return fmt.Errorf("yale: company table not found in HTML")
	}

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

	today := time.Now().Format("2006-01-02")
	var insertErr error
	for _, table := range tables {
		table.Find("tbody tr").Each(func(_ int, row *goquery.Selection) {
			if insertErr != nil {
				return
			}
			cells := row.Find("td")
			name := strings.TrimSpace(cells.Eq(0).Text())
			if name == "" {
				return
			}
			action := strings.TrimSpace(cells.Eq(1).Text())
			status := mapYaleStatus(action)
			if _, execErr := stmt.Exec(name, status, today); execErr != nil {
				insertErr = fmt.Errorf("insert %s: %w", name, execErr)
			}
		})
		if insertErr != nil {
			break
		}
	}
	if insertErr != nil {
		return insertErr
	}
	return tx.Commit()
}

func mapYaleStatus(action string) string {
	lower := strings.ToLower(action)
	for _, kw := range []string{"exit", "left", "withdraw", "depart", "divest", "sold", "liquidat", "shut down", "clos"} {
		if strings.Contains(lower, kw) {
			return "Exited"
		}
	}
	for _, kw := range []string{"suspend", "pause", "halt", "stop", "discontinu", "freeze"} {
		if strings.Contains(lower, kw) {
			return "Suspended"
		}
	}
	for _, kw := range []string{"reduc", "limit", "curtail", "scale back", "wind down", "partial"} {
		if strings.Contains(lower, kw) {
			return "Reduced Operations"
		}
	}
	for _, kw := range []string{"continu", "operat", "remain", "stay", "expand", "increas"} {
		if strings.Contains(lower, kw) {
			return "Operating"
		}
	}
	return "Unknown"
}
