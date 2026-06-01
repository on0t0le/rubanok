package normalize

import "testing"

func TestCompany(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Mondelēz International Inc.", "mondelez"},
		{"NESTLÉ S.A.", "nestle"},
		{"Volkswagen AG", "volkswagen"},
		{"Apple, Inc.", "apple"},
		{"Samsung Electronics Co., Ltd.", "samsung electronics"},
	}
	for _, c := range cases {
		got := Company(c.input)
		if got != c.want {
			t.Errorf("Company(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestTokenSortRatio(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"mondelez", "mondelez", 100},
		{"mondelez international", "international mondelez", 100},
		{"apple", "google", 0},
	}
	for _, c := range cases {
		got := TokenSortRatio(c.a, c.b)
		if got != c.want {
			t.Errorf("TokenSortRatio(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
