package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-daemon"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-mayor", "ct-prole-copper"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-prole-copper"}, ctDir, false, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-prole-copper", "ct-prole-iron"}, ctDir, true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	stopCore([]string{"ct-mayor", "ct-conductor", "ct-daemon"}, ctDir, true, killFn, sendKeysFn, updateStatus, removeAll, worktreePrune)

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

	nukeCore([]string{"ct-daemon"}, t.TempDir(), killFn, updateStatus, removeAll, worktreePrune)

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

	nukeCore([]string{"ct-daemon"}, t.TempDir(), killFn, updateStatus, removeAll, worktreePrune)

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

	nukeCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), killFn, updateStatus, removeAll, worktreePrune)

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
	nukeCore([]string{"ct-daemon", "ct-mayor"}, t.TempDir(), killFn, nil, removeAll, worktreePrune)

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

	nukeCore([]string{"ct-prole-copper", "ct-prole-iron"}, ctDir, killFn, updateStatus, removeAll, worktreePrune)

	if len(removed) != 2 {
		t.Errorf("expected 2 worktree removals, got %v", removed)
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

func TestNukeCore_doesNotRemoveWorktreesForNonProles(t *testing.T) {
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

	nukeCore([]string{"ct-daemon", "ct-mayor", "ct-conductor"}, ctDir, killFn, updateStatus, removeAll, worktreePrune)

	if len(removed) != 0 {
		t.Errorf("expected no removals for non-prole sessions, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("expected no prune when no proles removed, got %v", pruned)
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

	nukeCore([]string{"ct-prole-copper"}, ctDir, killFn, updateStatus, removeAll, worktreePrune)

	if len(removed) != 0 {
		t.Errorf("expected no removal when kill failed, got %v", removed)
	}
	if len(pruned) != 0 {
		t.Errorf("expected no prune when kill failed, got %v", pruned)
	}
}
