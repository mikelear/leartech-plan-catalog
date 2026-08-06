// Command plan-lint is the deterministic hard gate for the ShipProven Plan
// Catalog. It lints every *.yaml under plans/ and templates/ and exits non-zero
// if any Plan/PlanTemplate violates a structural or safety rule. See
// internal/lint for the rules (R1–R10).
package main

import (
	"fmt"
	"os"

	"github.com/mikelear/leartech-plan-catalog/internal/lint"
)

func main() {
	f, n, err := lint.Run([]string{"plans", "templates"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-lint:", err)
		os.Exit(2)
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
	if len(f.Errors) > 0 {
		fmt.Printf("\nplan-lint: FAIL — %d error(s), %d warning(s)\n", len(f.Errors), len(f.Warns))
		os.Exit(1)
	}
	fmt.Printf("\nplan-lint: PASS — 0 errors, %d warning(s)\n", len(f.Warns))
}
