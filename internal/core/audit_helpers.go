package core

import "time"

// Canonical audit actions
const (
	AuditActionPlan    = "plan"
	AuditActionExecute = "execute"
)

// NewPlanAuditEvent standardizes plan-time audit shape.
func NewPlanAuditEvent(root string, mode Mode, it PlanItem) AuditEvent {
	return AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: AuditActionPlan,
		Path:   it.Candidate.Path,
		Fields: map[string]any{
			"root":          root,
			"mode":          string(mode),
			"type":          string(it.Candidate.Type),
			"size_bytes":    it.Candidate.SizeBytes,
			"mod_time":      it.Candidate.ModTime,
			"score":         it.Decision.Score,
			"policy_allow":  it.Decision.Allow,
			"policy_reason": it.Decision.Reason,
			"safety_allow":  it.Safety.Allowed,
			"safety_reason": reasonKey(it.Safety.Reason),
		},
	}
}

// NewExecuteAuditEvent standardizes execute-time audit shape.
func NewExecuteAuditEvent(root string, mode Mode, it PlanItem, ar ActionResult) AuditEvent {
	resultAllow := ar.Reason == "would_delete" || ar.Reason == "deleted"

	return AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: AuditActionExecute,
		Path:   it.Candidate.Path,
		Fields: map[string]any{
			"root":          root,
			"mode":          string(mode),
			"type":          string(it.Candidate.Type),
			"size_bytes":    it.Candidate.SizeBytes,
			"mod_time":      it.Candidate.ModTime,
			"score":         it.Decision.Score,
			"policy_allow":  it.Decision.Allow,
			"policy_reason": it.Decision.Reason,
			"safety_allow":  it.Safety.Allowed,
			"safety_reason": reasonKey(executeSafetyReason(it, ar)),

			// Execute-only fields
			"result_allow":  resultAllow,
			"result_reason": ar.Reason,
			"deleted":       ar.Deleted,
			"bytes_freed":   ar.BytesFreed,
		},
	}
}

// reasonKey collapses reasons like "symlink_self:/path/to/file" -> "symlink_self"
func reasonKey(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i]
		}
	}
	return s
}

// executeSafetyReason returns the best safety reason for execute-time audit.
// If execution was denied due to execute-time safety, prefer the reason carried in ar.Reason
// (e.g. "safety_deny_execute:symlink_self:/x"). Otherwise fall back to plan-time safety.
func executeSafetyReason(it PlanItem, ar ActionResult) string {
	const pfx = "safety_deny_execute:"
	if len(ar.Reason) >= len(pfx) && ar.Reason[:len(pfx)] == pfx {
		// everything after the prefix is the safety reason (may include ":/path" detail)
		return ar.Reason[len(pfx):]
	}
	return it.Safety.Reason
}
