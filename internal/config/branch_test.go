package config

import "testing"

func TestProleBranchName(t *testing.T) {
	cases := []struct {
		prefix string
		prole  string
		id     int
		want   string
	}{
		{"nc", "copper", 56, "prole/copper/nc-56"},
		{"NC", "iron", 1, "prole/iron/NC-1"},
		{"ct", "obsidian", 42, "prole/obsidian/ct-42"},
		{"", "zinc", 7, "prole/zinc/-7"},
	}
	for _, tc := range cases {
		got := ProleBranchName(tc.prefix, tc.prole, tc.id)
		if got != tc.want {
			t.Errorf("ProleBranchName(%q, %q, %d) = %q, want %q",
				tc.prefix, tc.prole, tc.id, got, tc.want)
		}
	}
}
