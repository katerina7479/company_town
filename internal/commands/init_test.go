package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteClaudeMDForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Write stale content
	stale := "stale content that does not match the embedded template"
	if err := os.WriteFile(path, []byte(stale), 0644); err != nil {
		t.Fatalf("writing stale CLAUDE.md: %v", err)
	}

	// force=true must overwrite with the embedded template
	WriteClaudeMD(dir, "reviewer", true)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading CLAUDE.md after WriteClaudeMD: %v", err)
	}

	expected, err := LoadTemplate("reviewer")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	if string(got) != expected {
		t.Errorf("CLAUDE.md content does not match embedded template after force re-deploy\ngot  (%d bytes)\nwant (%d bytes)", len(got), len(expected))
	}
}

func TestWriteClaudeMDNoForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Write stale content
	stale := "stale content"
	if err := os.WriteFile(path, []byte(stale), 0644); err != nil {
		t.Fatalf("writing stale CLAUDE.md: %v", err)
	}

	// force=false must NOT overwrite when file already exists
	WriteClaudeMD(dir, "reviewer", false)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}

	if string(got) != stale {
		t.Errorf("WriteClaudeMD with force=false must not overwrite existing file")
	}
}

func TestWriteClaudeMDCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// file does not exist; force=false should create it
	WriteClaudeMD(dir, "reviewer", false)

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
