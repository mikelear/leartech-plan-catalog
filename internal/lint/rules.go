package lint

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

// Rules is the catalog of every lint rule, keyed by ID (R1…R19). Keep in lockstep
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
	"R21": {"R21", "Inputs by agent type", "A step's inputs are consumed by the agent runtime, so they must match the agentType's contract or the agent Job fails at run time. Dev agents (go/ng/rust) consume an Initiative (name+repo+goal); the infra agent is action-driven (action + per-action fields), not goal-driven.", "Dev steps: inputs {name, repo, goal}. Infra steps: inputs {action, …} (e.g. action: release-health-check / chart-config). See AGENTS.md."},
	"R20": {"R20", "No unknown fields", "Fields not in the Plan/PlanTemplate CRD are strict-decoded out by the apiserver at apply, so a Plan carrying them cannot be instantiated. Common offenders: a step-level `goal` (belongs under `inputs`) and a spec-level `description` (not a CRD field).", "Use only CRD fields. Put a step goal under `inputs:` (inputs.goal); drop spec-level `description` (use a YAML comment for human context)."},
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
