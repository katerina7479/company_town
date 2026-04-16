package gtcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// TestStart_Daemon verifies that `gt start daemon` registers the daemon agent,
// launches it via startDaemonFn, and sets its status to "working" (not "idle").
func TestStart_Daemon(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot: tmpDir,
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Daemon is not running.
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	// Stub startDaemonFn so no real tmux session is created.
	daemonLaunched := false
	oldStartDaemon := startDaemonFn
	defer func() { startDaemonFn = oldStartDaemon }()
	startDaemonFn = func(_ *config.Config) error {
		daemonLaunched = true
		return nil
	}

	if err := startAgentWithDeps(cfg, agents, "daemon"); err != nil {
		t.Fatalf("startAgentWithDeps(daemon): %v", err)
	}

	if !daemonLaunched {
		t.Error("expected startDaemonFn to be called, but it was not")
	}

	a, err := agents.Get("daemon")
	if err != nil {
		t.Fatalf("agents.Get(daemon): %v", err)
	}
	if a.Status != "working" {
		t.Errorf("expected status=working for daemon, got %q", a.Status)
	}
	if a.Type != "daemon" {
		t.Errorf("expected type=daemon, got %q", a.Type)
	}

	expectedSession := session.SessionName("daemon")
	if !a.TmuxSession.Valid || a.TmuxSession.String != expectedSession {
		t.Errorf("expected tmux session %q, got %q (valid=%v)", expectedSession, a.TmuxSession.String, a.TmuxSession.Valid)
	}
}

// TestStart_Daemon_AlreadyRunning verifies that if the daemon tmux session already
// exists, startAgentWithDeps returns nil without launching a new session.
func TestStart_Daemon_AlreadyRunning(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot: tmpDir,
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Daemon is already running.
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return true }

	launchCount := 0
	oldStartDaemon := startDaemonFn
	defer func() { startDaemonFn = oldStartDaemon }()
	startDaemonFn = func(_ *config.Config) error {
		launchCount++
		return nil
	}

	if err := startAgentWithDeps(cfg, agents, "daemon"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if launchCount != 0 {
		t.Errorf("expected startDaemonFn not to be called when already running, got %d calls", launchCount)
	}
}

// TestStart_SpawnsAgentIdle verifies that startAgentWithDeps sets agent status
// to "idle" (not "working") after spawning the session, guarding against regression.
func TestStart_SpawnsAgentIdle(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "test",
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Stub out session creation so no real tmux session is attempted.
	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(_ session.AgentSessionConfig) error { return nil }

	// Stub out tmux existence check so agent is not considered already running.
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	// Stub out worktree provisioning — no real git repo in test env.
	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, agentDir string) (string, error) {
		return agentDir + "/worktree", nil
	}

	if err := startAgentWithDeps(cfg, agents, "architect"); err != nil {
		t.Fatalf("startAgentWithDeps: %v", err)
	}

	a, err := agents.Get("architect")
	if err != nil {
		t.Fatalf("agents.Get(architect): %v", err)
	}
	if a.Status != "idle" {
		t.Errorf("expected status=idle after Start, got %q", a.Status)
	}
}

// TestStart_SetsAgentNameEnvVar verifies that CT_AGENT_NAME is set in the
// session environment so cmdlog.Actor() reports the agent name, not the OS user.
func TestStart_SetsAgentNameEnvVar(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "test",
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	var capturedEnvVars map[string]string
	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(c session.AgentSessionConfig) error {
		capturedEnvVars = c.EnvVars
		return nil
	}

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, agentDir string) (string, error) {
		return agentDir + "/worktree", nil
	}

	if err := startAgentWithDeps(cfg, agents, "architect"); err != nil {
		t.Fatalf("startAgentWithDeps: %v", err)
	}

	if capturedEnvVars["CT_AGENT_NAME"] != "architect" {
		t.Errorf("CT_AGENT_NAME = %q, want %q", capturedEnvVars["CT_AGENT_NAME"], "architect")
	}
}

// TestStart_Daemon_DBError verifies that if agents.Get returns a non-not-found
// error (e.g. a transient DB failure), startAgentWithDeps surfaces that error
// rather than silently attempting re-registration.
func TestStart_Daemon_DBError(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	// Close the connection before passing to startAgentWithDeps so all DB
	// operations return a real driver error (not repo.ErrNotFound).
	conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot: tmpDir,
	}
	agents := repo.NewAgentRepo(conn, nil)

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	oldStartDaemon := startDaemonFn
	defer func() { startDaemonFn = oldStartDaemon }()
	startDaemonFn = func(_ *config.Config) error {
		t.Error("startDaemonFn must not be called when DB returns a real error")
		return nil
	}

	err = startAgentWithDeps(cfg, agents, "daemon")
	if err == nil {
		t.Fatal("expected error when DB is closed, got nil")
	}
}

