package commands

import (
	"fmt"
	"io"
	"os"
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

	stopCore([]string{"ct-daemon"}, t.TempDir(), killFn, sendKeysFn, updateStatus)

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

	stopCore([]string{"ct-daemon"}, t.TempDir(), killFn, sendKeysFn, updateStatus)

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

	// Capture stdout to verify the error message is actually printed.
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	stopCore([]string{"ct-daemon"}, t.TempDir(), killFn, sendKeysFn, updateStatus)

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

	stopCore([]string{"ct-mayor", "ct-prole-copper"}, t.TempDir(), killFn, sendKeysFn, updateStatus)

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

	stopCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), killFn, sendKeysFn, updateStatus)

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

	nukeCore([]string{"ct-daemon"}, killFn, updateStatus)

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

	nukeCore([]string{"ct-daemon"}, killFn, updateStatus)

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

	nukeCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, killFn, updateStatus)

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

	// nil updateStatus simulates DB unavailable — must not panic
	nukeCore([]string{"ct-daemon", "ct-mayor"}, killFn, nil)

	if len(killed) != 2 {
		t.Errorf("expected 2 kills, got %v", killed)
	}
}
