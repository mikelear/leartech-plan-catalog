package lint

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// lintYAML decodes a single YAML document and lints it with an empty template
// index, returning the findings.
func lintYAML(t *testing.T, doc string) *Findings {
	t.Helper()
	return lintDocs(t, doc)
}

// lintDocs decodes several YAML documents, builds the template index across all
// of them (so a Plan and the template it uses can be co-submitted), then lints
// each. Mirrors Run's two-pass behaviour without touching the filesystem.
func lintDocs(t *testing.T, docs ...string) *Findings {
	t.Helper()
	var parsed []map[string]any
	for _, d := range docs {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(d), &m); err != nil {
			t.Fatalf("test YAML did not parse: %v", err)
		}
		parsed = append(parsed, m)
	}
	idx := TemplateIndex{}
	for _, m := range parsed {
		if asStr(m["kind"]) == "PlanTemplate" {
			if n := asStr(mapOf(m["metadata"])["name"]); n != "" {
				idx[n] = templateMeta(mapOf(m["spec"]))
			}
		}
	}
	f := &Findings{}
	for _, m := range parsed {
		LintDoc(m, "test.yaml", f, idx)
	}
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

func assertWarn(t *testing.T, f *Findings, code string) {
	t.Helper()
	if !rules(f.Warns)[code] {
		t.Errorf("expected warning %s; got warns=%v", code, f.Warns)
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
	assertErr(t, lintYAML(t, doc), "R6")
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
	assertErr(t, lintYAML(t, doc), "R9")
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

func TestR11Cycle(t *testing.T) {
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
      dependsOn: [b]
    - name: b
      kind: pr
      agentType: x
      dependsOn: [a]
`
	assertErr(t, lintYAML(t, doc), "R11")
}

func TestR12SelfDep(t *testing.T) {
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
      dependsOn: [a]
`
	assertErr(t, lintYAML(t, doc), "R12")
}

func TestR13FanInMissingValidate(t *testing.T) {
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
    - name: gate
      fanIn: true
      dependsOn: [a]
`
	assertErr(t, lintYAML(t, doc), "R13")
}

func TestR13NonFanInWithValidate(t *testing.T) {
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
      fanInValidate: ["README.md"]
`
	assertErr(t, lintYAML(t, doc), "R13")
}

func TestFanInStepIsValid(t *testing.T) {
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
    - name: gate
      fanIn: true
      dependsOn: [a]
      fanInValidate: ["README.md"]
`
	f := lintYAML(t, doc)
	if len(f.Errors) != 0 {
		t.Fatalf("a well-formed fan-in step should have no errors, got %v", f.Errors)
	}
}

func TestR14UseStepForbiddenFields(t *testing.T) {
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
      agentType: smuggled
      repo: some/repo
`
	assertErr(t, lintYAML(t, doc), "R14")
}

func TestR15UnknownTemplateWarnsNotErrors(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: inc
      use: not-in-this-pr
      with:
        service: foo
`
	f := lintYAML(t, doc)
	assertWarn(t, f, "R15")
	if len(f.Errors) != 0 {
		t.Fatalf("unknown template should warn, not error, got %v", f.Errors)
	}
}

func TestR16RequiredParamMissing(t *testing.T) {
	plan := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: inc
      use: good-template
      with: {}
`
	f := lintDocs(t, plan, goodTemplate)
	assertErr(t, f, "R16")
}

func TestR16RequiredParamSupplied(t *testing.T) {
	plan := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: inc
      use: good-template
      with:
        service: foo
`
	f := lintDocs(t, plan, goodTemplate)
	assertNoErr(t, f, "R16")
}

func TestR17TriggeredWhenBadPhase(t *testing.T) {
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
    - name: b
      kind: apply
      agentType: x
      triggeredWhen:
        step: a
        phase: Bogus
`
	assertErr(t, lintYAML(t, doc), "R17")
}

func TestR17TriggeredWhenUnknownStep(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: b
      kind: apply
      agentType: x
      triggeredWhen:
        step: ghost
        phase: AwaitingReview
`
	assertErr(t, lintYAML(t, doc), "R17")
}

func TestTriggeredWhenValid(t *testing.T) {
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
    - name: b
      kind: apply
      agentType: x
      triggeredWhen:
        step: a
        phase: AwaitingReview
`
	f := lintYAML(t, doc)
	if len(f.Errors) != 0 {
		t.Fatalf("valid triggeredWhen should have no errors, got %v", f.Errors)
	}
}

func TestR19TestDirectedBadForKind(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  test: true
  steps:
    - name: a
      kind: check
      agentType: x
      test:
        directed: merged
`
	assertErr(t, lintYAML(t, doc), "R19")
}

func TestR19TestDirectedWithoutTestModeWarns(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: a
      kind: check
      agentType: x
      test:
        directed: pass
`
	f := lintYAML(t, doc)
	assertWarn(t, f, "R19")
}

func TestTestDirectedValid(t *testing.T) {
	doc := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  test: true
  steps:
    - name: a
      kind: check
      agentType: x
      test:
        directed: pass
    - name: b
      kind: pr
      agentType: x
      test:
        directed: merged
`
	f := lintYAML(t, doc)
	if len(f.Errors) != 0 {
		t.Fatalf("valid test directives should have no errors, got %v", f.Errors)
	}
}

func TestUseStepIsValidWithoutKind(t *testing.T) {
	plan := `
apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: p
spec:
  paused: true
  steps:
    - name: inc
      use: good-template
      with:
        service: foo
`
	f := lintDocs(t, plan, goodTemplate)
	if len(f.Errors) != 0 {
		t.Fatalf("a use: step should be valid without kind/agentType, got %v", f.Errors)
	}
}
