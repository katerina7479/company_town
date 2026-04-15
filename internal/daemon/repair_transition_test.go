package daemon

import (
	"testing"
)

func TestRepairTransition_successSetsStatusAndReason(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	id, err := d.issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok := d.repairTransition(id, "repairing", "CI: lint")
	if !ok {
		t.Fatal("expected repairTransition to return true on success")
	}

	got, err := d.issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want %q", got.Status, "repairing")
	}
	if !got.RepairReason.Valid || got.RepairReason.String != "CI: lint" {
		t.Errorf("repair_reason: got valid=%v value=%q, want %q",
			got.RepairReason.Valid, got.RepairReason.String, "CI: lint")
	}
}

func TestRepairTransition_mergeConflictStatus(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	id, _ := d.issues.Create("conflict ticket", "task", nil, nil, nil)

	ok := d.repairTransition(id, "merge_conflict", "merge conflict with main")
	if !ok {
		t.Fatal("expected repairTransition to return true")
	}

	got, _ := d.issues.Get(id)
	if got.Status != "merge_conflict" {
		t.Errorf("status: got %q, want %q", got.Status, "merge_conflict")
	}
	if !got.RepairReason.Valid || got.RepairReason.String != "merge conflict with main" {
		t.Errorf("repair_reason: got valid=%v value=%q", got.RepairReason.Valid, got.RepairReason.String)
	}
}

func TestRepairTransition_unknownIssueReturnsFalse(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	ok := d.repairTransition(9999, "repairing", "some reason")
	if ok {
		t.Error("expected repairTransition to return false for nonexistent ticket")
	}
}
