package commands

import (
	"fmt"
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

	stopCore([]string{"ct-daemon"}, t.TempDir(), killFn, sendKeysFn)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected daemon session killed, got %v", killed)
	}
	if len(sent) != 0 {
		t.Errorf("expected no sendKeys for daemon, got %v", sent)
	}
}

func TestStopCore_daemonKillErrorSurfaced(t *testing.T) {
	killFn := func(s string) error {
		return fmt.Errorf("tmux error")
	}
	// Should not panic; error is printed, not returned.
	// We just verify it doesn't call sendKeys either.
	sent := []string{}
	sendKeysFn := func(s, msg string) error {
		sent = append(sent, s)
		return nil
	}

	stopCore([]string{"ct-daemon"}, t.TempDir(), killFn, sendKeysFn)

	if len(sent) != 0 {
		t.Errorf("expected no sendKeys on kill failure, got %v", sent)
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

	stopCore([]string{"ct-mayor", "ct-prole-copper"}, t.TempDir(), killFn, sendKeysFn)

	if len(killed) != 0 {
		t.Errorf("expected no kills for non-daemon sessions, got %v", killed)
	}
	if len(sent) != 2 {
		t.Errorf("expected 2 sendKeys for non-daemon sessions, got %d", len(sent))
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

	stopCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, t.TempDir(), killFn, sendKeysFn)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected only daemon killed, got %v", killed)
	}
	if len(sent) != 2 {
		t.Errorf("expected 2 non-daemon sessions signaled, got %d", len(sent))
	}
}

// --- nukeCore tests ---

func TestNukeCore_killsDaemonSession(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	updated := []string{}
	updateStatus := func(name, status string) error {
		updated = append(updated, name)
		return nil
	}

	nukeCore([]string{"ct-daemon"}, killFn, updateStatus)

	if len(killed) != 1 || killed[0] != "ct-daemon" {
		t.Errorf("expected daemon session killed, got %v", killed)
	}
}

func TestNukeCore_skipsDBUpdateForDaemon(t *testing.T) {
	killFn := func(s string) error { return nil }
	updated := []string{}
	updateStatus := func(name, status string) error {
		updated = append(updated, name)
		return nil
	}

	nukeCore([]string{"ct-daemon"}, killFn, updateStatus)

	if len(updated) != 0 {
		t.Errorf("expected no DB update for daemon, got updates for: %v", updated)
	}
}

func TestNukeCore_updatesStatusForNonDaemonSessions(t *testing.T) {
	killFn := func(s string) error { return nil }
	updated := []string{}
	updateStatus := func(name, status string) error {
		updated = append(updated, name)
		return nil
	}

	nukeCore([]string{"ct-mayor", "ct-prole-copper"}, killFn, updateStatus)

	if len(updated) != 2 {
		t.Errorf("expected 2 DB updates, got %v", updated)
	}
}

func TestNukeCore_daemonSkippedOtherSessionsUpdated(t *testing.T) {
	killed := []string{}
	killFn := func(s string) error {
		killed = append(killed, s)
		return nil
	}
	updated := []string{}
	updateStatus := func(name, status string) error {
		updated = append(updated, name)
		return nil
	}

	nukeCore([]string{"ct-daemon", "ct-mayor", "ct-prole-copper"}, killFn, updateStatus)

	if len(killed) != 3 {
		t.Errorf("expected all 3 sessions killed, got %v", killed)
	}
	// daemon must not appear in DB updates
	for _, name := range updated {
		if name == "daemon" {
			t.Errorf("daemon must not have a DB status update, but it appeared in updates")
		}
	}
	if len(updated) != 2 {
		t.Errorf("expected 2 DB updates (mayor + copper), got %v", updated)
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
