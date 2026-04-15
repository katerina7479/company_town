package gtcmd

import (
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
