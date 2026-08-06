// Package lint is the DETERMINISTIC gate for the ShipProven Plan Catalog.
//
// This is the "golangci for Plans": deterministic, un-bypassable structural +
// safety checks on every Plan / PlanTemplate submitted to the catalog. It is the
// HARD gate (a non-empty error set means the PR must not merge); the multi-model
// ai-review step is the advisory judgment layer on top.
//
// Design: the safety surface GROWS WITH THE PIPELINE — add a new guarantee by
// adding a rule here (and, later, a whole new Tekton step). Each rule below
// encodes a lesson we've paid for in a real run; the rule numbers are stable so
// failures are greppable.
package lint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only accepted apiVersion for catalog documents.
const APIVersion = "agent.leartech.io/v1alpha1"

// MaxName bounds metadata.name. The AgentRun jobName goes into a 63-byte
// pod-template label as <plan>-<step>-<attempt>; if the name overflows, the run
// never spawns (empty phase, no Job, no events). Keep names short.
const MaxName = 58

var (
	stepKinds   = map[string]bool{"pr": true, "apply": true, "check": true}
	sortedKinds = []string{"apply", "check", "pr"}
	nameRE      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`) // DNS-1123 label
	laptopRE    = regexp.MustCompile(`(/Users/|/home/|~/)`)
)

// Findings accumulates hard errors and advisory warnings.
type Findings struct {
	Errors []string
	Warns  []string
}

func (f *Findings) err(rule, where, msg string) {
	f.Errors = append(f.Errors, fmt.Sprintf("[%s] %s: %s", rule, where, msg))
}

func (f *Findings) warn(rule, where, msg string) {
	f.Warns = append(f.Warns, fmt.Sprintf("[%s] %s: %s", rule, where, msg))
}

// Run walks every *.yaml under the given roots and lints each document. Missing
// roots are ignored. It returns the findings and the number of files checked.
func Run(roots []string) (*Findings, int, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // a missing plans/ or templates/ dir is fine
			}
			if !d.IsDir() && strings.HasSuffix(p, ".yaml") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	sort.Strings(files)

	f := &Findings{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			f.err("R1", path, fmt.Sprintf("cannot read: %v", err))
			continue
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var doc map[string]any
			if derr := dec.Decode(&doc); derr != nil {
				if errors.Is(derr, io.EOF) {
					break
				}
				f.err("R1", path, fmt.Sprintf("invalid YAML: %v", derr))
				break
			}
			if len(doc) > 0 {
				LintDoc(doc, path, f)
			}
		}
	}
	return f, len(files), nil
}

// LintDoc applies the structural + safety rules to a single decoded document.
func LintDoc(doc map[string]any, where string, f *Findings) {
	// R2 apiVersion
	if asStr(doc["apiVersion"]) != APIVersion {
		f.err("R2", where, fmt.Sprintf("apiVersion must be %q, got %q", APIVersion, asStr(doc["apiVersion"])))
	}
	// R3 kind
	kind := asStr(doc["kind"])
	if kind != "Plan" && kind != "PlanTemplate" {
		f.err("R3", where, fmt.Sprintf("kind must be Plan|PlanTemplate, got %q", kind))
	}
	// R4 name present, DNS-1123, length
	meta, _ := asMap(doc["metadata"])
	name := asStr(meta["name"])
	switch {
	case name == "":
		f.err("R4", where, "metadata.name is required")
	default:
		if !nameRE.MatchString(name) {
			f.err("R4", where, fmt.Sprintf("metadata.name %q is not a DNS-1123 label", name))
		}
		if len(name) > MaxName {
			f.err("R4", where, fmt.Sprintf("metadata.name %q is %d>%d chars — the AgentRun will never spawn (63-byte label cap)", name, len(name), MaxName))
		}
	}

	spec, ok := asMap(doc["spec"])
	if !ok || len(spec) == 0 {
		f.err("R1", where, "spec is required and must be a mapping")
		return
	}

	switch kind {
	case "Plan":
		lintPlan(spec, where, f)
	case "PlanTemplate":
		lintTemplate(spec, where, f)
	}

	// R10 no laptop/absolute-path artifact refs anywhere in the spec (plans run
	// in a K8s Job with only the target repo checked out — laptop paths never
	// resolve).
	blob, _ := yaml.Marshal(spec)
	for _, m := range uniqueMatches(laptopRE, string(blob)) {
		f.err("R10", where, fmt.Sprintf("spec references a laptop/absolute path (%q) — reference only repo-committed or defined-store artifacts", m))
	}
}

func lintPlan(spec map[string]any, where string, f *Findings) {
	// R5 HOLD-BY-DEFAULT — the load-bearing safety rule. A catalog Plan is a
	// PROPOSAL: it must NOT be able to execute on merge. Execution is a separate,
	// human-gated promote/unpause. The reconciler honors spec.paused=true.
	if b, ok := spec["paused"].(bool); !ok || !b {
		f.err("R5", where, "spec.paused MUST be true — catalog Plans are hold-by-default proposals; execution is a separate human promote/unpause")
	}

	steps, ok := asSlice(spec["steps"])
	if !ok || len(steps) == 0 {
		f.err("R6", where, "spec.steps must be a non-empty list")
		return
	}
	names := stepNames(steps)
	// R7 unique step names
	if dupes := duplicates(names); len(dupes) > 0 {
		f.err("R7", where, fmt.Sprintf("duplicate step names: %v", dupes))
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	for i, raw := range steps {
		s, isMap := asMap(raw)
		name := asStr(s["name"])
		sw := fmt.Sprintf("%s step[%d]=%s", where, i, orQ(name))
		if !isMap || name == "" {
			f.err("R6", sw, "each step needs a name")
			continue
		}
		// R6 a step is either a template include (use:) OR a concrete step
		// (kind+agentType).
		if _, hasUse := s["use"]; hasUse {
			if _, hasKind := s["kind"]; hasKind {
				f.warn("R6", sw, "a `use:` step should not set `kind` (the template's steps carry kind)")
			}
		} else {
			if kind := asStr(s["kind"]); !stepKinds[kind] {
				f.err("R6", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, kind))
			}
			if asStr(s["agentType"]) == "" {
				f.err("R6", sw, "concrete step needs agentType")
			}
		}
		// R8 dependsOn references must resolve
		for _, dep := range strSlice(s["dependsOn"]) {
			if !known[dep] {
				f.err("R8", sw, fmt.Sprintf("dependsOn references unknown step %q", dep))
			}
		}
	}
}

func lintTemplate(spec map[string]any, where string, f *Findings) {
	// R9 templates declare params + steps; template steps are concrete (no nested
	// use: — expansion is depth-1) and carry kind+agentType.
	if _, ok := asSlice(spec["params"]); !ok {
		f.warn("R9", where, "spec.params should be a list of {name, required}")
	}
	steps, ok := asSlice(spec["steps"])
	if !ok || len(steps) == 0 {
		f.err("R9", where, "spec.steps must be a non-empty list")
		return
	}
	for i, raw := range steps {
		s, isMap := asMap(raw)
		name := asStr(s["name"])
		sw := fmt.Sprintf("%s step[%d]=%s", where, i, orQ(name))
		if !isMap || name == "" {
			f.err("R9", sw, "each step needs a name")
			continue
		}
		if _, hasUse := s["use"]; hasUse {
			f.err("R9", sw, "template steps must NOT nest `use:` (expansion is depth-1)")
		}
		if kind := asStr(s["kind"]); !stepKinds[kind] {
			f.err("R9", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, kind))
		}
		if asStr(s["agentType"]) == "" {
			f.err("R9", sw, "template step needs agentType")
		}
	}
}

// --- small typed accessors over the generic YAML tree -----------------------

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func stepNames(steps []any) []string {
	out := make([]string, 0, len(steps))
	for _, raw := range steps {
		if s, ok := asMap(raw); ok {
			out = append(out, asStr(s["name"]))
		}
	}
	return out
}

func strSlice(v any) []string {
	raw, ok := asSlice(v)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		out = append(out, asStr(e))
	}
	return out
}

func duplicates(names []string) []string {
	count := map[string]int{}
	for _, n := range names {
		count[n]++
	}
	var out []string
	for n, c := range count {
		if c > 1 {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueMatches(re *regexp.Regexp, s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(s, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
