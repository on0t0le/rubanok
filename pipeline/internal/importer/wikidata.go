package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var wikidataClient = &http.Client{Timeout: 60 * time.Second}
var wikidataEndpoint = "https://query.wikidata.org/sparql"

const wikidataSPARQL = `
SELECT DISTINCT ?brandLabel ?ownerLabel WHERE {
  ?brand wdt:P31 wd:Q1331049 ;
         wdt:P127 ?owner .
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en" }
}
LIMIT 50000
`

var qidRE = regexp.MustCompile(`^Q\d+$`)

type wikidataBrand struct {
	Brand string
	Owner string
}

type wikidataResponse struct {
	Results struct {
		Bindings []struct {
			BrandLabel struct{ Value string } `json:"brandLabel"`
			OwnerLabel struct{ Value string } `json:"ownerLabel"`
		} `json:"bindings"`
	} `json:"results"`
}

// ImportBrandsFromWikidata fetches brand→company pairs from the Wikidata
// SPARQL endpoint and inserts them into raw_brands with source "wikidata".
func ImportBrandsFromWikidata(conn *sql.DB) error {
	brands, err := queryWikidata(wikidataClient)
	if err != nil {
		return fmt.Errorf("wikidata query: %w", err)
	}
	return insertWikidataBrands(conn, brands)
}

func queryWikidata(client *http.Client) ([]wikidataBrand, error) {
	body := url.Values{"query": {wikidataSPARQL}}.Encode()
	req, err := http.NewRequest("POST", wikidataEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "BrandCheckUA/1.0 (https://github.com/on0t0le/rubanok)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseWikidataJSON(resp.Body)
}

func parseWikidataJSON(r io.Reader) ([]wikidataBrand, error) {
	var wr wikidataResponse
	if err := json.NewDecoder(r).Decode(&wr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var result []wikidataBrand
	for _, b := range wr.Results.Bindings {
		brand := b.BrandLabel.Value
		owner := b.OwnerLabel.Value
		if brand == "" || owner == "" {
			continue
		}
		if qidRE.MatchString(brand) || qidRE.MatchString(owner) {
			continue
		}
		result = append(result, wikidataBrand{Brand: brand, Owner: owner})
	}
	return result, nil
}

func insertWikidataBrands(conn *sql.DB, brands []wikidataBrand) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO raw_brands (brand_name, company_name, source) VALUES (?, ?, 'wikidata')`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range brands {
		if _, err := stmt.Exec(b.Brand, b.Owner); err != nil {
			return fmt.Errorf("insert %q: %w", b.Brand, err)
		}
	}
	return tx.Commit()
}