// TestStart_WorkDirIsWorktree verifies that the session is created with the
// worktree path as WorkDir, not cfg.ProjectRoot.
func TestStart_WorkDirIsWorktree(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "test",
		Agents: config.AgentsConfig{
			Reviewer: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	const fakeWorktree = "/fake/reviewer/worktree"

	var capturedWorkDir string
	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(c session.AgentSessionConfig) error {
		capturedWorkDir = c.WorkDir
		return nil
	}

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, _ string) (string, error) {
		return fakeWorktree, nil
	}

	if err := startAgentWithDeps(cfg, agents, "reviewer"); err != nil {
		t.Fatalf("startAgentWithDeps: %v", err)
	}

	if capturedWorkDir != fakeWorktree {
		t.Errorf("session WorkDir = %q, want worktree path %q", capturedWorkDir, fakeWorktree)
	}
	if capturedWorkDir == tmpDir {
		t.Errorf("session WorkDir must not be cfg.ProjectRoot after nc-128 fix")
	}
}

// TestStopDaemon_KillsSession verifies that stopping the daemon kills its tmux
// session and sets agent status to idle.
func TestStopDaemon_KillsSession(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)

	// Register daemon in working state.
	if err := agents.Register("daemon", "daemon", nil); err != nil {
		t.Fatalf("Register daemon: %v", err)
	}
	if err := agents.UpdateStatus("daemon", "working"); err != nil {
		t.Fatalf("UpdateStatus working: %v", err)
	}

	sessionName := session.SessionName("daemon")

	var killedSession string
	oldKill := killSessionFn
	defer func() { killSessionFn = oldKill }()
	killSessionFn = func(s string) error {
		killedSession = s
		return nil
	}

	if err := stopDaemonWithDeps(agents, sessionName); err != nil {
		t.Fatalf("stopDaemonWithDeps: %v", err)
	}

	if killedSession != sessionName {
		t.Errorf("expected killSessionFn called with %q, got %q", sessionName, killedSession)
	}

	a, err := agents.Get("daemon")
	if err != nil {
		t.Fatalf("agents.Get(daemon): %v", err)
	}
	if a.Status != "idle" {
		t.Errorf("expected status=idle after stop, got %q", a.Status)
	}
}

// TestStopDaemon_SessionAlreadyGone verifies that if the daemon tmux session
// does not exist, Stop returns early without error.
func TestStopDaemon_SessionAlreadyGone(t *testing.T) {
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	killed := false
	oldKill := killSessionFn
	defer func() { killSessionFn = oldKill }()
	killSessionFn = func(_ string) error {
		killed = true
		return nil
	}

	// Stop with no real DB — the early-return path should not reach killSessionFn.
	// We pass args directly to Stop; it will call tmuxExistsFn first.
	if err := Stop([]string{"daemon"}); err != nil {
		t.Fatalf("Stop(daemon) with no session: %v", err)
	}

	if killed {
		t.Error("expected killSessionFn NOT to be called when session does not exist")
	}
}

// TestStop_missingArgs verifies that Stop returns an error when no agent name is provided.
func TestStop_missingArgs(t *testing.T) {
	if err := Stop([]string{}); err == nil {
		t.Fatal("expected error for Stop with no args")
	}
}

// TestStop_nonDaemon_notRunning verifies that Stop returns early with a friendly
// message when the named agent's tmux session does not exist.
func TestStop_nonDaemon_notRunning(t *testing.T) {
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	if err := Stop([]string{"architect"}); err != nil {
		t.Fatalf("Stop for non-running agent: %v", err)
	}
}

// TestStopDaemon_KillError verifies that stopDaemonWithDeps returns an error when
// killSessionFn fails.
func TestStopDaemon_KillError(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)
	if err := agents.Register("daemon", "daemon", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	oldKill := killSessionFn
	defer func() { killSessionFn = oldKill }()
	killSessionFn = func(_ string) error {
		return fmt.Errorf("simulated kill failure")
	}

	err = stopDaemonWithDeps(agents, session.SessionName("daemon"))
	if err == nil {
		t.Fatal("expected error from killSessionFn failure, got nil")
	}
}

// TestStopDaemon_DBUpdateWarning verifies that stopDaemonWithDeps continues
// (doesn't return early) when agents.UpdateStatus fails, issuing only a warning.
func TestStopDaemon_DBUpdateWarning(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	// Close the connection so all DB ops fail.
	conn.Close()

	agents := repo.NewAgentRepo(conn, nil)

	killed := false
	oldKill := killSessionFn
	defer func() { killSessionFn = oldKill }()
	killSessionFn = func(_ string) error {
		killed = true
		return nil
	}

	// DB is closed so UpdateStatus will warn, but kill must still happen.
	err = stopDaemonWithDeps(agents, session.SessionName("daemon"))
	if err != nil {
		t.Fatalf("expected nil error from stopDaemonWithDeps: %v", err)
	}
	if !killed {
		t.Error("expected killSessionFn to be called even when DB update failed")
	}
}

