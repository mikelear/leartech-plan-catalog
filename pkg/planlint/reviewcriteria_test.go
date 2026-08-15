package planlint_test

// Guards docs/review-criteria.md — the system prompt for the advisory plan-ai-review gate.
//
// WHY THESE TESTS EXIST. The criteria used to be a ~1400-char single-line SYS='…' literal
// inside scripts/plan-ai-review.sh. Because it was unreviewable, criterion 7 drifted out of
// lockstep with lint rule R23 and, for several releases, instructed authors to add a
// `use: verify-release-flow` step after every deployable PR step — the exact step R23 FAILS
// as duplicate auto-injection. The advisory reviewer was walking authors into a hard-gate
// failure, and nothing caught it because no test compared the two halves of the gate.
//
// The rule these tests encode: the model half must never recommend what the deterministic
// half rejects.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const criteriaPath = "../../docs/review-criteria.md"

func loadCriteria(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(criteriaPath)
	if err != nil {
		t.Fatalf("read %s: %v (the review gate skips entirely without this file)", criteriaPath, err)
	}
	return string(b)
}

// criteriaBody returns only the text the script actually sends as the system message:
// everything below the `## Criteria` heading. Prose above it is documentation for humans
// and is deliberately NOT asserted on.
func criteriaBody(t *testing.T) string {
	t.Helper()
	full := loadCriteria(t)
	idx := regexp.MustCompile(`(?m)^##\s+Criteria\s*$`).FindStringIndex(full)
	if idx == nil {
		t.Fatal("no '## Criteria' heading — scripts/plan-ai-review.sh extracts the prompt from it, " +
			"so without the heading the whole file would be sent, documentation included")
	}
	body := strings.TrimSpace(full[idx[1]:])
	if body == "" {
		t.Fatal("'## Criteria' section is empty — the script refuses to review with an empty prompt")
	}
	return body
}

// The script parses this to stamp reviews and flywheel records. Unparseable version =>
// verdicts cannot be attributed to the criteria that produced them.
func TestCriteriaVersionIsParseable(t *testing.T) {
	m := regexp.MustCompile(`criteria-version:\s*(\d+)`).FindStringSubmatch(loadCriteria(t))
	if m == nil {
		t.Fatal("no parseable `criteria-version: <n>` marker; scripts/plan-ai-review.sh would stamp 'unknown' " +
			"and flywheel records could not be compared across criteria revisions")
	}
	if m[1] == "0" {
		t.Error("criteria-version must start at 1")
	}
}

func TestCriteriaBodyIsSubstantive(t *testing.T) {
	if n := len(criteriaBody(t)); n < 500 {
		t.Errorf("criteria body is only %d bytes — suspiciously short for a review prompt; "+
			"a truncated prompt degrades every review silently", n)
	}
}

// THE LOCKSTEP TEST. R23 auto-injects verify-release-flow after deployable PR steps and
// FAILS a hand-authored one that dependsOn such a step. The criteria must say so, and must
// not instruct the reviewer to recommend adding one.
func TestCriteriaDoNotContradictR23(t *testing.T) {
	body := criteriaBody(t)
	lower := strings.ToLower(body)

	if !strings.Contains(lower, "auto-inject") {
		t.Error("criteria never mention that verify-release-flow is AUTO-INJECTED; without that, " +
			"the reviewer reports 'missing release verification' on every deployable PR step — " +
			"a finding that is false by construction and invisible in the submitted YAML")
	}

	// The regression itself: a POSITIVE instruction to recommend the step R23 rejects.
	//
	// Negation matters here and a naive substring match gets it backwards — the criteria MUST
	// be able to say "do NOT recommend adding a `use: verify-release-flow` step". So check each
	// clause that mentions both "recommend" and the template, and require a negation in it.
	// Clause granularity is deliberately per-line: these criteria are written as bullets, and
	// splitting on sentences would let a negation in one bullet excuse a positive instruction
	// in the next.
	negation := regexp.MustCompile(`(?i)\b(do not|don't|never|no longer|must not|rather than|instead of)\b`)
	for i, line := range strings.Split(body, "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "verify-release-flow") || !strings.Contains(l, "recommend") {
			continue
		}
		if !negation.MatchString(line) {
			t.Errorf("criteria line %d gives an UNNEGATED instruction to recommend verify-release-flow, "+
				"which lint R23 fails as duplicate auto-injection — authors following the advice hit the "+
				"hard gate.\n  line: %q", i+1, strings.TrimSpace(line))
		}
	}

	if !strings.Contains(lower, "r23") {
		t.Error("criteria should cite R23 by name so the lockstep requirement is discoverable " +
			"from either half of the gate")
	}
}

// When the catalog can't load, the reviewer must say so rather than invent template names.
// This is the model-side half of the fix; the pipeline installing pyyaml is the other half.
func TestCriteriaHandleUnavailableTemplateCatalog(t *testing.T) {
	lower := strings.ToLower(criteriaBody(t))
	if !strings.Contains(lower, "unavailable") && !strings.Contains(lower, "empty") {
		t.Error("criteria give no instruction for an empty/unavailable AVAILABLE TEMPLATES list; " +
			"the reviewer then guesses template names, or reports the tooling fault as a Plan defect")
	}
}

// Criteria are only useful if the script can find them. Catches a rename of either side.
func TestReviewScriptReadsTheCriteriaFile(t *testing.T) {
	b, err := os.ReadFile("../../scripts/plan-ai-review.sh")
	if err != nil {
		t.Fatalf("read review script: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "docs/review-criteria.md") {
		t.Error("scripts/plan-ai-review.sh no longer references docs/review-criteria.md — " +
			"the criteria file and the script have been decoupled")
	}
	if strings.Contains(s, "SYS='You are the Plan-quality reviewer") {
		t.Error("the inline SYS='…' prompt literal is back in the script; criteria must stay in " +
			"docs/review-criteria.md so they remain reviewable and versioned")
	}
}
