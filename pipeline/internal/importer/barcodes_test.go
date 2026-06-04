package importer

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// buildGzipTSV builds a gzip-compressed TSV with header "code\tbrands\textra"
// and the given rows as [code, brands] pairs.
func buildGzipTSV(rows [][2]string) *bytes.Buffer {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("code\tbrands\textra\n"))
	for _, r := range rows {
		gw.Write([]byte(r[0] + "\t" + r[1] + "\textra\n"))
	}
	gw.Close()
	return &buf
}

func TestImportBarcodesFromReader_MatchesBrand(t *testing.T) {
	conn := tempDB(t)
	conn.Exec(`INSERT INTO companies (id, name, brands) VALUES ('heineken', 'Heineken NV', '["Heineken"]')`)

	if err := importBarcodesFromReader(conn, buildGzipTSV([][2]string{
		{"5000112637922", "Heineken,Heineken International"},
		{"9999999999999", "UnknownBrand"},
	})); err != nil {
		t.Fatal(err)
	}

	var brand string
	if err := conn.QueryRow(`SELECT brand FROM raw_barcodes WHERE code = '5000112637922'`).Scan(&brand); err != nil {
		t.Fatalf("barcode not inserted: %v", err)
	}
	if brand != "Heineken" {
		t.Errorf("brand = %q, want Heineken", brand)
	}
	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM raw_barcodes`).Scan(&count)
	if count != 1 {
		t.Errorf("count = %d, want 1 (UnknownBrand must be skipped)", count)
	}
}

func TestImportBarcodesFromReader_CaseInsensitive(t *testing.T) {
	conn := tempDB(t)
	conn.Exec(`INSERT INTO companies (id, name, brands) VALUES ('pepsi', 'PepsiCo', '["Pepsi"]')`)

	if err := importBarcodesFromReader(conn, buildGzipTSV([][2]string{
		{"0012000001253", "pepsi"}, // lowercase in CSV, original casing in companies
	})); err != nil {
		t.Fatal(err)
	}

	var brand string
	conn.QueryRow(`SELECT brand FROM raw_barcodes WHERE code = '0012000001253'`).Scan(&brand)
	if brand != "Pepsi" {
		t.Errorf("brand = %q, want Pepsi (original casing from companies table)", brand)
	}
}

func TestImportBarcodesFromReader_SkipsEmptyCode(t *testing.T) {
	conn := tempDB(t)
	conn.Exec(`INSERT INTO companies (id, name, brands) VALUES ('co', 'Co', '["Brand"]')`)

	if err := importBarcodesFromReader(conn, buildGzipTSV([][2]string{
		{"", "Brand"},
	})); err != nil {
		t.Fatal(err)
	}

	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM raw_barcodes`).Scan(&count)
	if count != 0 {
		t.Errorf("count = %d, want 0 (empty code must be skipped)", count)
	}
}

func TestImportBarcodesFromReader_NoCompanies(t *testing.T) {
	conn := tempDB(t)
	// no companies → empty brand set → nothing to match

	if err := importBarcodesFromReader(conn, buildGzipTSV([][2]string{
		{"123", "SomeBrand"},
	})); err != nil {
		t.Fatal(err)
	}

	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM raw_barcodes`).Scan(&count)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
