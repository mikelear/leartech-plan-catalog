// Command rulesdoc generates the machine- + human-readable rule catalog from the
// single source (pkg/planlint.Rules): docs/rules.json (agents) and docs/rules.md
// (humans + the `doc` anchors the PR comment links to). Regenerate whenever the
// rules change:
//
//	go run ./cmd/rulesdoc -out docs
//
// CI runs it and diffs, so the catalog can never drift from the linter.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mikelear/leartech-plan-catalog/pkg/planlint"
)

func main() {
	out := flag.String("out", "docs", "output directory for rules.json + rules.md")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	// Deterministic order: R1, R2, … R19 (numeric, not lexical).
	ids := make([]string, 0, len(planlint.Rules))
	for id := range planlint.Rules {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ruleNum(ids[i]) < ruleNum(ids[j]) })

	ordered := make([]planlint.RuleMeta, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, planlint.Rules[id])
	}

	// rules.json
	jb, err := json.MarshalIndent(map[string]any{
		"schema_version": "v1",
		"rules":          ordered,
	}, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*out, "rules.json"), append(jb, '\n'), 0o644); err != nil {
		fatal(err)
	}

	// rules.md
	var b strings.Builder
	fmt.Fprintf(&b, "# Plan Catalog rules (%s–%s)\n\n", ordered[0].ID, ordered[len(ordered)-1].ID)
	b.WriteString("_Generated from `pkg/planlint` — do not edit by hand; run `make rules`._\n\n")
	b.WriteString("Every rule the deterministic `plan-lint` gate enforces. Errors block merge; ")
	b.WriteString("a failing PR comment links each rule here by its anchor (e.g. `#r11`).\n\n")
	b.WriteString("| Rule | Title | What it checks |\n|---|---|---|\n")
	for _, r := range ordered {
		fmt.Fprintf(&b, "| [%s](#%s) | %s | %s |\n", r.ID, strings.ToLower(r.ID), r.Title, r.Why)
	}
	b.WriteString("\n")
	for _, r := range ordered {
		fmt.Fprintf(&b, "## %s — %s\n\n", r.ID, r.Title)
		fmt.Fprintf(&b, "**Why:** %s\n\n", r.Why)
		fmt.Fprintf(&b, "**Fix:** %s\n\n", r.Fix)
	}
	if err := os.WriteFile(filepath.Join(*out, "rules.md"), []byte(b.String()), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s/rules.json + %s/rules.md (%d rules)\n", *out, *out, len(ordered))
}

func ruleNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "R"))
	return n
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rulesdoc:", err)
	os.Exit(1)
}
