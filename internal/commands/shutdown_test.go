package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- stopCore tests ---

func TestStopCore_killsDaemonSession(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	sent := []string{}
	sendKeysFn := func(s, msg string) error {
		sent = append(sent, s)
		return nil
	}
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected daemon session killed, got %v", killed)
	}
	if len(sent) != 0 {
		t.Errorf("expected no sendKeys for daemon, got %v", sent)
	}
}

func TestStopCore_marksDaemonDeadAfterKill(t *testing.T) {
	killFn := func(s string) error { return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if updated["daemon"] != "dead" {
		t.Errorf("expected daemon status set to 'dead', got %q", updated["daemon"])
	}
}

func TestStopCore_daemonKillErrorSurfaced(t *testing.T) {
	killFn := func(s string) error {
		return fmt.Errorf("tmux error")
	}
	sendKeysFn := func(s, msg string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	// Capture stdout to verify the error message is actually printed.
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if !strings.Contains(string(out), "error stopping daemon") {
		t.Errorf("expected error message printed to stdout, got: %q", string(out))
	}
	if strings.Contains(string(out), "stopped:") {
		t.Errorf("must not print 'stopped' when kill failed, got: %q", string(out))
	}
	if updated["daemon"] == "dead" {
		t.Errorf("must not mark daemon dead when kill failed")
	}
}

func TestStopCore_nonDaemonSessionsNotKilled(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	sent := []string{}
	sendKeysFn := func(s, msg string) error {
		sent = append(sent, s)
		return nil
	}
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	stopCore([]string{"ct-mayor", "ct-prole-copper"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(killed) != 0 {
		t.Errorf("expected no kills for non-daemon sessions, got %v", killed)
	}
	if len(sent) != 2 {
		t.Errorf("expected 2 sendKeys for non-daemon sessions, got %d", len(sent))
	}
	for _, name := range []string{"mayor", "prole-copper"} {
		if updated[name] != "idle" {
			t.Errorf("expected %q status 'idle', got %q", name, updated[name])
		}
	}
}

func TestStopCore_daemonKilledOtherSessionsSignaled(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	sent := []string{}
	sendKeysFn := func(s, msg string) error {
		sent = append(sent, s)
		return nil
	}
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	stopCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected only daemon killed, got %v", killed)
	}
	if len(sent) != 2 {
		t.Errorf("expected 2 non-daemon sessions signaled, got %d", len(sent))
	}
	if updated["daemon"] != "dead" {
		t.Errorf("expected daemon status 'dead', got %q", updated["daemon"])
	}
	for _, name := range []string{"mayor", "prole-copper"} {
		if updated[name] != "idle" {
			t.Errorf("expected %q status 'idle', got %q", name, updated[name])
		}
	}
}

func TestStopCore_withoutClean_preservesWorktrees(t *testing.T) {
	ctDir := t.TempDir()
	proleName := "copper"
	worktreeDir := filepath.Join(ctDir, "proles", proleName)
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatal(err)
	}

	removed := []string{}
	killFn := func(s string) error { return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	stopCore([]string{"ct-prole-copper"}, ctDir, false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(removed) != 0 {
		t.Errorf("expected no worktree removals without --clean, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("expected no worktree prune without --clean, got %v", pruned)
	}
}

func TestStopCore_withClean_removesProleWorktrees(t *testing.T) {
	ctDir := t.TempDir()

	removed := []string{}
	killFn := func(s string) error { return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	stopCore([]string{"ct-prole-copper", "ct-prole-iron"}, ctDir, true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(removed) != 2 {
		t.Errorf("expected 2 worktree removals with --clean, got %v", removed)
	}
	expectedCopper := filepath.Join(ctDir, "proles", "copper")
	expectedIron := filepath.Join(ctDir, "proles", "iron")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedCopper] {
		t.Errorf("expected copper worktree removed, got %v", removed)
	}
	if !found[expectedIron] {
		t.Errorf("expected iron worktree removed, got %v", removed)
	}
	if len(pruned) != 1 {
		t.Errorf("expected worktree prune called once, got %v", pruned)
	}
}

func TestStopCore_withClean_doesNotRemoveNonProleWorktrees(t *testing.T) {
	ctDir := t.TempDir()

	removed := []string{}
	killFn := func(s string) error { return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	stopCore([]string{"ct-mayor", "ct-daemon"}, ctDir, true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(removed) != 0 {
		t.Errorf("expected no removals for non-prole sessions, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("expected no prune when no prole dirs removed, got %v", pruned)
	}
}

// --- nukeCore tests ---

func TestNukeCore_killsDaemonSession(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	nukeCore([]string{"ct-daemon"}, t.TempDir(), "", killFn, updateStatus, removeAll, worktreePrune)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected daemon session killed, got %v", killed)
	}
}

func TestNukeCore_marksDaemonDeadAfterKill(t *testing.T) {
	killFn := func(s string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	nukeCore([]string{"ct-daemon"}, t.TempDir(), "", killFn, updateStatus, removeAll, worktreePrune)

	if updated["daemon"] != "dead" {
		t.Errorf("expected daemon status set to 'dead', got %q", updated["daemon"])
	}
}

func TestNukeCore_updatesStatusForAllSessions(t *testing.T) {
	killFn := func(s string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error {
		updated[name] = status
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	nukeCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), "", killFn, updateStatus, removeAll, worktreePrune)

	for _, name := range []string{"daemon", "mayor", "prole-copper"} {
		if updated[name] != "dead" {
			t.Errorf("expected %q status 'dead', got %q", name, updated[name])
		}
	}
}

func TestNukeCore_nilUpdateStatusWhenDBUnavailable(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	// nil updateStatus simulates DB unavailable — must not panic
	nukeCore([]string{"ct-daemon", "ct-mayor"}, t.TempDir(), "", killFn, nil, removeAll, worktreePrune)

	if len(killed) != 2 {
		t.Errorf("expected 2 kills, got %v", killed)
	}
}

func TestNukeCore_removesWorktreeDirsForProles(t *testing.T) {
	ctDir := t.TempDir()

	removed := []string{}
	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	nukeCore([]string{"ct-prole-copper", "ct-prole-iron"}, ctDir, "", killFn, updateStatus, removeAll, worktreePrune)

	// Expect copper worktree + iron worktree + bare clone = 3 removals.
	expectedCopper := filepath.Join(ctDir, "proles", "copper")
	expectedIron := filepath.Join(ctDir, "proles", "iron")
	expectedRepo := filepath.Join(ctDir, "repo.git")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedCopper] {
		t.Errorf("expected copper worktree removed, got %v", removed)
	}
	if !found[expectedIron] {
		t.Errorf("expected iron worktree removed, got %v", removed)
	}
	if !found[expectedRepo] {
		t.Errorf("expected bare clone removed in nuke, got %v", removed)
	}
	if len(pruned) != 1 {
		t.Errorf("expected worktree prune called once, got %v", pruned)
	}
}

func TestNukeCore_removesAgentWorktreesOnNuke(t *testing.T) {
	// nuke removes agent worktrees (not just prole worktrees).
	// ct-daemon has no worktree; ct-mayor has one at agents/mayor/worktree.
	ctDir := t.TempDir()

	removed := []string{}
	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	nukeCore([]string{"ct-daemon", "ct-mayor"}, ctDir, "", killFn, updateStatus, removeAll, worktreePrune)

	// daemon has no worktree — mayor does.
	expectedMayor := filepath.Join(ctDir, "agents", "mayor", "worktree")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedMayor] {
		t.Errorf("expected mayor worktree removed in nuke, removed=%v", removed)
	}
}

func TestNukeCore_removesBarecloneAfterKillingProles(t *testing.T) {
	ctDir := t.TempDir()

	removed := []string{}
	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	nukeCore([]string{"ct-prole-copper"}, ctDir, "", killFn, updateStatus, removeAll, worktreePrune)

	expectedRepo := filepath.Join(ctDir, "repo.git")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedRepo] {
		t.Errorf("expected bare clone removed in nuke, removed=%v", removed)
	}
}

func TestNukeCore_doesNotRemoveWorktreeWhenKillFails(t *testing.T) {
	ctDir := t.TempDir()

	killFn := func(s string) error { return fmt.Errorf("tmux error") }
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	nukeCore([]string{"ct-prole-copper"}, ctDir, "", killFn, updateStatus, removeAll, worktreePrune)

	if len(removed) != 0 {
		t.Errorf("expected no removal when kill failed, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("expected no prune when kill failed, got %v", pruned)
	}
}

// --- targeted nukeCore tests ---

func TestNukeCore_targetedProle_killsOnlyThatSession(t *testing.T) {
	ctDir := t.TempDir()

	killed := []string{}
	killFn := func(s string) error { killed = append(killed, s); return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error { updated[name] = status; return nil }
	removed := []string{}
	removeAll := func(p string) error { removed = append(removed, p); return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	sessions := []string{"ct-prole-copper", "ct-prole-iron", "ct-architect"}
	nukeCore(sessions, ctDir, "prole-copper", killFn, updateStatus, removeAll, worktreePrune)

	if len(killed) != 1 || killed[0] != "ct-prole-copper" {
		t.Errorf("expected only ct-prole-copper killed, got %v", killed)
	}
	if updated["prole-copper"] != "dead" {
		t.Errorf("expected prole-copper status=dead, got %q", updated["prole-copper"])
	}
	if updated["prole-iron"] != "" || updated["architect"] != "" {
		t.Errorf("targeted nuke must not touch other agents, got %v", updated)
	}
}

func TestNukeCore_targetedProle_removesWorktreeButNotBareClone(t *testing.T) {
	ctDir := t.TempDir()

	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error { removed = append(removed, p); return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	sessions := []string{"ct-prole-copper", "ct-prole-iron"}
	nukeCore(sessions, ctDir, "prole-copper", killFn, updateStatus, removeAll, worktreePrune)

	expectedWorktree := filepath.Join(ctDir, "proles", "copper")
	unexpectedBare := filepath.Join(ctDir, "repo.git")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedWorktree] {
		t.Errorf("expected copper worktree removed, got %v", removed)
	}
	if found[unexpectedBare] {
		t.Errorf("targeted nuke must not remove bare clone (shared by other proles), got %v", removed)
	}
	if len(pruned) != 1 {
		t.Errorf("expected worktree prune called once after targeted nuke, got %v", pruned)
	}
}

func TestNukeCore_targetedProle_missingSession_isNoop(t *testing.T) {
	ctDir := t.TempDir()

	killed := []string{}
	killFn := func(s string) error { killed = append(killed, s); return nil }
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error { removed = append(removed, p); return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	// "tin" is not in the running sessions list.
	nukeCore([]string{"ct-prole-copper"}, ctDir, "prole-tin", killFn, updateStatus, removeAll, worktreePrune)

	if len(killed) != 0 {
		t.Errorf("expected no kills for absent target, got %v", killed)
	}
	if len(removed) != 0 {
		t.Errorf("expected no removals for absent target, got %v", removed)
	}
}

func TestNukeCore_targetedBare_removesOnlyBareClone(t *testing.T) {
	ctDir := t.TempDir()

	killed := []string{}
	killFn := func(s string) error { killed = append(killed, s); return nil }
	removed := []string{}
	removeAll := func(p string) error { removed = append(removed, p); return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	// sessions passed but should be ignored for target=="bare"
	nukeCore([]string{"ct-prole-copper"}, ctDir, "bare", killFn, nil, removeAll, worktreePrune)

	if len(killed) != 0 {
		t.Errorf("bare target must not kill sessions, got %v", killed)
	}
	expectedBare := filepath.Join(ctDir, "repo.git")
	if len(removed) != 1 || removed[0] != expectedBare {
		t.Errorf("expected only bare clone removed, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("bare target must not run worktree prune, got %v", pruned)
	}
}

func TestNukeCore_targetedAgent_removesWorktreeButNotBareClone(t *testing.T) {
	ctDir := t.TempDir()

	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error { removed = append(removed, p); return nil }
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	sessions := []string{"ct-architect", "ct-mayor"}
	nukeCore(sessions, ctDir, "architect", killFn, updateStatus, removeAll, worktreePrune)

	expectedWorktree := filepath.Join(ctDir, "agents", "architect", "worktree")
	unexpectedBare := filepath.Join(ctDir, "repo.git")
	found := map[string]bool{}
	for _, p := range removed {
		found[p] = true
	}
	if !found[expectedWorktree] {
		t.Errorf("expected architect worktree removed, got %v", removed)
	}
	if found[unexpectedBare] {
		t.Errorf("targeted agent nuke must not remove bare clone, got %v", removed)
	}
}

// --- nukeCore return-value tests ---

func TestNukeCore_returnsCountOfKilled(t *testing.T) {
	killFn := func(s string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	sessions := []string{"ct-daemon", "ct-mayor", "ct-prole-copper"}
	got := nukeCore(sessions, t.TempDir(), "", killFn, updateStatus, removeAll, worktreePrune)
	if got != 3 {
		t.Errorf("expected killedCount=3, got %d", got)
	}
}

func TestNukeCore_unknownTarget_returnsZero(t *testing.T) {
	killCalled := false
	killFn := func(s string) error { killCalled = true; return nil }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	// Capture stdout to verify the "no running session" line is printed.
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	got := nukeCore([]string{"ct-prole-copper"}, t.TempDir(), "prole-nonexistent", killFn, updateStatus, removeAll, worktreePrune)

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if got != 0 {
		t.Errorf("expected killedCount=0 for unknown target, got %d", got)
	}
	if killCalled {
		t.Errorf("killFn must not be called when target does not match any session")
	}
	if !strings.Contains(string(out), `no running session for target "prole-nonexistent"`) {
		t.Errorf("expected 'no running session' message, got: %q", string(out))
	}
}

func TestNukeCore_killFailureDoesNotCount(t *testing.T) {
	killFn := func(s string) error { return fmt.Errorf("tmux error") }
	updateStatus := func(name, status string) error { return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	// Capture stdout to verify the error line is printed.
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	got := nukeCore([]string{"ct-prole-copper"}, t.TempDir(), "", killFn, updateStatus, removeAll, worktreePrune)

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if got != 0 {
		t.Errorf("expected killedCount=0 when kill fails, got %d", got)
	}
	if !strings.Contains(string(out), "error killing") {
		t.Errorf("expected 'error killing' message, got: %q", string(out))
	}
}

// --- filterSessions tests ---

func TestFilterSessions_exactMatch(t *testing.T) {
	all := []string{"ct-daemon", "ct-mayor", "ct-prole-copper"}
	got := filterSessions(all, "ct-daemon")
	if len(got) != 1 || got[0] != "ct-daemon" {
		t.Errorf("expected [ct-daemon], got %v", got)
	}
}

func TestFilterSessions_noMatch(t *testing.T) {
	all := []string{"ct-daemon", "ct-mayor"}
	got := filterSessions(all, "ct-prole-iron")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestFilterSessions_emptyInput(t *testing.T) {
	got := filterSessions(nil, "ct-daemon")
	if len(got) != 0 {
		t.Errorf("expected empty on nil input, got %v", got)
	}
}

func TestFilterSessions_noPrefixMatch(t *testing.T) {
	// "ct-prole" must not match "ct-prole-copper" — exact match only.
	all := []string{"ct-prole-copper", "ct-prole-iron"}
	got := filterSessions(all, "ct-prole")
	if len(got) != 0 {
		t.Errorf("expected no match for partial name, got %v", got)
	}
}

// --- Stop (targeted) tests via filterSessions ---

func TestStop_targetFilters_daemon(t *testing.T) {
	all := []string{"ct-daemon", "ct-mayor", "ct-prole-copper"}
	filtered := filterSessions(all, "ct-daemon")
	if len(filtered) != 1 || filtered[0] != "ct-daemon" {
		t.Errorf("expected [ct-daemon], got %v", filtered)
	}
}

func TestStop_targetFilters_prole(t *testing.T) {
	all := []string{"ct-daemon", "ct-mayor", "ct-prole-copper"}
	filtered := filterSessions(all, "ct-prole-copper")
	if len(filtered) != 1 || filtered[0] != "ct-prole-copper" {
		t.Errorf("expected [ct-prole-copper], got %v", filtered)
	}
}

func TestStop_targetFilters_artisan(t *testing.T) {
	all := []string{"ct-daemon", "ct-artisan-backend"}
	filtered := filterSessions(all, "ct-artisan-backend")
	if len(filtered) != 1 || filtered[0] != "ct-artisan-backend" {
		t.Errorf("expected [ct-artisan-backend], got %v", filtered)
	}
}

func TestStop_targetNotFound_returnsNil(t *testing.T) {
	all := []string{"ct-daemon", "ct-mayor"}
	filtered := filterSessions(all, "ct-prole-iron")
	if len(filtered) != 0 {
		t.Errorf("expected nil when not found, got %v", filtered)
	}
}

func TestStop_cleanNonProle_stopCoreStillRunsNoRemoval(t *testing.T) {
	// Targeted stop of daemon with --clean: daemon killed, no worktree removed.
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	sendKeysFn := func(s, msg string) error { return nil }
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	stopCore([]string{"ct-daemon"}, t.TempDir(), true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected daemon killed, got %v", killed)
	}
	if len(removed) != 0 {
		t.Errorf("--clean on daemon must not remove any worktree, got %v", removed)
	}
}

func TestStop_cleanProleTarget_removesWorktree(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	sent := []string{}
	sendKeysFn := func(s, msg string) error {
		sent = append(sent, s)
		return nil
	}
	updateStatus := func(name, status string) error { return nil }
	removed := []string{}
	removeAll := func(p string) error {
		removed = append(removed, p)
		return nil
	}
	pruned := []string{}
	worktreePrune := func(p string) error { pruned = append(pruned, p); return nil }

	ctDir := t.TempDir()
	stopCore([]string{"ct-prole-copper"}, ctDir, true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if len(killed) != 0 {
		t.Errorf("prole should be signaled, not killed, got kills: %v", killed)
	}
	if len(sent) != 1 || sent[0] != "ct-prole-copper" {
		t.Errorf("expected prole signaled, got %v", sent)
	}
	expectedWorktree := filepath.Join(ctDir, "proles", "copper")
	if len(removed) != 1 || removed[0] != expectedWorktree {
		t.Errorf("expected worktree %q removed, got %v", expectedWorktree, removed)
	}
	if len(pruned) != 1 {
		t.Errorf("expected worktree prune called once, got %v", pruned)
	}
}

// --- stopCore wait-for-stopped tests ---

func TestStopCore_agentReachesStoppedWithinTimeout_killsAndSetsDead(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error { killed = append(killed, s); return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error { updated[name] = status; return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	getStatus := func(string) (string, error) { return "stopped", nil }

	stopCore([]string{"ct-mayor"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, getStatus, time.Second)

	if len(killed) != 1 || killed[0] != "ct-mayor" {
		t.Errorf("expected ct-mayor killed after stopped signal, got %v", killed)
	}
	if updated["mayor"] != "dead" {
		t.Errorf("expected mayor status 'dead', got %q", updated["mayor"])
	}
}

func TestStopCore_agentDoesNotReachStopped_warnsAndLeaves(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error { killed = append(killed, s); return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error { updated[name] = status; return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	getStatus := func(string) (string, error) { return "working", nil }

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	stopCore([]string{"ct-mayor"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, getStatus, time.Millisecond)

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if len(killed) != 0 {
		t.Errorf("expected no kill when not stopped, got %v", killed)
	}
	if updated["mayor"] != "" {
		t.Errorf("expected no status update when not stopped, got %q", updated["mayor"])
	}
	if !strings.Contains(string(out), "did not reach 'stopped'") {
		t.Errorf("expected warning message, got: %q", string(out))
	}
}

func TestStopCore_nilGetStatus_fallsBackToIdle(t *testing.T) {
	killFn := func(s string) error { return nil }
	sendKeysFn := func(s, msg string) error { return nil }
	updated := map[string]string{}
	updateStatus := func(name, status string) error { updated[name] = status; return nil }
	removeAll := func(string) error { return nil }
	worktreePrune := func(string) error { return nil }

	stopCore([]string{"ct-mayor"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune, nil, 0)

	if updated["mayor"] != "idle" {
		t.Errorf("expected mayor status 'idle' when DB unavailable, got %q", updated["mayor"])
	}
}
