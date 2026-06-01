package importer

import "testing"

func TestImportKSE(t *testing.T) {
	conn := tempDB(t)

	if err := ImportKSEFromPath(conn, "testdata/kse.csv"); err != nil {
		t.Fatalf("ImportKSEFromPath: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_kse").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("got %d rows, want 3", count)
	}

	var status string
	if err := conn.QueryRow(
		"SELECT status FROM raw_kse WHERE company_name = 'Gazprom'",
	).Scan(&status); err != nil {
		t.Fatalf("query Gazprom: %v", err)
	}
	if status != "Operating" {
		t.Errorf("status = %q, want Operating", status)
	}
}

func TestNormalizeKSEStatus(t *testing.T) {
	cases := []struct{ input, want string }{
		{"Exited", "Exited"},
		{"left", "Exited"},
		{"Suspended", "Suspended"},
		{"Operating", "Operating"},
		{"continues", "Operating"},
		{"Reduced Operations", "Reduced Operations"},
		{"paused", "Suspended"},
		{"reduced", "Reduced Operations"},
		{"whatever", "Unknown"},
	}
	for _, c := range cases {
		got := normalizeKSEStatus(c.input)
		if got != c.want {
			t.Errorf("normalizeKSEStatus(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
