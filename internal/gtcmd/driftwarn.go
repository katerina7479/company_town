package gtcmd

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

const driftWarnCooldown = 60 * time.Second

// WarnDriftOnStdErr checks the caller's agent row for drift and prints a
// one-line warning to stderr for each issue found, subject to a per-(agent,
// warning) rate limit. It never aborts and never returns an error — all
// failures are silently swallowed. The check is skipped when CT_AGENT_NAME is
// not set or when the project root cannot be found.
//
// Commands that are purely read-only (status, check list/history) should
// pass skipCmd=true to avoid noisy output on introspection commands. Agent
// lifecycle verbs (accept, release, do, status) should also pass skipCmd=true
// because they are actively correcting state.
func WarnDriftOnStdErr(skipCmd bool) {
	if skipCmd {
		return
	}

	agentName := os.Getenv("CT_AGENT_NAME")
	if agentName == "" {
		return // not running as an agent
	}

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return // not in a project
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return // DB not available — skip silently
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)
	issues := repo.NewIssueRepo(conn, nil)

	runDir := filepath.Join(config.CompanyTownDir(projectRoot), "run")
	warnDriftToWriter(os.Stderr, agentName, runDir, agents, issues, cfg.TicketPrefix, time.Now)
}

// warnDriftToWriter is the injectable core of WarnDriftOnStdErr. It checks
// for drift entries belonging to agentName and emits rate-limited warnings to w.
func warnDriftToWriter(w io.Writer, agentName, runDir string, agents *repo.AgentRepo, issues *repo.IssueRepo, prefix string, nowFn func() time.Time) {
	entries, err := repo.CheckDrift(agents, issues, prefix)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.AgentName != agentName {
			continue // only warn about the caller's own row
		}
		if !shouldEmitDriftWarning(runDir, agentName, e.Reason, nowFn) {
			continue
		}
		fmt.Fprintf(w, "warning: %s\n", e.Reason)
		recordDriftWarning(runDir, agentName, e.Reason, nowFn)
	}
}

// shouldEmitDriftWarning returns true if the warning has not been emitted
// within the cooldown window. Errors reading the state file default to true
// (emit the warning).
func shouldEmitDriftWarning(runDir, agentName, reason string, nowFn func() time.Time) bool {
	path := driftWarnPath(runDir, agentName, reason)
	data, err := os.ReadFile(path)
	if err != nil {
		return true // no prior record
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return true
	}
	last := time.Unix(ts, 0)
	return nowFn().Sub(last) >= driftWarnCooldown
}

// recordDriftWarning writes the current timestamp to the rate-limit file.
//
// State is stored as one file per (agent, warning-hash) under runDir. This is
// filesystem-atomic and needs no prune-on-read pass (unlike a JSON-map approach).
// Old files are harmless and can be cleaned by a maintenance cron if needed.
func recordDriftWarning(runDir, agentName, reason string, nowFn func() time.Time) {
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return
	}
	path := driftWarnPath(runDir, agentName, reason)
	ts := strconv.FormatInt(nowFn().Unix(), 10)
	_ = os.WriteFile(path, []byte(ts), 0644)
}

// driftWarnPath returns the rate-limit file path for a given agent+reason.
// The reason is hashed to keep filenames short and filesystem-safe.
func driftWarnPath(runDir, agentName, reason string) string {
	h := sha256.Sum256([]byte(reason))
	hash := fmt.Sprintf("%x", h[:4]) // 8 hex chars
	return filepath.Join(runDir, fmt.Sprintf("drift-%s-%s.ts", agentName, hash))
}