// TestStart_unknownAgent verifies that startAgentWithDeps returns an error for
// unrecognized agent names that are not in the registry and not artisan- prefixed.
func TestStart_unknownAgent(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{ProjectRoot: t.TempDir()}
	agents := repo.NewAgentRepo(conn, nil)

	err = startAgentWithDeps(cfg, agents, "bogus-agent")
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
}

// TestStart_artisan verifies that startAgentWithDeps handles an artisan-<specialty>
// agent, registering it and launching a session.
func TestStart_artisan(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Artisan: map[string]config.AgentConfig{
				"frontend": {Model: "claude-test"},
			},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(_ session.AgentSessionConfig) error { return nil }

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, agentDir string) (string, error) {
		return agentDir + "/worktree", nil
	}

	if err := startAgentWithDeps(cfg, agents, "artisan-frontend"); err != nil {
		t.Fatalf("startAgentWithDeps artisan-frontend: %v", err)
	}

	a, err := agents.Get("artisan-frontend")
	if err != nil {
		t.Fatalf("agents.Get(artisan-frontend): %v", err)
	}
	if a.Type != "artisan" {
		t.Errorf("expected type=artisan, got %q", a.Type)
	}
	if !a.Specialty.Valid || a.Specialty.String != "frontend" {
		t.Errorf("expected specialty=frontend, got %v", a.Specialty)
	}
}

// TestStart_artisanUnknownSpecialty verifies that startAgentWithDeps returns an
// error when the specialty is not in the config.
func TestStart_artisanUnknownSpecialty(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Agents: config.AgentsConfig{
			Artisan: map[string]config.AgentConfig{},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	err = startAgentWithDeps(cfg, agents, "artisan-nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown artisan specialty, got nil")
	}
}

// TestStart_nonDaemon_alreadyRunning verifies that startAgentWithDeps returns nil
// and prints a message when the agent's tmux session already exists (for non-daemon agents).
func TestStart_nonDaemon_alreadyRunning(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Stub both fns: worktree provisioning is a no-op, session always exists.
	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, agentDir string) (string, error) {
		return agentDir + "/worktree", nil
	}

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return true }

	// Session already running → startAgentWithDeps must return nil without launching.
	if err := startAgentWithDeps(cfg, agents, "architect"); err != nil {
		t.Fatalf("startAgentWithDeps with running session: %v", err)
	}
}

// TestStart_nonDaemon_alreadyRunning_resetsDeadStatus verifies that when the tmux
// session already exists but the DB shows the agent as dead (crash-and-restart
// scenario), startAgentWithDeps resets the status to idle.
func TestStart_nonDaemon_alreadyRunning_resetsDeadStatus(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Pre-register the architect agent and mark it dead.
	if err := agents.Register("architect", "architect", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.UpdateStatus("architect", repo.StatusDead); err != nil {
		t.Fatalf("UpdateStatus dead: %v", err)
	}

	// Stub tmux: session is live.
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return true }

	if err := startAgentWithDeps(cfg, agents, "architect"); err != nil {
		t.Fatalf("startAgentWithDeps with running dead session: %v", err)
	}

	a, err := agents.Get("architect")
	if err != nil {
		t.Fatalf("agents.Get(architect): %v", err)
	}
	if a.Status != repo.StatusIdle {
		t.Errorf("expected status=idle after start with dead DB entry, got %q", a.Status)
	}
}

// TestStop_nonDaemon_running verifies that Stop signals a running non-daemon agent
// and returns nil even when the database is unavailable (DB open failure is non-fatal
// in Stop's non-daemon path).
//
// A fake .company_town/ directory is created under t.TempDir() and the test CWD
// is moved there so db.FindProjectRoot() succeeds without a real project checkout.
func TestStop_nonDaemon_running(t *testing.T) {
	// Create a minimal fake project root so FindProjectRoot() succeeds in all
	// environments (local and CI both run this test inside the gtcmd package dir).
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, config.DirName), 0750); err != nil {
		t.Fatalf("creating .company_town: %v", err)
	}
	t.Chdir(projectRoot)

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	// Agent has a live tmux session.
	tmuxExistsFn = func(_ string) bool { return true }

	// "copper" is a prole — not in agentRegistry, no signal file, not daemon.
	// db.OpenFromWorkingDir() fails (no config.json in the fake root) which is
	// non-fatal in Stop's non-daemon path — it prints a warning and returns nil.
	if err := Stop([]string{"copper"}); err != nil {
		t.Fatalf("Stop(copper) with running session: %v", err)
	}
}
