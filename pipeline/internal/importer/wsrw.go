package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const wsrwBaseURL = "https://who-support-rus-war.com/wp-json/wp/v2/companies"

var wsrwClient = &http.Client{Timeout: 30 * time.Second}

var wsrwStatusMap = map[int]string{
	25: "Operating",
	26: "Bypassing",
	27: "Tacking",
}

type wsrwCompany struct {
	Slug  string `json:"slug"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	StatusIDs []int `json:"companies___status"`
	ACF       struct {
		Brands string `json:"brands"`
	} `json:"acf"`
}

// ImportFromWSRW fetches all companies from who-support-rus-war.com and stores them in raw_wsrw.
func ImportFromWSRW(conn *sql.DB) error {
	return importFromWSRWURL(conn, wsrwBaseURL)
}

func importFromWSRWURL(conn *sql.DB, baseURL string) error {
	companies, err := fetchAllWSRWCompanies(baseURL)
	if err != nil {
		return fmt.Errorf("fetch WSRW: %w", err)
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM raw_wsrw`); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO raw_wsrw (slug, name, status, brands) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range companies {
		name := cleanWSRWHTML(c.Title.Rendered)
		status := resolveWSRWStatus(c.StatusIDs)
		brands := strings.Join(extractWSRWBrands(c.ACF.Brands), ",")
		if _, err := stmt.Exec(c.Slug, name, status, brands); err != nil {
			return fmt.Errorf("insert %s: %w", c.Slug, err)
		}
	}

	fmt.Printf("WSRW: imported %d companies\n", len(companies))
	return tx.Commit()
}

func fetchAllWSRWCompanies(baseURL string) ([]wsrwCompany, error) {
	var all []wsrwCompany
	for page := 1; ; page++ {
		batch, totalPages, err := fetchWSRWPage(baseURL, page)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if page >= totalPages {
			break
		}
	}
	return all, nil
}

func fetchWSRWPage(baseURL string, page int) ([]wsrwCompany, int, error) {
	url := fmt.Sprintf("%s?per_page=100&page=%d", baseURL, page)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("page %d: %w", page, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; rubanok-pipeline/1.0; +https://github.com/on0t0le/rubanok)")
	resp, err := wsrwClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("page %d: %w", page, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("page %d: HTTP %d", page, resp.StatusCode)
	}
	totalPages := parseWSRWInt(resp.Header.Get("X-WP-TotalPages"))
	var batch []wsrwCompany
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, 0, fmt.Errorf("page %d: decode: %w", page, err)
	}
	return batch, totalPages, nil
}

func resolveWSRWStatus(ids []int) string {
	for _, id := range ids {
		if s, ok := wsrwStatusMap[id]; ok {
			return s
		}
	}
	return "Unknown"
}

var latinSeqRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9\-&.]*(?:\s+[A-Za-z][A-Za-z0-9\-&.]*)*`)

// extractWSRWBrands splits a comma-separated brands string and extracts
// Latin-script brand names. Mixed Cyrillic-English entries (e.g. "шампуні Schwarzkopf")
// are reduced to their Latin-script portion.
func extractWSRWBrands(raw string) []string {
	if raw == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if isAllLatin(part) {
			result = append(result, part)
		} else {
			for _, seq := range latinSeqRe.FindAllString(part, -1) {
				seq = strings.TrimSpace(seq)
				if len([]rune(seq)) >= 2 {
					result = append(result, seq)
				}
			}
		}
	}
	return result
}

func isAllLatin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func cleanWSRWHTML(s string) string {
	s = strings.ReplaceAll(s, "&#038;", "&")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#8230;", "...")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(s)
}

func parseWSRWInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	if n < 1 {
		return 1
	}
	return n
}
