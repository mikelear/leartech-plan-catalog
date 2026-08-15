package planlint

// RuleMeta is the human- + machine-readable description of a lint rule. It is the
// single source for: the generated rule catalog (docs/rules.json + rules.md), the
// `fix` / `doc` hints embedded in `plan-lint -json`, and the sticky PR comment. An
// AI that submitted a Plan reads this to understand + repair a failure.
type RuleMeta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Why   string `json:"why"`
	Fix   string `json:"fix"`
}

// Rules is the catalog of every lint rule, keyed by ID (R1…R27). Keep in lockstep
// with the checks in lint.go — a rule referenced in a Finding must have an entry.
var Rules = map[string]RuleMeta{
	"R1":  {"R1", "Valid document", "A Plan must parse as YAML and have a spec mapping, or the controller can't admit it.", "Fix the YAML syntax; ensure top-level apiVersion/kind/metadata/spec are present and spec is a mapping."},
	"R2":  {"R2", "apiVersion", "Only agent.leartech.io/v1alpha1 is served by the platform CRD.", "Set apiVersion: agent.leartech.io/v1alpha1."},
	"R3":  {"R3", "kind", "The catalog accepts only Plan and PlanTemplate.", "Set kind to Plan or PlanTemplate."},
	"R4":  {"R4", "Name shape", "metadata.name becomes a DNS-1123 label and part of the child AgentRun name; >58 chars risks the 63-byte pod-label cap.", "Use a lowercase DNS-1123 name (letters, digits, hyphens) of at most 58 characters."},
	"R5":  {"R5", "Hold-by-default", "A catalog Plan is a PROPOSAL — it must not execute on merge. Execution is a separate, human-gated promote/unpause.", "Set spec.paused: true on every Plan."},
	"R6":  {"R6", "Step shape", "Each step is either a template include (use:) or a concrete step; a concrete non-fan-in step needs a kind and an agentType.", "Give the step a name; for a concrete step set kind to pr|apply|check and set agentType; or make it a use:/fanIn step."},
	"R7":  {"R7", "Unique step names", "Step names must be unique — they compute the child AgentRun name and dependsOn edges.", "Rename duplicate steps so every step name is unique."},
	"R8":  {"R8", "dependsOn resolves", "A dependsOn that names no declared step leaves the step permanently blocked.", "Point dependsOn at existing step names, or remove the dangling entry."},
	"R9":  {"R9", "Template shape", "A PlanTemplate declares params and steps; its steps are concrete (kind+agentType) and must not nest use: (expansion is depth-1).", "Give the template params + steps; make each step concrete (kind+agentType) with no nested use:."},
	"R10": {"R10", "No laptop paths", "A Plan runs in a K8s Job with only the target repo checked out — laptop/absolute paths never resolve.", "Reference only repo-committed or defined-store artifacts; remove /Users/, /home/, or ~/ paths."},
	"R11": {"R11", "No cycle", "A dependsOn cycle makes the DAG un-runnable; the controller marks the Plan Failed.", "Break the cycle so the dependsOn graph is acyclic."},
	"R12": {"R12", "No self-dependency", "A step depending on itself (via dependsOn or triggeredWhen) can never start.", "Remove the self-reference."},
	"R13": {"R13", "Fan-in shape", "A fanIn step is a no-agent gate: it must fan in from >=1 dependsOn and assert >=1 fanInValidate path; a non-fan-in step must not set fanInValidate.", "For fanIn: true add dependsOn + fanInValidate; otherwise remove fanInValidate."},
	"R14": {"R14", "use-step fields", "A use: step must not smuggle runtime fields — the template supplies them; expansion hard-fails otherwise.", "On a use: step keep only name, use, with, dependsOn (and triggeredWhen); remove agentType/inputs/repo/budgetIter/hold/fanIn/fanInValidate."},
	"R15": {"R15", "Template resolvable", "A use: references a PlanTemplate; if it isn't in this PR it must already exist on the target cluster.", "Co-submit the referenced template, or confirm it is deployed on the cluster."},
	"R16": {"R16", "Required params", "A template param marked required must be supplied, or expansion fails.", "Add the missing param(s) to the use-step's with: block."},
	"R17": {"R17", "triggeredWhen", "triggeredWhen must name a declared step and a valid phase, or the trigger never fires.", "Point triggeredWhen.step at an existing step and set phase to Running|AwaitingReview|AwaitingApproval|Succeeded."},
	"R18": {"R18", "Expanded name length", "An over-long expanded child name (<plan>-<useStep>-<tmplStep>) is hash-truncated by the controller — it still runs but the name is unreadable.", "Shorten the plan, use-step, or template step names to keep the expanded name under 57 chars."},
	"R19": {"R19", "Test coherence", "test.directed vocabulary is kind-specific; a mismatch is a silent no-op, and a directive without spec.test is ignored.", "Match test.directed to the step kind (pr→merged|closed_unmerged|opened, check→pass|fail, apply→succeed|error) and set spec.test: true."},
	"R21": {"R21", "Inputs by agent type", "A step's inputs are consumed by the agent runtime, so they must match the agentType's contract or the agent Job fails at run time. Dev agents (go/ng/rust/py) consume an Initiative (name + goal + repo + branch, or a repos: list); the infra agent is action-driven (action + per-action fields), not goal-driven.", "Dev steps: inputs {name, repo, branch, goal} (branch required for the legacy single-repo shape; or use repos:[{repo,branch}]). Infra steps: inputs {action, …} (e.g. release-health-check / chart-config). See AGENTS.md."},
	"R20": {"R20", "No unknown fields", "Fields not in the Plan/PlanTemplate CRD are strict-decoded out by the apiserver at apply, so a Plan carrying them cannot be instantiated. Common offenders: a step-level `goal` (belongs under `inputs`) and a spec-level `description` (not a CRD field).", "Use only CRD fields. Put a step goal under `inputs:` (inputs.goal); drop spec-level `description` (use a YAML comment for human context)."},
	"R22": {"R22", "Infra steps live in templates", "Infra steps (agentType leartech-agent-infra) are privileged, cross-cutting operations — release-verify, deploy-health, chart-config. In the PUBLIC catalog a plain Plan must not hand-author them: they belong in curated, OWNERS-gated PlanTemplates and are composed via use:. Hand-authored infra in a submitted Plan is a governance + security hole (a submitter declaring privileged infra work directly) and duplicates the standard verification the deployment path injects. Infra steps are allowed ONLY in kind: PlanTemplate.", "Remove the infra step from the Plan and compose the right PlanTemplate via a use: step instead (e.g. `use: verify-release-flow`). If no template covers your need, propose a new PlanTemplate (which OWNERS review) rather than hand-authoring the infra step."},
	"R23": {"R23", "verify-release-flow is auto-injected", "The platform auto-composes verify-release-flow after every deployable PR step (kind:pr carrying a repo), wired to the REAL merged PR (pr=${steps.<step>.pr}). A hand-authored `use: verify-release-flow` that dependsOn such a step therefore duplicates that injection — two verify-release-flow expansions — and typically also carries a broken hand-set sha. (A standalone verify-release-flow that does NOT dependsOn a PR step — verifying an already-deployed service by explicit sha — is legitimate and allowed.)", "Remove the `use: verify-release-flow` step that dependsOn your PR step — the platform injects it on submission. Keep a hand-authored verify-release-flow only to verify a service NOT produced by a PR step in this Plan (and then supply an explicit commit sha)."},
	"R25": {"R25", "Verdict steps must be deterministic", "A kind: check step handed to a DEV agentType (go/ng/rust/py) cannot produce a deterministic verdict. A dev agent is a single LLM query() session: it exits 0 unless it crashes or hits max-turns, so a goal saying \"FAIL (exit non-zero) if any assertion fails\" is untrue — the runtime discards the verdict (see hub/status/agent-verdict-exit-code-fix-2026-08-05.md, still SPEC). Observed live: a verify step reported six of six assertions un-executed and verdict FAIL in prose while the session exited 0. Deterministic verification belongs on an infra agent with a canned action, which R22 requires be composed via use: a PlanTemplate.", "Replace the hand-authored check with a `use:` step composing a PlanTemplate whose infra step uses a canned action (release-pipeline-status, promote-status, bootjob-for-commit, deploy-health). If no template covers the assertion, propose a new PlanTemplate (OWNERS-reviewed) rather than asserting it in a dev-agent goal."},
	"R24": {"R24", "Resolvable commit sha", "A supplied `with.sha` must be a real commit sha (7–40 hex) so the release/verify checks can resolve the commit; a ref like HEAD or a branch name never resolves at check time and the check fails. With auto-injection you normally omit sha entirely — the injected verify uses the merged PR.", "Set with.sha to a full commit sha (40 hex), or omit it and rely on the injected PR token. Do not use HEAD or a branch name."},
	"R26": {"R26", "GitOps repo targets are template-only", "A step whose target repo is a cluster GitOps repo (jx-build-cluster-*) edits live helmfiles and JX-rendered config-root across the fleet — privileged infra work no matter which agentType runs it. R22 catches this only when the step declares an infra agentType, so a dev agent (e.g. leartech-agent-go) pointed at jx-build-cluster-gsm bypasses it. That is exactly the governance hole R22 exists to close, reached from the repo side rather than the agent side. A one-off GitOps decommission/enablement is operator work (needs-human:gitops-overlay); a recurring one belongs in an OWNERS-gated PlanTemplate.", "Remove the GitOps step from the Plan. Compose the change via an OWNERS-gated infra PlanTemplate (use:), or have an operator apply it manually. A plain catalog Plan must not hand-author edits to jx-build-cluster-* repos."},
	"R27": {"R27", "Goal-prose hazards", "The structural rules cannot see inside a step's free-text inputs.goal, so a hazardous instruction — today, editing the JX-rendered config-root tree — slips through. config-root/ is boot-rendered output; hand-editing it is always wrong because boot overwrites it, and a dev agent reads the goal literally. (Advisory warning — goal prose is heuristic, so this surfaces the hazard for human + ai-review rather than hard-gating.)", "Do not instruct edits to config-root in a step goal. Edit the source (helmfile.yaml / .jx/gitops inputs) and let boot re-render config-root. If the goal merely warns against editing config-root, this is safe to ignore."},
}

// Enriched is a Finding plus its catalog metadata — the shape emitted in
// `plan-lint -json` and consumed by agents.
type Enriched struct {
	Rule    string `json:"rule"`
	Where   string `json:"where"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
	Doc     string `json:"doc,omitempty"`
}

// Enrich joins a Finding with its rule metadata + a doc anchor. docBase is the
// base URL of rules.md (empty → no doc link).
func Enrich(f Finding, docBase string) Enriched {
	e := Enriched{Rule: f.Rule, Where: f.Where, Message: f.Message}
	if m, ok := Rules[f.Rule]; ok {
		e.Fix = m.Fix
	}
	if docBase != "" {
		e.Doc = docBase + "#" + lowerRule(f.Rule)
	}
	return e
}

// lowerRule renders a rule id as a markdown-heading anchor (e.g. "R11" -> "r11").
func lowerRule(r string) string {
	b := []byte(r)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
