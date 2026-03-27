package repo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/db"
)

func newTestQualityRepo(t *testing.T) *QualityMetricRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewQualityMetricRepo(conn)
}

func TestQualityMetricRepo_Record(t *testing.T) {
	r := newTestQualityRepo(t)

	m := &QualityMetric{
		CheckName: "go-test",
		Status:    "pass",
		Output:    "ok ./...",
		RunAt:     time.Now(),
	}
	if err := r.Record(m); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestQualityMetricRepo_ListRecent(t *testing.T) {
	r := newTestQualityRepo(t)

	now := time.Now()
	for i, name := range []string{"check-a", "check-b", "check-c"} {
		if err := r.Record(&QualityMetric{
			CheckName: name,
			Status:    "pass",
			RunAt:     now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	results, err := r.ListRecent(10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Newest first
	if results[0].CheckName != "check-c" {
		t.Errorf("expected newest first, got %q", results[0].CheckName)
	}
}

func TestQualityMetricRepo_ListRecent_limit(t *testing.T) {
	r := newTestQualityRepo(t)

	now := time.Now()
	for i := 0; i < 5; i++ {
		r.Record(&QualityMetric{
			CheckName: "check",
			Status:    "pass",
			RunAt:     now.Add(time.Duration(i) * time.Second),
		})
	}

	results, err := r.ListRecent(3)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 (limit), got %d", len(results))
	}
}

func TestQualityMetricRepo_ListByCheck(t *testing.T) {
	r := newTestQualityRepo(t)

	now := time.Now()
	for i := 0; i < 3; i++ {
		r.Record(&QualityMetric{CheckName: "go-test", Status: "pass", RunAt: now.Add(time.Duration(i) * time.Second)})
	}
	r.Record(&QualityMetric{CheckName: "go-vet", Status: "pass", RunAt: now})

	results, err := r.ListByCheck("go-test", 10)
	if err != nil {
		t.Fatalf("ListByCheck: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results for go-test, got %d", len(results))
	}
	for _, res := range results {
		if res.CheckName != "go-test" {
			t.Errorf("unexpected check name %q", res.CheckName)
		}
	}
}

func TestQualityMetricRepo_ListByCheck_limit(t *testing.T) {
	r := newTestQualityRepo(t)

	now := time.Now()
	for i := 0; i < 5; i++ {
		r.Record(&QualityMetric{CheckName: "go-test", Status: "pass", RunAt: now.Add(time.Duration(i) * time.Second)})
	}

	results, err := r.ListByCheck("go-test", 2)
	if err != nil {
		t.Fatalf("ListByCheck: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 (limit), got %d", len(results))
	}
}

func TestQualityMetricRepo_ListByCheck_empty(t *testing.T) {
	r := newTestQualityRepo(t)

	results, err := r.ListByCheck("nonexistent", 10)
	if err != nil {
		t.Fatalf("ListByCheck: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestQualityMetricRepo_LatestPerCheck(t *testing.T) {
	r := newTestQualityRepo(t)

	now := time.Now()
	// Two runs of go-test, one of go-vet
	r.Record(&QualityMetric{CheckName: "go-test", Status: "fail", Output: "first run", RunAt: now})
	r.Record(&QualityMetric{CheckName: "go-test", Status: "pass", Output: "second run", RunAt: now.Add(time.Second)})
	r.Record(&QualityMetric{CheckName: "go-vet", Status: "pass", Output: "vet ok", RunAt: now})

	latest, err := r.LatestPerCheck()
	if err != nil {
		t.Fatalf("LatestPerCheck: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected 2 (one per check), got %d", len(latest))
	}

	byName := make(map[string]*QualityMetric)
	for _, m := range latest {
		byName[m.CheckName] = m
	}

	if byName["go-test"] == nil {
		t.Fatal("missing go-test in LatestPerCheck")
	}
	if byName["go-test"].Status != "pass" {
		t.Errorf("go-test: expected latest status=pass, got %q", byName["go-test"].Status)
	}
	if byName["go-test"].Output != "second run" {
		t.Errorf("go-test: expected latest output=%q, got %q", "second run", byName["go-test"].Output)
	}
}

func TestQualityMetricRepo_LatestPerCheck_empty(t *testing.T) {
	r := newTestQualityRepo(t)

	results, err := r.LatestPerCheck()
	if err != nil {
		t.Fatalf("LatestPerCheck: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestQualityMetricRepo_Record_withValue(t *testing.T) {
	r := newTestQualityRepo(t)

	val := 87.5
	m := &QualityMetric{
		CheckName: "coverage",
		Status:    "pass",
		Output:    "87.5%",
		Value:     sql.NullFloat64{Float64: val, Valid: true},
		RunAt:     time.Now(),
	}
	if err := r.Record(m); err != nil {
		t.Fatalf("Record: %v", err)
	}

	results, err := r.ListByCheck("coverage", 1)
	if err != nil {
		t.Fatalf("ListByCheck: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Value.Valid {
		t.Error("Value should be valid (non-NULL)")
	}
	if results[0].Value.Float64 != val {
		t.Errorf("Value = %v, want %v", results[0].Value.Float64, val)
	}
}

func TestQualityMetricRepo_Record_withError(t *testing.T) {
	r := newTestQualityRepo(t)

	m := &QualityMetric{
		CheckName: "go-test",
		Status:    "error",
		Error:     "command not found: gotestsum",
		RunAt:     time.Now(),
	}
	if err := r.Record(m); err != nil {
		t.Fatalf("Record: %v", err)
	}

	results, err := r.ListByCheck("go-test", 1)
	if err != nil {
		t.Fatalf("ListByCheck: %v", err)
	}
	if results[0].Error != "command not found: gotestsum" {
		t.Errorf("Error = %q, want %q", results[0].Error, "command not found: gotestsum")
	}
}

func TestQualityMetricRepo_Record_statusVariants(t *testing.T) {
	r := newTestQualityRepo(t)

	statuses := []string{"pass", "fail", "warn", "error"}
	now := time.Now()
	for i, s := range statuses {
		if err := r.Record(&QualityMetric{
			CheckName: "check-" + s,
			Status:    s,
			RunAt:     now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("Record status=%s: %v", s, err)
		}
	}

	results, err := r.ListRecent(10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
}

