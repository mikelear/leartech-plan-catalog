// Package sync holds no code — only tests over scripts/sync-templates-to-controller.sh,
// which is what actually runs. The script is the artefact; these execute it.
package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// stripViaScript runs the SHIPPED strip_yaml_comments over input, by extracting
// the function from the sync script and sourcing it. Extracting rather than
// reimplementing is the point: a copy of the logic here would pass while the
// script that runs in CI drifted away from it.
func stripViaScript(t *testing.T, input string) string {
	t.Helper()
	script := filepath.Join("..", "..", "scripts", "sync-templates-to-controller.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("reading the sync script: %v — this test cannot pass by failing to find it", err)
	}
	shell := `eval "$(sed -n '/^strip_yaml_comments() {/,/^}/p' ` + script + `)"; strip_yaml_comments`
	cmd := exec.Command("bash", "-c", shell)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running strip_yaml_comments: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("strip_yaml_comments produced nothing; the function was probably not extracted")
	}
	return string(out)
}

// A STRIPPED TEMPLATE IS THE SAME TEMPLATE.
//
// The sync copies these files into the controller chart, and they are applied
// to both clusters. Removing comments must change what a reader sees and
// nothing else, so this compares the PARSED documents rather than the text.
func TestStrippingChangesNoTemplateSemantics(t *testing.T) {
	dir := filepath.Join("..", "..", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading templates/: %v", err)
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		checked++
		t.Run(e.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			var before, after map[string]any
			if err := yaml.Unmarshal(raw, &before); err != nil {
				t.Fatalf("source is not valid YAML: %v", err)
			}
			stripped := stripViaScript(t, string(raw))
			if err := yaml.Unmarshal([]byte(stripped), &after); err != nil {
				t.Fatalf("stripping produced invalid YAML: %v\n%s", err, stripped)
			}
			if !equalYAML(t, before, after) {
				t.Errorf("stripping changed the template's meaning, not just its comments")
			}
		})
	}
	if checked == 0 {
		t.Fatal("no templates found, so this examined nothing")
	}
}

// A # INSIDE A BLOCK SCALAR IS CONTENT.
//
// This is the case that would do real damage and is invisible in a diff of the
// two existing templates, because neither happens to contain one today. Inside
// a `goal: |` block every line is literal text handed to the agent, so a line
// beginning with # is part of the prompt. A naive line filter deletes it and
// the agent silently receives a different brief.
func TestABlockScalarKeepsItsHashLines(t *testing.T) {
	const src = `apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
metadata:
  name: t
spec:
  steps:
    - name: dev
      inputs:
        goal: |
          Fix the build.
          # not a comment: this line is part of the prompt
          Run make test.
        other: value
`
	out := stripViaScript(t, src)

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid YAML after stripping: %v\n%s", err, out)
	}
	goal := digGoal(t, doc)
	if !strings.Contains(goal, "# not a comment") {
		t.Errorf("the # line was stripped out of a block scalar, so the agent would get a different brief:\n%q", goal)
	}
	if strings.Contains(out, "# a real comment") {
		t.Error("a real comment survived")
	}
}

// THE GENERATED FILE COSTS NOTHING AGAINST THE COMMENT GATE.
//
// The controller's comment gate fails a change that adds more prose comment
// lines than test lines, and a sync PR is a machine copy of YAML that can never
// add a test line — so any counted prose fails it permanently. controller#199
// was the first sync after the gate reached that repo and went red on 49 lines.
//
// commentRe is anchored (`^\s*#`), so this counts whole-line comments only; the
// one that remains is the canonical generated marker, which the gate's
// functional-directive exemption already recognises.
func TestStrippedTemplatesLeaveNoCountedProse(t *testing.T) {
	dir := filepath.Join("..", "..", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading templates/: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			for i, ln := range strings.Split(stripViaScript(t, string(raw)), "\n") {
				if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
					t.Errorf("line %d is a whole-line comment the gate would count:\n  %s", i+1, ln)
				}
			}
		})
	}
}

// THE SYNC EMITS THE MARKER THE GATE RECOGNISES.
//
// The exemption keys on the literal "Code generated". A header that merely says
// "do not edit" reads the same to a human and is counted by the gate, which is
// what the previous three-line header did.
func TestSyncWritesTheCanonicalGeneratedMarker(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "sync-templates-to-controller.sh"))
	if err != nil {
		t.Fatalf("reading the sync script: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "# Code generated by sync-templates-to-controller") {
		t.Error("the sync does not emit the canonical generated marker, so its header is counted as prose")
	}
	if !strings.Contains(s, "DO NOT EDIT.") {
		t.Error("the marker is not in the canonical form")
	}
	if !strings.Contains(s, "strip_yaml_comments < \"$src\"") {
		t.Error("the sync copies the template without stripping comments")
	}
}

func digGoal(t *testing.T, doc map[string]any) string {
	t.Helper()
	spec, _ := doc["spec"].(map[string]any)
	steps, _ := spec["steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("no steps in the stripped document")
	}
	step, _ := steps[0].(map[string]any)
	inputs, _ := step["inputs"].(map[string]any)
	goal, _ := inputs["goal"].(string)
	return goal
}

func equalYAML(t *testing.T, a, b map[string]any) bool {
	t.Helper()
	ab, err := yaml.Marshal(a)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	bb, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	return string(ab) == string(bb)
}
