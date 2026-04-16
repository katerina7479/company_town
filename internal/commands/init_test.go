package commands

import (
	"encoding/json"
	"fmt"
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

// --- deriveTicketPrefix tests ---

func TestDeriveTicketPrefix_alphaOnly(t *testing.T) {
	got := deriveTicketPrefix("/home/user/docflow")
	if got != "docflow" {
		t.Errorf("deriveTicketPrefix = %q, want %q", got, "docflow")
	}
}

func TestDeriveTicketPrefix_stripsDigitsAndSymbols(t *testing.T) {
	got := deriveTicketPrefix("/projects/my-project-42")
	if got != "myproject" {
		t.Errorf("deriveTicketPrefix = %q, want %q", got, "myproject")
	}
}

func TestDeriveTicketPrefix_uppercase(t *testing.T) {
	got := deriveTicketPrefix("/Users/Kate/CompanyTown")
	if got != "companytown" {
		t.Errorf("deriveTicketPrefix = %q, want %q", got, "companytown")
	}
}

func TestDeriveTicketPrefix_emptyWhenNoAlpha(t *testing.T) {
	got := deriveTicketPrefix("/projects/123-456")
	if got != "" {
		t.Errorf("deriveTicketPrefix = %q, want %q", got, "")
	}
}

// --- deriveDoltDatabase tests ---

func TestDeriveDoltDatabase_simple(t *testing.T) {
	got := deriveDoltDatabase("/home/user/docflow")
	if got != "docflow" {
		t.Errorf("deriveDoltDatabase = %q, want %q", got, "docflow")
	}
}

func TestDeriveDoltDatabase_hyphenBecomeUnderscore(t *testing.T) {
	got := deriveDoltDatabase("/projects/my-project")
	if got != "my_project" {
		t.Errorf("deriveDoltDatabase = %q, want %q", got, "my_project")
	}
}

func TestDeriveDoltDatabase_stripsLeadingDigit(t *testing.T) {
	got := deriveDoltDatabase("/projects/42app")
	if got != "app" {
		t.Errorf("deriveDoltDatabase = %q, want %q", got, "app")
	}
}

func TestDeriveDoltDatabase_fallbackWhenEmpty(t *testing.T) {
	got := deriveDoltDatabase("/projects/---")
	if got != "company_town" {
		t.Errorf("deriveDoltDatabase = %q, want %q", got, "company_town")
	}
}

// --- deriveGithubRepo tests ---

func TestDeriveGithubRepo_httpsURL(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) {
		return "https://github.com/kate/myrepo.git", nil
	}
	got := deriveGithubRepo()
	if got != "kate/myrepo" {
		t.Errorf("deriveGithubRepo = %q, want %q", got, "kate/myrepo")
	}
}

func TestDeriveGithubRepo_sshURL(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) {
		return "git@github.com:kate/myrepo.git", nil
	}
	got := deriveGithubRepo()
	if got != "kate/myrepo" {
		t.Errorf("deriveGithubRepo = %q, want %q", got, "kate/myrepo")
	}
}

func TestDeriveGithubRepo_noRemote(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }
	got := deriveGithubRepo()
	if got != "" {
		t.Errorf("deriveGithubRepo = %q, want empty string", got)
	}
}

// --- validateTicketPrefix tests ---

func TestValidateTicketPrefix_valid(t *testing.T) {
	for _, s := range []string{"nc", "ct", "docflow", "abc"} {
		if err := validateTicketPrefix(s); err != nil {
			t.Errorf("validateTicketPrefix(%q) unexpected error: %v", s, err)
		}
	}
}

func TestValidateTicketPrefix_invalid(t *testing.T) {
	for _, s := range []string{"", "NC", "nc-1", "123", "my_app"} {
		if err := validateTicketPrefix(s); err == nil {
			t.Errorf("validateTicketPrefix(%q) expected error, got nil", s)
		}
	}
}

// --- collectInitParams tests ---

func TestCollectInitParams_nonInteractive_derivesFromDir(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) {
		return "https://github.com/owner/docflow.git", nil
	}

	params, err := collectInitParams(true, nil, "/projects/docflow", 3308)
	if err != nil {
		t.Fatalf("collectInitParams: %v", err)
	}
	if params.ticketPrefix != "docflow" {
		t.Errorf("ticketPrefix = %q, want %q", params.ticketPrefix, "docflow")
	}
	if params.githubRepo != "owner/docflow" {
		t.Errorf("githubRepo = %q, want %q", params.githubRepo, "owner/docflow")
	}
	if params.doltDatabase != "docflow" {
		t.Errorf("doltDatabase = %q, want %q", params.doltDatabase, "docflow")
	}
	if params.doltPort != 3308 {
		t.Errorf("doltPort = %d, want %d", params.doltPort, 3308)
	}
}

