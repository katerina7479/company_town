package commands

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
)

func TestPickFreePort_returnsFreeStart(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer l.Close()
	occupied := l.Addr().(*net.TCPAddr).Port
	got, err := pickFreePort(occupied)
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if got == occupied {
		t.Errorf("pickFreePort returned the occupied port %d", occupied)
	}
}

func TestPickFreePort_unoccupiedStartReturnedDirectly(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	free := l.Addr().(*net.TCPAddr).Port
	l.Close()
	got, err := pickFreePort(free)
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if got != free {
		t.Errorf("pickFreePort(%d) = %d, want %d", free, got, free)
	}
}

func TestPickFreePort_skipsOccupied(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer l.Close()
	start := l.Addr().(*net.TCPAddr).Port
	got, err := pickFreePort(start)
	if err != nil {
		t.Fatalf("pickFreePort(%d): %v", start, err)
	}
	if got <= start {
		t.Errorf("expected port > %d (occupied), got %d", start, got)
	}
}

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

func TestEnsureRootGitignore_bareNameIsEquivalent(t *testing.T) {
	dir := t.TempDir()
	// ".company_town" (no trailing slash) is equivalent — must not duplicate.
	existing := "node_modules/\n.company_town\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(before) != string(after) {
		t.Errorf("file was modified but .company_town (no slash) should be treated as equivalent:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestEnsureRootGitignore_rootAnchoredIsEquivalent(t *testing.T) {
	dir := t.TempDir()
	// "/.company_town/" (root-anchored) is equivalent — must not duplicate.
	existing := "node_modules/\n/.company_town/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(before) != string(after) {
		t.Errorf("file was modified but /.company_town/ should be treated as equivalent:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestEnsureRootGitignore_inlineCommentIsEquivalent(t *testing.T) {
	dir := t.TempDir()
	// ".company_town/  # local runtime state" must be treated as already present.
	existing := ".company_town/  # local runtime state\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err := ensureRootGitignore(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(before) != string(after) {
		t.Errorf("file was modified but entry with inline comment should be equivalent:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestDefaultConfigGithubRepoPlaceholder(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, config.DirName)
	if err := os.MkdirAll(ctDir, 0750); err != nil {
		t.Fatalf("creating ct dir: %v", err)
	}

	cfg := config.DefaultConfig(dir, "owner/repo")
	if err := config.Write(dir, cfg); err != nil {
		t.Fatalf("config.Write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ctDir, config.ConfigFile))
	if err != nil {
		t.Fatalf("reading config.json: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshaling config.json: %v", err)
	}

	got, _ := raw["github_repo"].(string)
	if got != "owner/repo" {
		t.Errorf("github_repo = %q, want %q", got, "owner/repo")
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
