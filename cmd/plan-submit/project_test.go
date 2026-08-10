package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestProject_UseStepWithParams projects a Plan whose single step composes a
// PlanTemplate via use:+with: — the canonical catalog shape. Asserts the DTO
// carries name, kind, use, and with as raw JSON (opaque, verbatim).
func TestProject_UseStepWithParams(t *testing.T) {
	yaml := []byte(`apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: verify-plan-api
spec:
  paused: true
  steps:
    - name: verify
      kind: check
      use: verify-release-flow
      with:
        repo: mikelear/leartech-plan-api
        sha: "0f490c0"
        service: leartech-plan-api
`)
	req, err := project(yaml)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if req.Name != "verify-plan-api" {
		t.Errorf("name = %q, want verify-plan-api", req.Name)
	}
	if len(req.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(req.Steps))
	}
	s := req.Steps[0]
	if s.Kind != "check" || s.Use != "verify-release-flow" {
		t.Errorf("step kind/use = %q/%q, want check/verify-release-flow", s.Kind, s.Use)
	}
	var with map[string]any
	if err := json.Unmarshal(s.With, &with); err != nil {
		t.Fatalf("with is not valid JSON: %v (%s)", err, s.With)
	}
	if with["service"] != "leartech-plan-api" {
		t.Errorf("with.service = %v, want leartech-plan-api", with["service"])
	}
}

// TestProject_DevAgentStep projects a kind:pr dev-agent step and asserts inputs
// round-trip to raw JSON and agentType/repo carry through.
func TestProject_DevAgentStep(t *testing.T) {
	yaml := []byte(`kind: Plan
metadata:
  name: impl-plan
spec:
  steps:
    - name: impl
      kind: pr
      agentType: leartech-agent-go
      repo: mikelear/example
      inputs:
        name: impl
        repo: mikelear/example
        branch: feat/x
        goal: do the thing
`)
	req, err := project(yaml)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	s := req.Steps[0]
	if s.AgentType != "leartech-agent-go" || s.Repo != "mikelear/example" {
		t.Errorf("agentType/repo = %q/%q", s.AgentType, s.Repo)
	}
	var in map[string]any
	if err := json.Unmarshal(s.Inputs, &in); err != nil {
		t.Fatalf("inputs not valid JSON: %v", err)
	}
	if in["goal"] != "do the thing" {
		t.Errorf("inputs.goal = %v", in["goal"])
	}
}

// TestProject_OmitsEmptyWithInputs asserts an absent with/inputs projects to nil
// (omitted from JSON) rather than an empty object.
func TestProject_OmitsEmptyWithInputs(t *testing.T) {
	req, err := project([]byte("kind: Plan\nmetadata:\n  name: p\nspec:\n  steps:\n    - name: s\n      kind: check\n      use: t\n"))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if req.Steps[0].With != nil || req.Steps[0].Inputs != nil {
		t.Errorf("empty with/inputs should be nil, got with=%s inputs=%s", req.Steps[0].With, req.Steps[0].Inputs)
	}
	// And they must not appear in the marshaled JSON.
	b, _ := json.Marshal(req)
	if got := string(b); contains(got, `"with"`) || contains(got, `"inputs"`) {
		t.Errorf("empty with/inputs must be omitted from JSON: %s", got)
	}
}

func TestProject_Rejects(t *testing.T) {
	cases := map[string]string{
		"non-plan kind": "kind: PlanTemplate\nmetadata:\n  name: t\nspec:\n  steps: [{name: s}]\n",
		"no name":       "kind: Plan\nspec:\n  steps: [{name: s}]\n",
		"no steps":      "kind: Plan\nmetadata:\n  name: p\nspec: {}\n",
		"bad yaml":      "kind: Plan\n  : : :\n",
	}
	for name, y := range cases {
		if _, err := project([]byte(y)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestProject_RepoExamplePlan projects the checked-in plans/example-verify-release.yaml
// (if present) to prove the real catalog artifact projects cleanly — a guard that
// the DTO shape stays in sync with what authors actually write.
func TestProject_RepoExamplePlan(t *testing.T) {
	path := filepath.Join("..", "..", "plans", "example-verify-release.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("example plan not found (%v) — skipping", err)
	}
	req, err := project(data)
	if err != nil {
		t.Fatalf("project example plan: %v", err)
	}
	if req.Name == "" || len(req.Steps) == 0 {
		t.Errorf("example plan projected empty: %+v", req)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
