package planlint_test

// R26 + R27 regression tests. Both rules exist because of the ORIGINAL
// decom-orchestrator-service Plan (plan-catalog#36, commit 469942e): two
// leartech-agent-go (DEV) steps that removed the Orch release from the cluster
// GitOps repos jx-build-cluster-{gsm,akv} and instructed edits under config-root/.
// Nothing in R1–R25 flagged it — R21 was satisfied (a correct dev shape) and R22
// keys on an infra agentType, which a dev agent isn't. R26 gates on the target
// repo; R27 surfaces the config-root goal prose the structural rules can't see.
//
// (Uses the lintYAML / hasRule helpers defined in r25_test.go — same package.)

import (
	"testing"

	"github.com/mikelear/leartech-plan-catalog/pkg/planlint"
)

// lintRuleSeverity reports whether a rule fired as an error and/or a warning.
func lintRuleSeverity(t *testing.T, y, rule string) (isErr, isWarn bool) {
	t.Helper()
	res, _ := planlint.LintBytes("plans/t.yaml", []byte(y))
	for _, e := range res.Errors {
		if e.Rule == rule {
			isErr = true
		}
	}
	for _, w := range res.Warns {
		if w.Rule == rule {
			isWarn = true
		}
	}
	return
}

// The exact shape that shipped in 469942e and evaded R22: a dev agent pointed at a
// cluster GitOps repo, with a config-root instruction in its goal.
const gitOpsDecomStep = `apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: decom-orchestrator-service
spec:
  paused: true
  steps:
    - name: remove-orch-gsm
      kind: pr
      agentType: leartech-agent-go
      inputs:
        name: remove-orch-gsm
        repo: mikelear/jx-build-cluster-gsm
        branch: chore/remove-orch
        goal: >-
          Remove the leartech-orchestrator release from helmfile.yaml and also remove
          the rendered resources under config-root/namespaces/jx-staging/leartech-orchestrator/.
`

func TestR26IsHardErrorOnDevAgentGitOpsRepo(t *testing.T) {
	isErr, _ := lintRuleSeverity(t, gitOpsDecomStep, "R26")
	if !isErr {
		t.Errorf("R26 did not fire as a HARD ERROR on a dev agent (leartech-agent-go) " +
			"targeting jx-build-cluster-gsm — the GitOps step that evaded R22.")
	}
}

func TestR27IsWarningOnConfigRootGoal(t *testing.T) {
	isErr, isWarn := lintRuleSeverity(t, gitOpsDecomStep, "R27")
	if !isWarn {
		t.Errorf("R27 did not fire on a goal naming config-root.")
	}
	if isErr {
		t.Errorf("R27 fired as an error — it is a prose heuristic and must stay a warning.")
	}
}

// A clean code-repo step (the legitimate half of the decom) must NOT trip R26/R27.
const cleanCodeStep = `apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: t
spec:
  paused: true
  steps:
    - name: drop-orch-cli-client
      kind: pr
      agentType: leartech-agent-py
      inputs:
        name: drop-orch-cli-client
        repo: mikelear/leartech-automated-agent
        branch: chore/drop-cli
        goal: Remove the orchestrator CLI client and its call sites; update tests.
`

func TestR26R27QuietOnCleanCodeStep(t *testing.T) {
	fs := lintYAML(t, cleanCodeStep)
	if hasRule(fs, "R26") {
		t.Errorf("R26 fired on a non-GitOps code repo — false positive.\n  got: %v", fs)
	}
	if hasRule(fs, "R27") {
		t.Errorf("R27 fired on a goal with no config-root reference — false positive.\n  got: %v", fs)
	}
}

// A GitOps target inside a PlanTemplate is the sanctioned, OWNERS-gated home — R26
// is Plan-only and must not fire there.
const gitOpsInTemplate = `apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
metadata:
  name: remove-release-from-cluster
spec:
  params:
    - name: repo
      required: true
  steps:
    - name: remove-release
      kind: pr
      agentType: leartech-agent-go
      inputs:
        name: remove-release
        repo: mikelear/jx-build-cluster-gsm
        branch: chore/remove
        goal: Remove the named release entry from helmfile.yaml; let boot re-render.
`

func TestR26QuietInsideTemplate(t *testing.T) {
	fs := lintYAML(t, gitOpsInTemplate)
	if hasRule(fs, "R26") {
		t.Errorf("R26 fired inside a PlanTemplate — templates are the sanctioned home "+
			"for GitOps changes; R26 must be Plan-only.\n  got: %v", fs)
	}
}
