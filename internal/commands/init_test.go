package commands

import (
	"net"
	"os"
	"path/filepath"
	"testing"
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
