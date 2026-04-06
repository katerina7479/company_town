package gtcmd

import (
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupPRTestRepo(t *testing.T) *repo.IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn)
}

func testCfg() *config.Config {
	return &config.Config{TicketPrefix: "nc"}
}

func TestFormatPRTitle(t *testing.T) {
	cases := []struct {
		prefix string
		id     int
		title  string
		want   string
	}{
		{"nc", 42, "Fix the bug", "[nc-42] Fix the bug"},
		{"CT", 1, "Add new feature", "[CT-1] Add new feature"},
		{"nc", 100, "Refactor auth layer", "[nc-100] Refactor auth layer"},
	}

	for _, tc := range cases {
		got := formatPRTitle(tc.prefix, tc.id, tc.title)
		if got != tc.want {
			t.Errorf("formatPRTitle(%q, %d, %q) = %q, want %q",
				tc.prefix, tc.id, tc.title, got, tc.want)
		}
	}
}

func TestFormatPRTitle_hasBracketPrefix(t *testing.T) {
	title := formatPRTitle("nc", 7, "Some work")
	if !strings.HasPrefix(title, "[nc-7] ") {
		t.Errorf("expected title to start with \"[nc-7] \", got %q", title)
	}
}

func TestFormatPRTitle_prefixCaseSensitive(t *testing.T) {
	lower := formatPRTitle("nc", 1, "title")
	upper := formatPRTitle("NC", 1, "title")
	if lower == upper {
		t.Errorf("expected prefix to be case-sensitive, but %q == %q", lower, upper)
	}
}

func TestParseTicketID(t *testing.T) {
	cases := []struct {
		input   string
		wantID  int
		wantErr bool
	}{
		{"58", 58, false},
		{"nc-58", 58, false},
		{"NC-58", 58, false},
		{"CT-100", 100, false},
		{"1", 1, false},
		{"prefix-42", 42, false},
		{"notanumber", 0, true},
		{"nc-notanumber", 0, true},
		{"nc-", 0, true},
		{"nc-58-2", 0, true},
	}

	for _, tc := range cases {
		id, err := parseTicketID(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTicketID(%q): expected error, got id=%d", tc.input, id)
			}
		} else {
			if err != nil {
				t.Errorf("parseTicketID(%q): unexpected error: %v", tc.input, err)
			} else if id != tc.wantID {
				t.Errorf("parseTicketID(%q) = %d, want %d", tc.input, id, tc.wantID)
			}
		}
	}
}

func TestPRUpdate_missingArgs(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}
}

func TestPRUpdate_notFound(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestPRUpdate_wrongStatus(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "in_review")

	err := prUpdate(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error when ticket is not in repairing status, got nil")
	}
	if !strings.Contains(err.Error(), "repairing") {
		t.Errorf("expected error to mention 'repairing', got: %v", err)
	}
}

func TestPRUpdate_wrongStatus_open(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "open")

	err := prUpdate(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error for ticket in open status, got nil")
	}
}
