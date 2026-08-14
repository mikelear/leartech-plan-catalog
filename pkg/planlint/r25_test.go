package planlint_test

// R25 + R22-prefix regression tests. Both rules exist because of a live failure on
// 2026-08-12: artifact-rest-and-mcp/verify-roundtrip was a kind: check step on
// leartech-agent-go. It reported "six of six assertion classes un-executed → verdict
// FAIL" in prose, and the session still exited 0 — a dev agent cannot turn a verdict
// into an exit code. Nothing in R1–R24 flagged it: R21 was satisfied (the dev shape
// was correct) and R22 matched only the exact string "leartech-agent-infra".

import (
	"strings"
	"testing"

	"github.com/mikelear/leartech-plan-catalog/pkg/planlint"
)

func lintYAML(t *testing.T, y string) []string {
	t.Helper()
	res, _ := planlint.LintBytes("plans/t.yaml", []byte(y))
	var out []string
	// R25 is a WARNING (see lintVerdictStep); R22 is an error. Collect both so the
	// tests assert on the rule firing, not on its current severity.
	for _, f := range append(append([]planlint.Finding{}, res.Errors...), res.Warns...) {
		out = append(out, f.Rule+": "+f.Message)
	}
	return out
}

func hasRule(fs []string, rule string) bool {
	for _, f := range fs {
		if strings.HasPrefix(f, rule+":") {
			return true
		}
	}
	return false
}

const devCheckPlan = `apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: t
spec:
  steps:
    - name: verify
      kind: check
      agentType: leartech-agent-go
      inputs:
        name: verify
        repo: mikelear/x
        branch: feat/v
        goal: Prove it works. FAIL (exit non-zero) if any assertion fails.
`

// The exact shape that shipped and could not fail.
func TestR25FlagsDevAgentVerdictStep(t *testing.T) {
	fs := lintYAML(t, devCheckPlan)
	if !hasRule(fs, "R25") {
		t.Errorf("R25 did not fire on kind:check + leartech-agent-go — the shape that "+
			"silently exits 0 while reporting FAIL in prose.\n  got: %v", fs)
	}
}

// A dev agent doing kind:pr is the normal case and must stay clean.
func TestR25IgnoresDevAgentPRStep(t *testing.T) {
	fs := lintYAML(t, strings.Replace(devCheckPlan, "kind: check", "kind: pr", 1))
	if hasRule(fs, "R25") {
		t.Errorf("R25 fired on a normal dev kind:pr step: %v", fs)
	}
}

// The bypass: R22 matched == "leartech-agent-infra", so the MORE capable
// leartech-agent-infra-go slipped through the rule meant to police privileged infra.
func TestR22CatchesInfraGoVariant(t *testing.T) {
	y := `apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: t
spec:
  steps:
    - name: gate
      kind: check
      agentType: leartech-agent-infra-go
      inputs:
        action: deploy-health
`
	fs := lintYAML(t, y)
	if !hasRule(fs, "R22") {
		t.Errorf("R22 missed leartech-agent-infra-go — the agentType every real "+
			"verify-release-flow check runs on, and more privileged than the one R22 blocked.\n  got: %v", fs)
	}
	if hasRule(fs, "R25") {
		t.Error("R25 must not fire on an infra check — infra IS the correct home for a verdict")
	}
}

// Templates are where infra belongs; neither rule should complain there.
func TestInfraCheckAllowedInPlanTemplate(t *testing.T) {
	y := `apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
metadata:
  name: t
spec:
  steps:
    - name: gate
      kind: check
      agentType: leartech-agent-infra-go
      inputs:
        action: deploy-health
`
	fs := lintYAML(t, y)
	if hasRule(fs, "R22") || hasRule(fs, "R25") {
		t.Errorf("infra check in a PlanTemplate must be clean: %v", fs)
	}
}
