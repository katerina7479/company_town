package health

import (
	"fmt"
	"testing"

	"github.com/katerina7479/company_town/internal/db"
)

// --- Runner tests ---

type alwaysOK struct{ name string }

func (c *alwaysOK) Name() string { return c.name }
func (c *alwaysOK) Run() Result  { return Result{Name: c.name, Status: StatusOK, Message: "ok"} }

type alwaysFail struct{ name string }

func (c *alwaysFail) Name() string { return c.name }
func (c *alwaysFail) Run() Result {
	return Result{Name: c.name, Status: StatusFail, Message: "failed"}
}

type alwaysWarn struct{ name string }

func (c *alwaysWarn) Name() string { return c.name }
func (c *alwaysWarn) Run() Result {
	return Result{Name: c.name, Status: StatusWarn, Message: "warning"}
}

func TestRunner_RunAll_empty(t *testing.T) {
	var r Runner
	results := r.RunAll()
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRunner_RunAll_executesAllChecks(t *testing.T) {
	var r Runner
	r.Register(&alwaysOK{name: "a"})
	r.Register(&alwaysOK{name: "b"})
	r.Register(&alwaysFail{name: "c"})

	results := r.RunAll()
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Status != StatusOK {
		t.Errorf("results[0]: expected OK, got %s", results[0].Status)
	}
	if results[2].Status != StatusFail {
		t.Errorf("results[2]: expected Fail, got %s", results[2].Status)
	}
}

func TestRunner_RunAll_preservesOrder(t *testing.T) {
	var r Runner
	r.Register(&alwaysOK{name: "first"})
	r.Register(&alwaysWarn{name: "second"})
	r.Register(&alwaysFail{name: "third"})

	results := r.RunAll()
	names := []string{results[0].Name, results[1].Name, results[2].Name}
	want := []string{"first", "second", "third"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("results[%d].Name = %q, want %q", i, names[i], want[i])
		}
	}
}

// --- Overall tests ---

func TestOverall_allOK(t *testing.T) {
	results := []Result{
		{Status: StatusOK},
		{Status: StatusOK},
	}
	if s := Overall(results); s != StatusOK {
		t.Errorf("expected OK, got %s", s)
	}
}

func TestOverall_warnDominatesOK(t *testing.T) {
	results := []Result{
		{Status: StatusOK},
		{Status: StatusWarn},
	}
	if s := Overall(results); s != StatusWarn {
		t.Errorf("expected Warn, got %s", s)
	}
}

func TestOverall_failDominatesAll(t *testing.T) {
	results := []Result{
		{Status: StatusOK},
		{Status: StatusWarn},
		{Status: StatusFail},
	}
	if s := Overall(results); s != StatusFail {
		t.Errorf("expected Fail, got %s", s)
	}
}

func TestOverall_empty(t *testing.T) {
	if s := Overall(nil); s != StatusOK {
		t.Errorf("expected OK for empty results, got %s", s)
	}
}

// --- DBCheck tests ---

func TestDBCheck_ok(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	defer conn.Close()

	c := NewDBCheck(conn)
	if c.Name() != "database" {
		t.Errorf("unexpected name: %q", c.Name())
	}

	result := c.Run()
	if result.Status != StatusOK {
		t.Errorf("expected OK for live db, got %s: %s", result.Status, result.Message)
	}
}

func TestDBCheck_fail(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	conn.Close() // close so Ping fails

	c := NewDBCheck(conn)
	result := c.Run()
	if result.Status != StatusFail {
		t.Errorf("expected Fail for closed db, got %s", result.Status)
	}
}

// --- SessionCheck tests ---

func TestSessionCheck_running(t *testing.T) {
	c := NewSessionCheck("ct-daemon", func(string) bool { return true })
	if c.Name() != "session:ct-daemon" {
		t.Errorf("unexpected name: %q", c.Name())
	}

	result := c.Run()
	if result.Status != StatusOK {
		t.Errorf("expected OK when session exists, got %s", result.Status)
	}
}

func TestSessionCheck_notRunning(t *testing.T) {
	c := NewSessionCheck("ct-daemon", func(string) bool { return false })
	result := c.Run()
	if result.Status != StatusWarn {
		t.Errorf("expected Warn when session absent, got %s", result.Status)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestSessionCheck_passesCorrectName(t *testing.T) {
	var got string
	c := NewSessionCheck("ct-mayor", func(name string) bool {
		got = name
		return true
	})
	c.Run()
	if got != "ct-mayor" {
		t.Errorf("expected sessionExists called with 'ct-mayor', got %q", got)
	}
}

// --- BinaryCheck tests ---

func TestBinaryCheck_found(t *testing.T) {
	// "sh" is available on all Unix systems
	c := NewBinaryCheck("sh")
	if c.Name() != "binary:sh" {
		t.Errorf("unexpected name: %q", c.Name())
	}

	result := c.Run()
	if result.Status != StatusOK {
		t.Errorf("expected OK for sh binary, got %s: %s", result.Status, result.Message)
	}
}

func TestBinaryCheck_notFound(t *testing.T) {
	c := NewBinaryCheck(fmt.Sprintf("nonexistent-binary-%d", 12345))
	result := c.Run()
	if result.Status != StatusFail {
		t.Errorf("expected Fail for missing binary, got %s", result.Status)
	}
}