func TestCollectInitParams_nonInteractive_fallbackWhenNoDerived(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	// dir name "---" produces no alpha prefix and no usable DB name (falls to company_town)
	params, err := collectInitParams(true, nil, "/projects/---", 3307)
	if err != nil {
		t.Fatalf("collectInitParams: %v", err)
	}
	if params.ticketPrefix != "tk" {
		t.Errorf("ticketPrefix fallback = %q, want %q", params.ticketPrefix, "tk")
	}
	if params.githubRepo != "owner/repo" {
		t.Errorf("githubRepo fallback = %q, want %q", params.githubRepo, "owner/repo")
	}
	if params.doltDatabase != "company_town" {
		t.Errorf("doltDatabase fallback = %q, want %q", params.doltDatabase, "company_town")
	}
}

func TestCollectInitParams_interactive_acceptsDefaults(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) {
		return "https://github.com/owner/myapp.git", nil
	}

	// Simulate user pressing Enter for all 5 prompts (accepts all defaults).
	input := strings.NewReader("\n\n\n\n\n")
	params, err := collectInitParams(false, input, "/projects/myapp", 3309)
	if err != nil {
		t.Fatalf("collectInitParams interactive: %v", err)
	}
	if params.ticketPrefix != "myapp" {
		t.Errorf("ticketPrefix = %q, want %q", params.ticketPrefix, "myapp")
	}
	if params.githubRepo != "owner/myapp" {
		t.Errorf("githubRepo = %q, want %q", params.githubRepo, "owner/myapp")
	}
	if params.doltPort != 3309 {
		t.Errorf("doltPort = %d, want %d", params.doltPort, 3309)
	}
	// /projects/myapp does not exist, so no marker files → agnostic
	if params.languagePreset != "" {
		t.Errorf("languagePreset = %q, want empty (no marker files)", params.languagePreset)
	}
}

func TestCollectInitParams_interactive_customValues(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	// User types custom values for all five fields.
	input := strings.NewReader("myproj\nkate/myproj\nmydb\n4000\npython\n")
	params, err := collectInitParams(false, input, "/projects/x", 3307)
	if err != nil {
		t.Fatalf("collectInitParams interactive custom: %v", err)
	}
	if params.ticketPrefix != "myproj" {
		t.Errorf("ticketPrefix = %q, want %q", params.ticketPrefix, "myproj")
	}
	if params.githubRepo != "kate/myproj" {
		t.Errorf("githubRepo = %q, want %q", params.githubRepo, "kate/myproj")
	}
	if params.doltDatabase != "mydb" {
		t.Errorf("doltDatabase = %q, want %q", params.doltDatabase, "mydb")
	}
	if params.doltPort != 4000 {
		t.Errorf("doltPort = %d, want %d", params.doltPort, 4000)
	}
	if params.languagePreset != "python" {
		t.Errorf("languagePreset = %q, want %q", params.languagePreset, "python")
	}
}

func TestCollectInitParams_interactive_rejectsInvalidPrefix(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	// First attempt: invalid prefix "BAD", second: valid "good". Blank for language.
	input := strings.NewReader("BAD\ngood\nowner/repo\nmydb\n3307\n\n")
	params, err := collectInitParams(false, input, "/projects/x", 3307)
	if err != nil {
		t.Fatalf("collectInitParams retry: %v", err)
	}
	if params.ticketPrefix != "good" {
		t.Errorf("ticketPrefix after retry = %q, want %q", params.ticketPrefix, "good")
	}
}

func TestCollectInitParams_interactive_rejectsInvalidLanguagePreset(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	// First language attempt: "rust" (invalid), second: "go" (valid).
	input := strings.NewReader("myproj\nowner/myproj\nmydb\n3307\nrust\ngo\n")
	params, err := collectInitParams(false, input, "/projects/x", 3307)
	if err != nil {
		t.Fatalf("collectInitParams language retry: %v", err)
	}
	if params.languagePreset != "go" {
		t.Errorf("languagePreset after retry = %q, want %q", params.languagePreset, "go")
	}
}

func TestCollectInitParams_nonInteractive_detectsGoProject(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	params, err := collectInitParams(true, nil, dir, 3307)
	if err != nil {
		t.Fatalf("collectInitParams: %v", err)
	}
	if params.languagePreset != "go" {
		t.Errorf("languagePreset = %q, want %q", params.languagePreset, "go")
	}
}

func TestCollectInitParams_nonInteractive_detectsPythonProject(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.pytest]\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	params, err := collectInitParams(true, nil, dir, 3307)
	if err != nil {
		t.Fatalf("collectInitParams: %v", err)
	}
	if params.languagePreset != "python" {
		t.Errorf("languagePreset = %q, want %q", params.languagePreset, "python")
	}
}

func TestCollectInitParams_nonInteractive_agnosticWhenNoMarkers(t *testing.T) {
	old := gitRemoteURLFn
	defer func() { gitRemoteURLFn = old }()
	gitRemoteURLFn = func() (string, error) { return "", fmt.Errorf("no remote") }

	dir := t.TempDir()
	params, err := collectInitParams(true, nil, dir, 3307)
	if err != nil {
		t.Fatalf("collectInitParams: %v", err)
	}
	if params.languagePreset != "" {
		t.Errorf("languagePreset = %q, want empty (no marker files)", params.languagePreset)
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
