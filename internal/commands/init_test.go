package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteClaudeMDOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Write stale content
	stale := "stale content that does not match the embedded template"
	if err := os.WriteFile(path, []byte(stale), 0644); err != nil {
		t.Fatalf("writing stale CLAUDE.md: %v", err)
	}

	// WriteClaudeMD must always overwrite with the embedded template
	WriteClaudeMD(dir, "reviewer")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading CLAUDE.md after WriteClaudeMD: %v", err)
	}

	expected, err := LoadTemplate("reviewer")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	if string(got) != expected {
		t.Errorf("CLAUDE.md content does not match embedded template after re-deploy\ngot  (%d bytes)\nwant (%d bytes)", len(got), len(expected))
	}
}

func TestWriteClaudeMDCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// file does not exist; WriteClaudeMD should create it
	WriteClaudeMD(dir, "reviewer")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}

	expected, err := LoadTemplate("reviewer")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	if string(got) != expected {
		t.Errorf("newly created CLAUDE.md does not match embedded template")
	}
}

func TestEnsureRootGitignore_createsFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	if !strings.Contains(string(data), ".company_town/") {
		t.Errorf(".gitignore missing .company_town/ entry: %s", data)
	}
}

func TestEnsureRootGitignore_appendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n.env\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "node_modules/") {
		t.Error("existing content was lost")
	}
	if !strings.Contains(content, ".company_town/") {
		t.Errorf(".gitignore missing .company_town/ entry: %s", content)
	}
}

func TestEnsureRootGitignore_idempotent(t *testing.T) {
	dir := t.TempDir()
	existing := "node_modules/\n.company_town/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	// Entry must not be duplicated.
	count := strings.Count(string(data), ".company_town/")
	if count != 1 {
		t.Errorf("expected exactly 1 .company_town/ entry, got %d: %s", count, data)
	}
}

func TestEnsureRootGitignore_noTrailingNewlineInExisting(t *testing.T) {
	dir := t.TempDir()
	// File exists but has no trailing newline.
	existing := "node_modules/"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	content := string(data)
	// Must separate existing content from new entry with a newline.
	if !strings.Contains(content, "\n.company_town/") {
		t.Errorf("entry not properly separated: %q", content)
	}
}

func TestLoadTemplateAllAgentTypes(t *testing.T) {
	types := []string{"mayor", "architect", "reviewer", "artisan",
		"artisan-frontend", "artisan-backend", "artisan-qa_coder"}
	for _, agentType := range types {
		content, err := LoadTemplate(agentType)
		if err != nil {
			t.Errorf("LoadTemplate(%q): unexpected error: %v", agentType, err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("LoadTemplate(%q): returned empty content", agentType)
		}
	}
}
