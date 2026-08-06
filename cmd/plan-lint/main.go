// Command plan-lint is the deterministic hard gate for the ShipProven Plan
// Catalog. It lints every *.yaml under plans/ and templates/ and exits non-zero
// if any Plan/PlanTemplate violates a structural or safety rule. See
// internal/lint for the rules (R1–R19).
//
// With -json it emits a machine-readable verdict record instead of text — the
// substrate a verdict store / Plan-quality flywheel consumes (see docs/flywheel.md).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/mikelear/leartech-plan-catalog/internal/lint"
)

// report is the structured plan-lint verdict emitted under -json.
type report struct {
	Tool         string         `json:"tool"`
	FilesChecked int            `json:"files_checked"`
	Pass         bool           `json:"pass"`
	Errors       []lint.Finding `json:"errors"`
	Warnings     []lint.Finding `json:"warnings"`
}

func main() {
	asJSON := flag.Bool("json", false, "emit a machine-readable verdict record instead of text")
	flag.Parse()

	f, n, err := lint.Run([]string{"plans", "templates"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-lint:", err)
		os.Exit(2)
	}
	pass := len(f.Errors) == 0

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report{
			Tool:         "plan-lint",
			FilesChecked: n,
			Pass:         pass,
			Errors:       f.Errors,
			Warnings:     f.Warns,
		})
		if !pass {
			os.Exit(1)
		}
		return
	}

	if n == 0 {
		fmt.Println("plan-lint: no Plan/PlanTemplate YAML under plans/ or templates/ — nothing to check.")
		return
	}
	fmt.Printf("plan-lint: checked %d file(s)\n", n)
	for _, w := range f.Warns {
		fmt.Printf("  WARN %s\n", w)
	}
	for _, e := range f.Errors {
		fmt.Printf("  FAIL %s\n", e)
	}
	if !pass {
		fmt.Printf("\nplan-lint: FAIL — %d error(s), %d warning(s)\n", len(f.Errors), len(f.Warns))
		os.Exit(1)
	}
	fmt.Printf("\nplan-lint: PASS — 0 errors, %d warning(s)\n", len(f.Warns))
}
