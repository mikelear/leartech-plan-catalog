package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Wire contract with plan-api on a POST /plans 409 response, mirroring
// leartech-plan-api/internal/plans/resubmit.go (PR #33). Field name is
// `result`; the string constants below are the ONLY recognised verdicts.
// A response body that does not carry `result`, or carries a value not in
// this set, MUST fail the submit — a reader that cannot determine an
// outcome must say so rather than fall back to the old
// treat-409-as-success behaviour.
//
// Body shape:
//
//	{
//	  "error":   "<human message>",
//	  "result":  "identical" | "differs" | "conflict_running" | "unknown",
//	  "phase":   "<current plan phase>",
//	  "paused":  <bool>,
//	  "changed": [<top-level spec fields that differ>],  // omitted for identical
//	}
const (
	// resubmitIdentical — plan-api compared the submitted brief to the
	// on-cluster plan and they matched. Genuine SKIP, exit 0.
	resubmitIdentical = "identical"
	// resubmitDiffers — the submitted brief differs AND plan-api chose
	// NOT to apply it (plan-api never mutates a plan on resubmit). Hard
	// failure: the file changed and did not reach the cluster.
	// Remediation is delete the on-cluster Plan CR and re-submit.
	resubmitDiffers = "differs"
	// resubmitConflictRunning — the brief differs AND the on-cluster
	// plan is mid-flight (neither paused nor terminal). Hard failure:
	// mutating a running plan is forbidden.
	resubmitConflictRunning = "conflict_running"
	// resubmitUnknown — plan-api could not compute a verdict (typically
	// a transient Get failure after AlreadyExists). Hard failure — the
	// step must not proceed as if the submit succeeded.
	resubmitUnknown = "unknown"
)

// resubmitBody is the projection of plan-api's 409 response body this tool
// reads. Extra fields are ignored (json.Unmarshal drops unknown keys),
// which keeps this decoder tolerant of plan-api adding new envelope
// fields without breaking the release step.
type resubmitBody struct {
	Error   string   `json:"error"`
	Result  string   `json:"result"`
	Phase   string   `json:"phase"`
	Paused  bool     `json:"paused"`
	Changed []string `json:"changed"`
}

// classifyResubmit reads plan-api's 409 body and decides whether this
// submit is a genuine idempotent SKIP or a hard failure. Returns (ok,
// human message). The message is the operator's ONLY artefact when the
// release step fails on main (the PipelineRun is reaped shortly after),
// so it must name the plan, say the amendment was not applied, and
// state the remediation (delete the on-cluster Plan CR and re-submit).
// When plan-api's body describes what changed, include it — it lets the
// reader decide whether the divergence is intentional before deleting.
//
// A body that does not decode, a body missing `result`, and a body
// carrying a `result` this code does not recognise all fail — the same
// rule that made run_report render UNAVAILABLE instead of 0 rather than
// silently pretend to know an outcome.
func classifyResubmit(name, file, body string) (ok bool, msg string) {
	var rb resubmitBody
	if body == "" {
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 with an empty body — plan-api did not report a verdict; the amendment was NOT applied. Remediation: delete the on-cluster Plan CR and re-submit.",
			name, file)
	}
	if err := json.Unmarshal([]byte(body), &rb); err != nil {
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 with an unparseable body (%v); the amendment was NOT applied. Remediation: delete the on-cluster Plan CR and re-submit. Body: %s",
			name, file, err, truncate(body, 300))
	}

	switch rb.Result {
	case resubmitIdentical:
		// The on-cluster plan already matches this file. Nothing to do;
		// a resubmit of the same brief is a safe no-op.
		return true, fmt.Sprintf("  SKIP %s (%s) -> HTTP 409 identical (phase=%s, paused=%t)",
			name, file, rb.Phase, rb.Paused)

	case resubmitDiffers:
		// The file changed and plan-api chose NOT to apply it. This
		// tool must not either — delete-then-recreate would destroy a
		// paused or terminal plan's history. Fail loudly with the
		// remediation the operator needs.
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 differs%s: the on-cluster Plan differs from the submitted file and plan-api DID NOT apply the amendment. Remediation: delete the on-cluster Plan CR (`kubectl -n jx-staging delete plan %s`) and re-submit. plan-api reason: %q",
			name, file, changedSuffix(rb.Changed), name, rb.Error)

	case resubmitConflictRunning:
		// A running plan's spec cannot be swapped without stomping the
		// AgentRun executing against the old brief. Same failure class
		// as `differs`; the remediation is the same but the OPERATOR
		// must decide whether to cancel-and-restart or wait.
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 conflict_running%s: the on-cluster Plan is mid-flight (phase=%s, paused=%t) and plan-api refused to apply the amendment. Remediation: wait for the plan to reach a terminal phase (or cancel it), then delete the on-cluster Plan CR (`kubectl -n jx-staging delete plan %s`) and re-submit. plan-api reason: %q",
			name, file, changedSuffix(rb.Changed), rb.Phase, rb.Paused, name, rb.Error)

	case "":
		// No verdict at all. plan-api added the wire contract in PR #33
		// (mikelear/leartech-plan-api); a plan-api older than that
		// would fall into this branch and now fails the step. Both
		// clusters already run a version containing the wire contract.
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 with no `result` verdict on the body; the amendment was NOT applied and this tool cannot determine why. Remediation: check plan-api is on a build containing mikelear/leartech-plan-api#33 (or later); then delete the on-cluster Plan CR and re-submit. Body: %s",
			name, file, truncate(body, 300))

	default:
		// A `result` value this build does not recognise. Fail rather
		// than fall through — a reader that cannot determine the
		// outcome must say so.
		return false, fmt.Sprintf(
			"  FAIL %s (%s) -> HTTP 409 with an unrecognised `result` verdict %q (this tool recognises: identical, differs, conflict_running, unknown); the amendment was NOT applied. Remediation: delete the on-cluster Plan CR and re-submit. Body: %s",
			name, file, rb.Result, truncate(body, 300))
	}
}

// changedSuffix renders plan-api's `changed` list into a parenthesised
// tail for the failure message, e.g. " (spec fields changed: steps)".
// Empty list → empty string, so the message stays clean when the field
// is absent.
func changedSuffix(changed []string) string {
	if len(changed) == 0 {
		return ""
	}
	return " (spec fields changed: " + strings.Join(changed, ", ") + ")"
}
