package commands

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// TestStartAgent_resetsDeadStatusWhenSessionExists verifies that when a tmux
// session is already live but the DB shows the agent as dead, startAgent resets
// the status to idle before attaching.
func TestStartAgent_resetsDeadStatusWhenSessionExists(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Agents: config.AgentsConfig{
			Mayor: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Pre-register the agent and mark it dead.
	if err := agents.Register("mayor", "mayor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.UpdateStatus("mayor", repo.StatusDead); err != nil {
		t.Fatalf("UpdateStatus dead: %v", err)
	}

	// Stub session existence: session is live.
	oldExists := sessionExistsFn
	defer func() { sessionExistsFn = oldExists }()
	sessionExistsFn = func(_ string) bool { return true }

	// Stub attach: no-op so no real tmux call is made.
	oldAttach := sessionAttachFn
	defer func() { sessionAttachFn = oldAttach }()
	sessionAttachFn = func(_ string) error { return nil }

	if err := startAgent("mayor", "mayor", "claude-test", cfg, agents, "prompt"); err != nil {
		t.Fatalf("startAgent: %v", err)
	}

	a, err := agents.Get("mayor")
	if err != nil {
		t.Fatalf("agents.Get(mayor): %v", err)
	}
	if a.Status != repo.StatusIdle {
		t.Errorf("expected status=idle after start with dead DB entry, got %q", a.Status)
	}
}

// TestStartAgent_doesNotOverwriteWorkingStatus verifies that if the agent is
// already working (not dead) and its session is live, startAgent does not
// downgrade the status to idle.
func TestStartAgent_doesNotOverwriteWorkingStatus(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Agents: config.AgentsConfig{
			Mayor: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	if err := agents.Register("mayor", "mayor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.UpdateStatus("mayor", repo.StatusWorking); err != nil {
		t.Fatalf("UpdateStatus working: %v", err)
	}

	oldExists := sessionExistsFn
	defer func() { sessionExistsFn = oldExists }()
	sessionExistsFn = func(_ string) bool { return true }

	oldAttach := sessionAttachFn
	defer func() { sessionAttachFn = oldAttach }()
	sessionAttachFn = func(_ string) error { return nil }

	if err := startAgent("mayor", "mayor", "claude-test", cfg, agents, "prompt"); err != nil {
		t.Fatalf("startAgent: %v", err)
	}

	a, err := agents.Get("mayor")
	if err != nil {
		t.Fatalf("agents.Get(mayor): %v", err)
	}
	if a.Status != repo.StatusWorking {
		t.Errorf("expected status=working to be preserved, got %q", a.Status)
	}
}

// writeTestProjectConfig creates a minimal .company_town/config.json in root
// so that db.FindProjectRoot and config.Load succeed in Start() tests.
func writeTestProjectConfig(t *testing.T, root string) {
	t.Helper()
	ctDir := filepath.Join(root, config.DirName)
	if err := os.MkdirAll(ctDir, 0750); err != nil {
		t.Fatalf("mkdir .company_town: %v", err)
	}
	cfg := config.DefaultConfig(root, "github", "octocat/hello-world")
	if err := config.Write(root, cfg); err != nil {
		t.Fatalf("config.Write: %v", err)
	}
}

// TestStart_callsStartServerOnColdStart verifies that Start() calls startServerFn
// before opening the DB connection. This is the cold-start wiring invariant: even
// when the Dolt server is not yet running, ct start brings it up automatically.
func TestStart_callsStartServerOnColdStart(t *testing.T) {
	root := t.TempDir()
	writeTestProjectConfig(t, root)

	// Change CWD so db.FindProjectRoot() walks up and finds .company_town/.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck // test cleanup
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	serverCalled := false
	old := startServerFn
	defer func() { startServerFn = old }()
	startServerFn = func(doltDir, ctDir string, cfg *config.DoltConfig) error {
		serverCalled = true
		// Verify paths point inside the test project (resolve symlinks for macOS /var → /private/var).
		if !strings.HasPrefix(doltDir, resolvedRoot) {
			t.Errorf("startServerFn: doltDir %q does not start with project root %q", doltDir, resolvedRoot)
		}
		return nil
	}

	// Inject a fully-migrated test DB so RunMigrations is a no-op.
	testDB, err := db.NewFullyMigratedTestDB()
	if err != nil {
		t.Fatalf("NewFullyMigratedTestDB: %v", err)
	}
	oldConnect := connectFn
	defer func() { connectFn = oldConnect }()
	connectFn = func(_ *config.DoltConfig) (*sql.DB, error) { return testDB, nil }

	// Stub sessions: both daemon and mayor are already running to avoid real tmux calls.
	oldExists := sessionExistsFn
	defer func() { sessionExistsFn = oldExists }()
	sessionExistsFn = func(_ string) bool { return true }

	oldAttach := sessionAttachFn
	defer func() { sessionAttachFn = oldAttach }()
	sessionAttachFn = func(_ string) error { return nil }

	if err := Start(); err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}
	if !serverCalled {
		t.Error("Start() did not call startServerFn — Dolt server would not be started on cold boot")
	}
}

// TestStart_propagatesStartServerError verifies that Start() returns an error
// (and does not proceed to connect or start agents) when startServerFn fails.
func TestStart_propagatesStartServerError(t *testing.T) {
	root := t.TempDir()
	writeTestProjectConfig(t, root)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck // test cleanup
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	sentinelErr := errors.New("dolt binary not found in PATH")
	old := startServerFn
	defer func() { startServerFn = old }()
	startServerFn = func(_, _ string, _ *config.DoltConfig) error { return sentinelErr }

	connectCalled := false
	oldConnect := connectFn
	defer func() { connectFn = oldConnect }()
	connectFn = func(_ *config.DoltConfig) (*sql.DB, error) {
		connectCalled = true
		return nil, errors.New("should not be reached")
	}

	err = Start()
	if err == nil {
		t.Fatal("Start() expected error when startServerFn fails, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("Start() error does not wrap sentinelErr: %v", err)
	}
	if connectCalled {
		t.Error("Start() called connectFn after startServerFn failed — should abort early")
	}
}
