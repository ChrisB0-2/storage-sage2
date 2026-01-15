package core

import (
	"testing"
	"time"
)

func TestAuditHelpers_SafetyReasonIsKeyOnly(t *testing.T) {
	it := PlanItem{
		Candidate: Candidate{
			Path:      "/x",
			Type:      TargetFile,
			SizeBytes: 1,
			ModTime:   time.Now(),
			Root:      "/root",
		},
		Decision: Decision{Allow: true, Reason: "age_ok", Score: 1},
		Safety:   SafetyVerdict{Allowed: false, Reason: "symlink_self:/x"},
	}

	evt := NewPlanAuditEvent("/root", ModeExecute, it)

	f, ok := evt.Fields["safety_reason"].(string)
	if !ok {
		t.Fatalf("safety_reason missing or not string: %#v", evt.Fields["safety_reason"])
	}
	if f != "symlink_self" {
		t.Fatalf("expected key-only safety_reason, got %q", f)
	}

}

func TestAuditHelpers_SafetyReasonIsKeyOnly_Execute(t *testing.T) {
	it := PlanItem{
		Candidate: Candidate{
			Path:      "/x",
			Type:      TargetFile,
			SizeBytes: 1,
			ModTime:   time.Now(),
			Root:      "/root",
		},
		Decision: Decision{Allow: true, Reason: "age_ok", Score: 1},
		Safety:   SafetyVerdict{Allowed: true, Reason: "ok"},
	}

	ar := ActionResult{
		Path:       "/x",
		Mode:       ModeExecute,
		Deleted:    false,
		BytesFreed: 0,
		Score:      1,
		Reason:     "safety_deny_execute:symlink_self:/x",
		FinishedAt: time.Now(),
	}

	evt := NewExecuteAuditEvent("/root", ModeExecute, it, ar)

	f, ok := evt.Fields["safety_reason"].(string)
	if !ok {
		t.Fatalf("safety_reason missing or not string: %#v", evt.Fields["safety_reason"])
	}
	if f != "symlink_self" {
		t.Fatalf("expected key-only safety_reason, got %q", f)
	}
}
