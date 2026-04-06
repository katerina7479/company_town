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
	// Description should remain NULL when not provided.
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

	id, _ := issues.Create("Test ticket", "task", nil, nil)

	err := ticketDescribe(issues, []string{"1", "Updated description."})
	if err != nil {
		t.Fatalf("ticketDescribe: %v", err)
	}

	issue, err := issues.Get(id)
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
