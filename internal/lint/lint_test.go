package lint

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// lintYAML decodes a single YAML document and lints it, returning the findings.
func lintYAML(t *testing.T, doc string) *Findings {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("test YAML did not parse: %v", err)
	}
	f := &Findings{}
	LintDoc(m, "test.yaml", f)
	return f
}

// rules returns the set of rule codes present in the given lines (e.g. "R5").
func rules(lines []string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lines {
		if i := strings.Index(l, "]"); strings.HasPrefix(l, "[") && i > 0 {
			out[l[1:i]] = true
		}
	}
	return out
}

func assertErr(t *testing.T, f *Findings, code string) {
	t.Helper()
	if !rules(f.Errors)[code] {
		t.Errorf("expected error %s; got errors=%v", code, f.Errors)
	}
}

func assertNoErr(t *testing.T, f *Findings, code string) {
	t.Helper()
	if rules(f.Errors)[code] {
		t.Errorf("did not expect error %s; got errors=%v", code, f.Errors)
	}
}

const goodPlan = `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: good-plan
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: leartech-agent-go
    - name: b
      kind: check
      agentType: leartech-agent-infra
      dependsOn: [a]
`

const goodTemplate = `
apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
metadata:
  name: good-template
spec:
  params:
    - name: service
      required: true
  steps:
    - name: check-it
      kind: check
      agentType: leartech-agent-infra
`

func TestGoodPlanPasses(t *testing.T) {
	f := lintYAML(t, goodPlan)
	if len(f.Errors) != 0 {
		t.Fatalf("good plan should have no errors, got %v", f.Errors)
	}
}

func TestGoodTemplatePasses(t *testing.T) {
	f := lintYAML(t, goodTemplate)
	if len(f.Errors) != 0 {
		t.Fatalf("good template should have no errors, got %v", f.Errors)
	}
}

func TestR2ApiVersion(t *testing.T) {
	f := lintYAML(t, strings.Replace(goodPlan, "agent.leartech.io/v1alpha1", "wrong/v1", 1))
	assertErr(t, f, "R2")
}

func TestR3Kind(t *testing.T) {
	f := lintYAML(t, strings.Replace(goodPlan, "kind: Plan", "kind: Widget", 1))
	assertErr(t, f, "R3")
}

func TestR4NameTooLong(t *testing.T) {
	long := strings.Repeat("x", 72)
	f := lintYAML(t, strings.Replace(goodPlan, "good-plan", long, 1))
	assertErr(t, f, "R4")
}

func TestR4NameNotDNS(t *testing.T) {
	f := lintYAML(t, strings.Replace(goodPlan, "good-plan", "Bad_Name", 1))
	assertErr(t, f, "R4")
}

func TestR5HoldByDefault(t *testing.T) {
	f := lintYAML(t, strings.Replace(goodPlan, "paused: true", "paused: false", 1))
	assertErr(t, f, "R5")

	// paused true must NOT trip R5
	assertNoErr(t, lintYAML(t, goodPlan), "R5")
}

func TestR6BadKindAndMissingAgentType(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: nope
`
	f := lintYAML(t, doc)
	assertErr(t, f, "R6") // bad kind + missing agentType both R6
}

func TestR6EmptySteps(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps: []
`
	assertErr(t, lintYAML(t, doc), "R6")
}

func TestR7DuplicateNames(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: x
    - name: a
      kind: check
      agentType: y
`
	assertErr(t, lintYAML(t, doc), "R7")
}

func TestR8DependsOnUnknown(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: x
      dependsOn: [ghost]
`
	assertErr(t, lintYAML(t, doc), "R8")
}

func TestR9TemplateNestedUse(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
metadata:
  name: t
spec:
  params: []
  steps:
    - name: a
      use: other-template
`
	f := lintYAML(t, doc)
	assertErr(t, f, "R9") // nested use + missing kind/agentType
}

func TestR10LaptopPath(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: pr
      agentType: x
      goal: "read the spec at /Users/mike/spec.md"
`
	assertErr(t, lintYAML(t, doc), "R10")
}

func TestUseStepIsValidWithoutKind(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: inc
      use: some-template
      with:
        service: foo
`
	f := lintYAML(t, doc)
	if len(f.Errors) != 0 {
		t.Fatalf("a use: step should be valid without kind/agentType, got %v", f.Errors)
	}
}
