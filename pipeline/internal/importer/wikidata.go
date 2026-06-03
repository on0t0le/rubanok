package importer

import (
	"bytes"
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

var wikidataClient = &http.Client{Timeout: 120 * time.Second}
var wikidataEndpoint = "https://query.wikidata.org/sparql"

// wikidataSPARQL fetches brand→company for typed brand entities (Q1331049=product brand,
// Q167270=trademark, Q431289=brand, Q80359036=alcohol brand, Q10373548=whisky distillery).
// Uses P127 (owned by) and P176 (manufacturer) to cover different ownership styles.
const wikidataSPARQL = `
SELECT DISTINCT ?brandLabel ?ownerLabel WHERE {
  VALUES ?type { wd:Q1331049 wd:Q167270 wd:Q431289 wd:Q80359036 wd:Q10373548 }
  ?brand wdt:P31 ?type .
  { ?brand wdt:P127 ?owner . } UNION { ?brand wdt:P176 ?owner . }
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

type personQueryResponse struct {
	Results struct {
		Bindings []struct {
			Name struct{ Value string } `json:"name"`
		} `json:"bindings"`
	} `json:"results"`
}

// FetchPersonNames queries Wikidata and returns the subset of names that are
// instances of human (Q5). Returns empty map on empty input or error.
func FetchPersonNames(names []string) (map[string]bool, error) {
	if len(names) == 0 {
		return map[string]bool{}, nil
	}

	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(`"`)
		sb.WriteString(strings.ReplaceAll(name, `"`, `\"`))
		sb.WriteString(`"@en `)
	}
	sparql := fmt.Sprintf(`SELECT DISTINCT ?name WHERE {
  VALUES ?name { %s }
  ?item rdfs:label ?name ;
        wdt:P31 wd:Q5 .
}`, sb.String())

	body := url.Values{"query": {sparql}}.Encode()
	req, err := http.NewRequest("POST", wikidataEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "BrandCheckUA/1.0 (https://github.com/on0t0le/rubanok)")

	resp, err := wikidataClient.Do(req)
	if err != nil {
		return map[string]bool{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return map[string]bool{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var pr personQueryResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&pr); err != nil { // 16 MB sufficient — person queries return sparse name lists
		return nil, fmt.Errorf("decode: %w", err)
	}

	result := make(map[string]bool)
	for _, b := range pr.Results.Bindings {
		if b.Name.Value != "" {
			result[b.Name.Value] = true
		}
	}
	return result, nil
}

// ImportBrandsFromWikidata fetches brand→company pairs from the Wikidata
// SPARQL endpoint and inserts them into raw_brands with source "wikidata".
func ImportBrandsFromWikidata(conn *sql.DB) error {
	brands, err := queryWikidata(wikidataClient, wikidataSPARQL)
	if err != nil {
		return fmt.Errorf("wikidata query: %w", err)
	}
	return insertWikidataBrands(conn, brands)
}

func queryWikidata(client *http.Client, sparql string) ([]wikidataBrand, error) {
	body := url.Values{"query": {sparql}}.Encode()
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	// Wikidata may return literal newlines inside JSON string values (invalid JSON);
	// replace them with spaces before parsing.
	raw = bytes.ReplaceAll(raw, []byte("\n"), []byte(" "))
	return parseWikidataJSON(bytes.NewReader(raw))
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
