package gtcmd

import (
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupTicketTestRepo(t *testing.T) *repo.IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn)
}

func TestTicketCreate_withDescription(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"My ticket", "--description", "Some details here."})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Description.Valid || issue.Description.String != "Some details here." {
		t.Errorf("expected description %q, got %q (valid=%v)",
			"Some details here.", issue.Description.String, issue.Description.Valid)
	}
}

func TestTicketCreate_withoutDescription(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"No desc ticket"})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Description.Valid && issue.Description.String != "" {
		t.Errorf("expected description to be empty, got %q", issue.Description.String)
	}
}

func TestTicketCreate_descriptionMissingValue(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"My ticket", "--description"})
	if err == nil {
		t.Fatal("expected error when --description has no value")
	}
	if !strings.Contains(err.Error(), "--description requires a value") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTicketDescribe(t *testing.T) {
	issues := setupTicketTestRepo(t)

	issues.Create("Test ticket", "task", nil, nil, nil)

	err := ticketDescribe(issues, []string{"1", "Updated description."})
	if err != nil {
		t.Fatalf("ticketDescribe: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Description.Valid || issue.Description.String != "Updated description." {
		t.Errorf("expected %q, got %q (valid=%v)",
			"Updated description.", issue.Description.String, issue.Description.Valid)
	}
}

func TestTicketDescribe_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{"9999", "anything"})
	if err == nil {
		t.Fatal("expected error for non-existent issue")
	}
}

func TestTicketDescribe_missingArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{"1"})
	if err == nil {
		t.Fatal("expected error when description argument is missing")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestTicketDescribe_noArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{})
	if err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestTicketPrioritize_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketPrioritize(issues, []string{"1", "P1"}); err != nil {
		t.Fatalf("ticketPrioritize: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P1" {
		t.Errorf("expected priority='P1', got %v", issue.Priority)
	}
}

func TestTicketPrioritize_allValidPriorities(t *testing.T) {
	for _, p := range []string{"P0", "P1", "P2", "P3"} {
		t.Run(p, func(t *testing.T) {
			issues := setupTicketTestRepo(t)

			_, err := issues.Create("A task", "task", nil, nil, nil)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			if err := ticketPrioritize(issues, []string{"1", p}); err != nil {
				t.Errorf("ticketPrioritize with %q: unexpected error: %v", p, err)
			}
		})
	}
}

func TestTicketPrioritize_invalidPriority(t *testing.T) {
	issues := setupTicketTestRepo(t)

	_, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = ticketPrioritize(issues, []string{"1", "P5"})
	if err == nil {
		t.Fatal("expected error for invalid priority 'P5', got nil")
	}
}

func TestTicketPrioritize_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketPrioritize(issues, []string{"9999", "P0"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestTicketPrioritize_missingArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketPrioritize(issues, []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}

	err = ticketPrioritize(issues, []string{"1"})
	if err == nil {
		t.Fatal("expected usage error for 1 arg, got nil")
	}
}

func TestTicketPrioritize_prefixedID(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketPrioritize(issues, []string{"nc-1", "P2"}); err != nil {
		t.Fatalf("ticketPrioritize with prefixed id: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P2" {
		t.Errorf("expected priority='P2', got %v", issue.Priority)
	}
}
