package planlint

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// These cover the SERVICE-facing wrappers (LintBytes / LintOne / BuildIndex /
// HasErrors) that leartech-plan-api uses on its write path — proving a single
// source of truth: the same rules the CLI gate applies fire through the in-memory
// API too. (The rule logic itself is covered by lint_test.go.)

func TestLintBytes_GoodSubmissionPasses(t *testing.T) {
	body := goodPlan + "\n---\n" + goodTemplate
	f, err := LintBytes("submission", []byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.HasErrors() {
		t.Fatalf("good submission should pass, got errors %v", f.Errors)
	}
}

func TestLintBytes_R22CaughtOnServicePath(t *testing.T) {
	const infraInPlan = `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: bad-plan
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: leartech-agent-infra
      inputs:
        name: a
        repo: mikelear/x
        goal: do
`
	f, err := LintBytes("submission", []byte(infraInPlan))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !f.HasErrors() {
		t.Fatal("infra agentType in a plain Plan must be a hard error (R22)")
	}
	if !rules(f.Errors)["R22"] {
		t.Fatalf("want R22, got %v", f.Errors)
	}
}

func TestLintBytes_InvalidYAMLIsErrorAndFinding(t *testing.T) {
	f, err := LintBytes("submission", []byte("kind: Plan\n  bad: : indent"))
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if !rules(f.Errors)["R1"] {
		t.Fatalf("want R1 for invalid YAML, got %v", f.Errors)
	}
}

// LintOne is the plan-api shape: templates are resolved EXTERNALLY (from the
// cluster/catalog, not co-submitted) and a single Plan is linted against them.
func TestLintOne_WithExternalTemplateIndex(t *testing.T) {
	var tmpl, plan map[string]any
	if err := yaml.Unmarshal([]byte(goodTemplate), &tmpl); err != nil {
		t.Fatalf("template parse: %v", err)
	}
	if err := yaml.Unmarshal([]byte(goodPlan), &plan); err != nil {
		t.Fatalf("plan parse: %v", err)
	}
	idx := BuildIndex(tmpl)
	if _, ok := idx["good-template"]; !ok {
		t.Fatalf("BuildIndex should index the PlanTemplate, got %v", idx)
	}
	f := LintOne(plan, "good-plan", idx)
	if f.HasErrors() {
		t.Fatalf("plan should pass against the resolved template, got %v", f.Errors)
	}
}
