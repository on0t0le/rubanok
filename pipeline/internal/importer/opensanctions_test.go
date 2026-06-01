package importer

import "testing"

func TestImportOpenSanctions(t *testing.T) {
	conn := tempDB(t)

	if err := ImportOpenSanctionsFromPath(conn, "testdata/opensanctions.csv"); err != nil {
		t.Fatalf("ImportOpenSanctionsFromPath: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_opensanctions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("got %d rows, want 3", count)
	}

	var name, aliases string
	var sanctioned int
	err := conn.QueryRow(
		"SELECT name, aliases, sanctioned_ua FROM raw_opensanctions WHERE id = 'NK-abc123'",
	).Scan(&name, &aliases, &sanctioned)
	if err != nil {
		t.Fatalf("query NK-abc123: %v", err)
	}
	if name != "Gazprom" {
		t.Errorf("name = %q, want Gazprom", name)
	}
	if sanctioned != 1 {
		t.Errorf("sanctioned_ua = %d, want 1", sanctioned)
	}
}
