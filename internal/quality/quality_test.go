package quality

import (
	"testing"
	"time"
)

func float64ptr(v float64) *float64 { return &v }

func TestBaseline_Pass_allPass(t *testing.T) {
	b := &Baseline{
		RunAt: time.Now(),
		Results: []Result{
			{CheckName: "tests", Status: StatusPass},
			{CheckName: "vet", Status: StatusPass},
		},
	}
	if !b.Pass() {
		t.Error("expected Pass()=true when all results pass")
	}
}

func TestBaseline_Pass_anyFail(t *testing.T) {
	b := &Baseline{
		Results: []Result{
			{CheckName: "tests", Status: StatusPass},
			{CheckName: "vet", Status: StatusFail},
		},
	}
	if b.Pass() {
		t.Error("expected Pass()=false when any result fails")
	}
}

func TestBaseline_Pass_empty(t *testing.T) {
	b := &Baseline{}
	if !b.Pass() {
		t.Error("expected Pass()=true for empty baseline")
	}
}

func TestBaseline_FailedChecks_returnsNonPassing(t *testing.T) {
	b := &Baseline{
		Results: []Result{
			{CheckName: "tests", Status: StatusPass},
			{CheckName: "vet", Status: StatusFail},
			{CheckName: "coverage", Status: StatusWarn},
			{CheckName: "lint", Status: StatusError},
		},
	}
	failed := b.FailedChecks()
	if len(failed) != 3 {
		t.Fatalf("expected 3 failed checks, got %d", len(failed))
	}
	names := map[string]bool{}
	for _, r := range failed {
		names[r.CheckName] = true
	}
	for _, want := range []string{"vet", "coverage", "lint"} {
		if !names[want] {
			t.Errorf("expected %q in failed checks", want)
		}
	}
}

func TestBaseline_FailedChecks_allPass(t *testing.T) {
	b := &Baseline{
		Results: []Result{
			{CheckName: "tests", Status: StatusPass},
		},
	}
	if failed := b.FailedChecks(); len(failed) != 0 {
		t.Errorf("expected 0 failed checks, got %d", len(failed))
	}
}

func TestResult_ValueField(t *testing.T) {
	// pass_fail check has nil Value
	pf := Result{CheckName: "vet", Status: StatusPass}
	if pf.Value != nil {
		t.Errorf("expected nil Value for pass_fail result, got %v", pf.Value)
	}

	// metric check has non-nil Value
	metric := Result{
		CheckName: "coverage",
		Status:    StatusPass,
		Value:     float64ptr(87.5),
	}
	if metric.Value == nil || *metric.Value != 87.5 {
		t.Errorf("expected Value=87.5, got %v", metric.Value)
	}
}

func TestCheckType_constants(t *testing.T) {
	if CheckTypePassFail != "pass_fail" {
		t.Errorf("unexpected CheckTypePassFail value: %q", CheckTypePassFail)
	}
	if CheckTypeMetric != "metric" {
		t.Errorf("unexpected CheckTypeMetric value: %q", CheckTypeMetric)
	}
}

func TestStatus_constants(t *testing.T) {
	for _, s := range []Status{StatusPass, StatusFail, StatusWarn, StatusError} {
		if s == "" {
			t.Error("unexpected empty Status constant")
		}
	}
}
