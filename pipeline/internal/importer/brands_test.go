package importer

import "testing"

func TestImportBrandsFromJSON(t *testing.T) {
	conn := tempDB(t)

	if err := ImportBrandsFromJSONPath(conn, "testdata/brands.json"); err != nil {
		t.Fatalf("ImportBrandsFromJSONPath: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_brands").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("got %d rows, want 3", count)
	}

	var owner string
	if err := conn.QueryRow(
		"SELECT company_name FROM raw_brands WHERE brand_name = 'Oreo'",
	).Scan(&owner); err != nil {
		t.Fatalf("query Oreo: %v", err)
	}
	if owner != "Mondelez International" {
		t.Errorf("owner = %q, want Mondelez International", owner)
	}
}
