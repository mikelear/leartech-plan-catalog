package planlint

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures under testdata/ are illustrative Plan YAMLs — one rule (or the
// clean baseline) per file, each with a header comment explaining it — so the
// rules are legible to humans reading real Plans, not just encoded in Go. Each
// file's expected HARD-error rule codes are declared here; an empty set means the
// fixture must pass with no errors. Add a fixture + a row here for each rule you
// want to document by example.
func TestTestdataFixtures(t *testing.T) {
	cases := map[string][]string{
		"good-plan.yaml":             {},      // the canonical correct shape — passes clean
		"use-step-invalid-kind.yaml": {"R6"},  // kind enum enforced on use: steps
		"fanin-with-agenttype.yaml":  {"R13"}, // fan-in gate must not carry agentType
		"infra-in-plan.yaml":         {"R22"}, // infra steps are template-only
	}
	for file, want := range cases {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			f, err := LintBytes(file, data)
			if err != nil {
				t.Fatalf("lint %s: %v", file, err)
			}
			got := rules(f.Errors)
			for _, code := range want {
				if !got[code] {
					t.Errorf("%s: expected hard error %s, got %v", file, code, f.Errors)
				}
			}
			if len(want) == 0 && f.HasErrors() {
				t.Errorf("%s: expected NO errors, got %v", file, f.Errors)
			}
		})
	}
}
