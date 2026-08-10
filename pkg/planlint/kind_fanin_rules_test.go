package planlint

import (
	"strings"
	"testing"
)

// TestUseStepBadKindRejected — a use-step with an invalid kind must be caught
// (R6). Before this rule, kind:gremlin on a use-step slipped past the enum check
// that only ran on concrete steps.
func TestUseStepBadKindRejected(t *testing.T) {
	const plan = `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: s
      kind: gremlin
      use: some-template
`
	f, err := LintBytes("p", []byte(plan))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !rules(f.Errors)["R6"] {
		t.Fatalf("use-step with kind:gremlin must be rejected (R6), got %v", f.Errors)
	}
}

// TestFanInWithAgentTypeRejected — a fan-in step is a no-agent gate; declaring
// agentType on it is a wiring bug (R13).
func TestFanInWithAgentTypeRejected(t *testing.T) {
	const plan = `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: leartech-agent-go
      inputs:
        name: a
        repo: mikelear/x
        branch: b
        goal: g
    - name: gate
      kind: check
      fanIn: true
      agentType: leartech-agent-go
      dependsOn: [a]
      fanInValidate: [docs/x.md]
`
	f, err := LintBytes("p", []byte(plan))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, e := range f.Errors {
		if e.Rule == "R13" && strings.Contains(e.Message, "must not declare agentType") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fan-in step with agentType must be rejected (R13), got %v", f.Errors)
	}
}
