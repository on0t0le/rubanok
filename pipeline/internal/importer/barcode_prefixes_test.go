package importer

import "testing"

func TestDeriveBarcodePrefixes_Basic(t *testing.T) {
	conn := tempDB(t)

	// 5 barcodes all mapping to Pepsi — prefix "001200" should be reliable
	for _, code := range []string{"0012000001253", "0012000001260", "0012000001277", "0012000001284", "0012000001291"} {
		conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES (?, 'Pepsi')`, code)
	}

	if err := DeriveBarcodePrefixes(conn); err != nil {
		t.Fatalf("DeriveBarcodePrefixes: %v", err)
	}

	var brand string
	if err := conn.QueryRow(`SELECT brand FROM barcode_prefixes WHERE prefix = '001200'`).Scan(&brand); err != nil {
		t.Fatalf("prefix not found: %v", err)
	}
	if brand != "Pepsi" {
		t.Errorf("brand = %q, want Pepsi", brand)
	}
}

func TestDeriveBarcodePrefixes_BelowThreshold(t *testing.T) {
	conn := tempDB(t)

	// 3 Pepsi + 1 Coke sharing same prefix — 75% Pepsi, below 95% threshold
	for _, code := range []string{"0012000001253", "0012000001260", "0012000001277"} {
		conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES (?, 'Pepsi')`, code)
	}
	conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES ('0012000001284', 'Coke')`)

	if err := DeriveBarcodePrefixes(conn); err != nil {
		t.Fatalf("DeriveBarcodePrefixes: %v", err)
	}

	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM barcode_prefixes WHERE prefix = '001200'`).Scan(&count)
	if count != 0 {
		t.Errorf("mixed prefix should not be stored, got count=%d", count)
	}
}

func TestDeriveBarcodePrefixes_BelowMinCount(t *testing.T) {
	conn := tempDB(t)

	// Only 2 barcodes — below minCount of 3
	conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES ('9000101213942', 'Schauma')`)
	conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES ('9000101213943', 'Schauma')`)

	if err := DeriveBarcodePrefixes(conn); err != nil {
		t.Fatalf("DeriveBarcodePrefixes: %v", err)
	}

	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM barcode_prefixes`).Scan(&count)
	if count != 0 {
		t.Errorf("below-mincount prefix should not be stored, got count=%d", count)
	}
}

func TestDeriveBarcodePrefixes_EmptyTable(t *testing.T) {
	conn := tempDB(t)
	if err := DeriveBarcodePrefixes(conn); err != nil {
		t.Fatalf("DeriveBarcodePrefixes on empty table: %v", err)
	}
}

func TestDeriveBarcodePrefixes_LongerPrefixMoreSpecific(t *testing.T) {
	conn := tempDB(t)

	// 6 barcodes: all start with "900010", but only 3 share "9000101" → Henkel
	// and 3 share "9000102" → Schwarzkopf
	for _, code := range []string{"9000101001111", "9000101002222", "9000101003333"} {
		conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES (?, 'Schwarzkopf')`, code)
	}
	for _, code := range []string{"9000102001111", "9000102002222", "9000102003333"} {
		conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES (?, 'Fa')`, code)
	}

	if err := DeriveBarcodePrefixes(conn); err != nil {
		t.Fatalf("DeriveBarcodePrefixes: %v", err)
	}

	// "9000101" prefix should map to Schwarzkopf
	var brand string
	if err := conn.QueryRow(`SELECT brand FROM barcode_prefixes WHERE prefix = '9000101'`).Scan(&brand); err != nil {
		t.Fatalf("prefix 9000101 not found: %v", err)
	}
	if brand != "Schwarzkopf" {
		t.Errorf("prefix 9000101 brand = %q, want Schwarzkopf", brand)
	}
}
