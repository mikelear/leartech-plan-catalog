// Package planlint is the DETERMINISTIC gate for the ShipProven Plan Catalog.
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
//
// The graph + expansion rules (R11–R19) are ported directly from the controller's
// own reconcile-time validation (validateDAG / detectCycle / validateFanInShape /
// ExpandPlanSteps in leartech-orchestrator-controller) so that a Plan the catalog
// accepts is a Plan the controller will actually run — the gate catches at PR time
// exactly what would otherwise become a terminal Failed at reconcile.
package planlint

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
// pod-template label as <plan>-<step>-<attempt>; keep the Plan name short so the
// child names stay readable. (The controller hash-truncates overflow rather than
// failing, so this is a sanity/readability cap, enforced hard for the catalog.)
const MaxName = 58

// maxExpandedRunName mirrors the controller's MaxPlanAgentRunNameLen (57): the
// deterministic child-AgentRun name budget before it hash-truncates. Beyond this
// the run still spawns (hash-truncated) but the name is no longer readable — so
// we WARN, not fail.
const maxExpandedRunName = 57

var (
	stepKinds    = map[string]bool{"pr": true, "apply": true, "check": true}
	sortedKinds  = []string{"apply", "check", "pr"}
	nameRE       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`) // DNS-1123 label
	laptopRE     = regexp.MustCompile(`(/Users/|/home/|~/)`)
	triggerPhase = map[string]bool{"Running": true, "AwaitingReview": true, "AwaitingApproval": true, "Succeeded": true}
	// forbidden fields on a `use:` step — mirrors controller validateUseStepShape.
	// (kind + triggeredWhen are NOT forbidden there; only these smuggle runtime.)
	useForbidden = []string{"agentType", "inputs", "repo", "budgetIter", "hold", "fanIn", "fanInValidate"}
	// Allowlists of accepted field names, taken verbatim from the Plan/PlanTemplate
	// CRD openAPIV3Schema. Anything else is rejected by the apiserver's strict
	// decoding at apply time — most commonly a step `goal` (which belongs under
	// `inputs`) or a spec-level `description` (not a CRD field). R20 catches these.
	planSpecKeys     = strset("command", "paused", "remediates", "steps", "tenant", "test", "triggeredBy")
	templateSpecKeys = strset("params", "steps")
	stepKeys         = strset("agentType", "budgetIter", "dependsOn", "fanIn", "fanInValidate", "hold", "inputs", "kind", "name", "onFailure", "repo", "test", "triggeredWhen", "use", "with")
	paramKeys        = strset("name", "required")
	// Agent-type input contracts (R21). Dev agents consume an Initiative
	// (name+repo+branch+goal); the infra agent is action-driven. AgentType CRs
	// carry no InputSchema, so these known runtime contracts are encoded here.
	//
	// The dev set is the agentTypes whose AgentType.spec.entrypoint is empty —
	// they run the image's default entrypoint (job_adapter → run_initiative →
	// load_initiative), which validates against the Initiative model. On the live
	// cluster that's go, ng, rust AND py (py shares the default entrypoint). ba and
	// infra declare their own entrypoints (gate.agent.ba_agent / .infra_agent);
	// infra is action-driven (checked below). ba's contract isn't encoded here —
	// it falls through unvalidated rather than risk asserting a wrong shape.
	devAgentTypes = strset("leartech-agent-go", "leartech-agent-ng", "leartech-agent-rust", "leartech-agent-py")
	// autoInjectedTemplates are PlanTemplates the platform auto-composes on
	// submission (plan-api injection) after a deployable PR step. A Plan must not
	// hand-author them after its own PR step — that duplicates the injection (R23).
	autoInjectedTemplates = strset("verify-release-flow")
	// shaRE matches a git commit sha (7–40 hex). A with.sha that isn't one (e.g.
	// the literal HEAD or a branch name) never resolves at check time (R24).
	shaRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// infraAgentType is the privileged infra agent (release-verify / deploy-health /
// chart-config). Concrete steps using it are allowed ONLY in PlanTemplates (R22):
// a plain catalog Plan must compose infra via a use: template, never hand-author it.
const infraAgentType = "leartech-agent-infra"

// lintInputs (R21) checks a concrete step's inputs match what its agentType's
// runtime consumes — the agent Job fails at run time otherwise (a `goal`-only
// input is the classic miss). Dev agents want an Initiative (name+repo+goal);
// the infra agent wants an action.
func lintInputs(agentType string, s map[string]any, sw string, f *Findings) {
	inp := mapOf(s["inputs"])
	switch {
	case devAgentTypes[agentType]:
		if asStr(inp["name"]) == "" {
			f.err("R21", sw, "dev-agent step inputs need `name` (the Initiative shape is inputs: {name, repo, branch, goal})")
		}
		if asStr(inp["goal"]) == "" {
			f.err("R21", sw, "dev-agent step inputs need `goal` (under inputs, not at step level)")
		}
		// Initiative: legacy single-repo requires BOTH repo and branch; or the new
		// `repos: [{repo, branch, base?}]` list. (base defaults to main.)
		if !has(inp, "repos") {
			if asStr(inp["repo"]) == "" {
				f.err("R21", sw, "dev-agent step inputs need `repo` (or a `repos:` list)")
			}
			if asStr(inp["branch"]) == "" {
				f.err("R21", sw, "dev-agent step inputs need `branch` — the Initiative's legacy single-repo shape requires both repo and branch (or use a `repos:` list)")
			}
		}
	case agentType == "leartech-agent-infra":
		if asStr(inp["action"]) == "" {
			f.err("R21", sw, "infra-agent step inputs need `action` (e.g. chart-config, release-health-check) — the infra agent is action-driven, not goal-driven")
		}
	}
}

func strset(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// unknownKeys returns the sorted keys of m not present in allowed.
func unknownKeys(m map[string]any, allowed map[string]bool) []string {
	var out []string
	for k := range m {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// lintUnknown (R20) flags fields not in the CRD schema — the apiserver strict-
// decodes them out at apply, so a Plan carrying them cannot be instantiated. The
// classic offenders are a step-level `goal` (belongs under `inputs`) and a
// spec-level `description` (not a CRD field).
func lintUnknown(m map[string]any, allowed map[string]bool, scope, where string, f *Findings) {
	uk := unknownKeys(m, allowed)
	if len(uk) == 0 {
		return
	}
	hint := ""
	for _, k := range uk {
		if k == "goal" {
			hint = " — a step goal goes under `inputs:` (inputs.goal), not at step level"
			break
		}
	}
	f.err("R20", where, fmt.Sprintf("unknown %s field(s) %v — not in the Plan CRD; the apiserver strict-decodes them out at apply%s", scope, uk, hint))
}

// Finding is one structured rule result — machine-readable so a verdict store /
// training flywheel can consume it (the ai-review layer + real run outcomes are
// keyed off the same shape).
type Finding struct {
	Rule    string `json:"rule"`
	Where   string `json:"where"`
	Message string `json:"message"`
}

// String renders a finding as "[rule] where: message".
func (f Finding) String() string {
	return fmt.Sprintf("[%s] %s: %s", f.Rule, f.Where, f.Message)
}

// Findings accumulates hard errors and advisory warnings.
type Findings struct {
	Errors []Finding
	Warns  []Finding
}

func (f *Findings) err(rule, where, msg string) {
	f.Errors = append(f.Errors, Finding{Rule: rule, Where: where, Message: msg})
}

func (f *Findings) warn(rule, where, msg string) {
	f.Warns = append(f.Warns, Finding{Rule: rule, Where: where, Message: msg})
}

// TemplateMeta is the cross-document index entry for a PlanTemplate present in
// the same PR: enough to validate use-steps that reference it (R15/R16/R18).
type TemplateMeta struct {
	RequiredParams []string
	StepNames      []string
}

// TemplateIndex maps PlanTemplate name → its metadata, built across every
// document in the submission so a Plan and the template it uses can be
// co-submitted and cross-checked.
type TemplateIndex map[string]TemplateMeta

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

	type parsed struct {
		path string
		doc  map[string]any
	}
	var docs []parsed
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
				docs = append(docs, parsed{path, doc})
			}
		}
	}

	// First pass: index every PlanTemplate so co-submitted Plans can be checked
	// against the templates they `use:`.
	idx := TemplateIndex{}
	for _, p := range docs {
		if asStr(p.doc["kind"]) != "PlanTemplate" {
			continue
		}
		name := asStr(mapOf(p.doc["metadata"])["name"])
		if name == "" {
			continue
		}
		idx[name] = templateMeta(mapOf(p.doc["spec"]))
	}

	for _, p := range docs {
		LintDoc(p.doc, p.path, f, idx)
	}
	return f, len(files), nil
}

func templateMeta(spec map[string]any) TemplateMeta {
	var m TemplateMeta
	for _, raw := range sliceOf(spec["params"]) {
		p := mapOf(raw)
		if b, ok := p["required"].(bool); ok && b {
			m.RequiredParams = append(m.RequiredParams, asStr(p["name"]))
		}
	}
	for _, raw := range sliceOf(spec["steps"]) {
		m.StepNames = append(m.StepNames, asStr(mapOf(raw)["name"]))
	}
	return m
}

// LintDoc applies the structural + safety rules to a single decoded document.
func LintDoc(doc map[string]any, where string, f *Findings, idx TemplateIndex) {
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
	name := asStr(mapOf(doc["metadata"])["name"])
	if name == "" {
		f.err("R4", where, "metadata.name is required")
	} else {
		if !nameRE.MatchString(name) {
			f.err("R4", where, fmt.Sprintf("metadata.name %q is not a DNS-1123 label", name))
		}
		if len(name) > MaxName {
			f.err("R4", where, fmt.Sprintf("metadata.name %q is %d>%d chars — the AgentRun will never spawn cleanly (63-byte label cap)", name, len(name), MaxName))
		}
	}

	spec, ok := asMap(doc["spec"])
	if !ok || len(spec) == 0 {
		f.err("R1", where, "spec is required and must be a mapping")
		return
	}

	switch kind {
	case "Plan":
		lintPlan(name, spec, where, f, idx)
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

func lintPlan(planName string, spec map[string]any, where string, f *Findings, idx TemplateIndex) {
	// R5 HOLD-BY-DEFAULT — the load-bearing safety rule. A catalog Plan is a
	// PROPOSAL: it must NOT be able to execute on merge. Execution is a separate,
	// human-gated promote/unpause. The reconciler honors spec.paused=true.
	if b, ok := spec["paused"].(bool); !ok || !b {
		f.err("R5", where, "spec.paused MUST be true — catalog Plans are hold-by-default proposals; execution is a separate human promote/unpause")
	}
	// R20 no unknown spec fields (the apiserver strict-decodes them out at apply).
	lintUnknown(spec, planSpecKeys, "spec", where, f)

	steps, ok := asSlice(spec["steps"])
	if !ok || len(steps) == 0 {
		f.err("R6", where, "spec.steps must be a non-empty list")
		return
	}
	planIsTest, _ := spec["test"].(bool)

	// Per-step shape checks (R6 concrete shape, R14 use-step fields, R15/R16
	// template resolution, R18 expanded-name length, R19 test coherence).
	for i, raw := range steps {
		s, isMap := asMap(raw)
		name := asStr(s["name"])
		sw := fmt.Sprintf("%s step[%d]=%s", where, i, orQ(name))
		if !isMap || name == "" {
			f.err("R6", sw, "each step needs a name")
			continue
		}
		// R20 no unknown step fields (e.g. a top-level `goal` — it goes in `inputs`).
		lintUnknown(s, stepKeys, "step", sw, f)
		switch {
		case has(s, "use"):
			// A use-step's kind must still be a valid enum — kind: gremlin + use
			// would otherwise slip past the concrete-only check in default.
			if k := asStr(s["kind"]); k != "" && !stepKinds[k] {
				f.err("R6", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, k))
			}
			lintUseStep(planName, s, sw, f, idx)
		case truthy(s["fanIn"]):
			// fan-in is a no-agent GATE step; its DAG shape is checked in
			// graphChecks (R13). Its kind must still be a valid enum, and it must
			// NOT carry agentType — mixing a gate with a dev-agent step is a
			// wiring bug the controller would silently drop.
			if k := asStr(s["kind"]); k != "" && !stepKinds[k] {
				f.err("R6", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, k))
			}
			if at := asStr(s["agentType"]); at != "" {
				f.err("R13", sw, "fan-in step must not declare agentType (it is a no-agent gate — the agent work belongs in the steps it fans in from)")
			}
		default:
			if k := asStr(s["kind"]); !stepKinds[k] {
				f.err("R6", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, k))
			}
			if at := asStr(s["agentType"]); at == "" {
				f.err("R6", sw, "concrete step needs agentType")
			} else {
				// R22 infra steps are template-only. lintPlan runs for kind: Plan
				// only, so any concrete infra step here is a violation — infra is
				// composed via a use: PlanTemplate, never hand-authored in a Plan.
				if at == infraAgentType {
					f.err("R22", sw, "infra steps (agentType leartech-agent-infra) are not allowed in a plain Plan — compose a PlanTemplate via `use:` (e.g. use: verify-release-flow); infra steps belong only in PlanTemplates")
				}
				lintInputs(at, s, sw, f) // R21 inputs shape by agentType
			}
		}
		// R19 test/kind coherence
		lintTestDirected(planIsTest, s, sw, f)
	}

	lintAutoInject(steps, where, f)
	graphChecks(steps, where, f)
}

// lintAutoInject (R23) rejects a hand-authored use:<auto-injected-template> step
// that dependsOn a deployable PR step — the platform injects that verification on
// submission (wired to the merged PR), so hand-authoring it duplicates the
// expansion (and usually carries a broken hand-set sha). The deployable-PR test
// mirrors the plan-api injection trigger: a concrete (non-use, non-fanIn) step
// whose kind is pr (empty coerces to pr) AND that carries a repo. A STANDALONE
// verify-release-flow (no dependsOn on such a step — verifying an already-deployed
// service by explicit sha) is legitimate and left alone. Runs on the SUBMITTED
// plan; plan-api validates BEFORE it injects, so its own injected step (added
// after validation) never trips this.
func lintAutoInject(steps []any, where string, f *Findings) {
	prSteps := map[string]bool{}
	for _, raw := range steps {
		s := mapOf(raw)
		if has(s, "use") || truthy(s["fanIn"]) {
			continue
		}
		// Mirror the plan-api injection trigger: a deployable PR step is kind:pr
		// (empty coerces to pr) carrying a repo EITHER at step level OR under
		// inputs.repo (the natural R21 dev-agent shape — injection falls back to it).
		if normalizeKind(asStr(s["kind"])) == "pr" && stepRepo(s) != "" {
			if n := asStr(s["name"]); n != "" {
				prSteps[n] = true
			}
		}
	}
	if len(prSteps) == 0 {
		return
	}
	for i, raw := range steps {
		s := mapOf(raw)
		use := asStr(s["use"])
		if !autoInjectedTemplates[use] {
			continue
		}
		sw := fmt.Sprintf("%s step[%d]=%s", where, i, orQ(asStr(s["name"])))
		for _, dep := range strSlice(s["dependsOn"]) {
			if prSteps[dep] {
				f.err("R23", sw, fmt.Sprintf("`use: %s` dependsOn PR step %q — the platform auto-injects this verification after deployable PR steps (wired to the merged PR). Remove this step; injection composes it.", use, dep))
				break
			}
		}
	}
}

// stepRepo returns a step's repo, preferring the step-level `repo` and falling
// back to inputs.repo — mirroring the plan-api injection trigger (deployable when
// either is set). Authors commonly put repo only under inputs (the R21 shape).
func stepRepo(s map[string]any) string {
	if r := asStr(s["repo"]); r != "" {
		return r
	}
	return asStr(mapOf(s["inputs"])["repo"])
}

func lintUseStep(planName string, s map[string]any, sw string, f *Findings, idx TemplateIndex) {
	// R14 a `use:` step must not smuggle runtime fields (the template supplies
	// them). Mirrors controller validateUseStepShape — expansion hard-fails on
	// these.
	var bad []string
	for _, field := range useForbidden {
		if has(s, field) {
			bad = append(bad, field)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		f.err("R14", sw, fmt.Sprintf("a `use:` step MUST NOT declare: %s (the template supplies these)", strings.Join(bad, ", ")))
	}

	// R24 a supplied with.sha must be a resolvable commit sha (7–40 hex); the
	// literal HEAD or a branch name never resolves at check time. Checked here so
	// it applies whether or not the template is co-submitted.
	if sha := asStr(mapOf(s["with"])["sha"]); sha != "" && !shaRE.MatchString(sha) {
		f.err("R24", sw, fmt.Sprintf("with.sha %q is not a commit sha — use a full 40-hex sha, or omit sha (the injected verify uses the merged PR); HEAD/branch names never resolve", sha))
	}

	tmplName := asStr(s["use"])
	tmpl, present := idx[tmplName]
	if !present {
		// The template may exist on the target cluster but isn't co-submitted —
		// we can't prove it's missing, so warn rather than fail.
		f.warn("R15", sw, fmt.Sprintf("references PlanTemplate %q which is not in this PR — ensure it exists on the target cluster", tmplName))
		return
	}
	// R16 every required param must be supplied in `with:`.
	with := mapOf(s["with"])
	for _, p := range tmpl.RequiredParams {
		if _, ok := with[p]; !ok {
			f.err("R16", sw, fmt.Sprintf("PlanTemplate %q requires param %q, not supplied in `with:`", tmplName, p))
		}
	}
	// R18 warn if the expanded child-AgentRun name would overflow and get
	// hash-truncated (still runs, just less readable/debuggable).
	useName := asStr(s["name"])
	for _, ts := range tmpl.StepNames {
		full := planName + "-" + useName + "-" + ts
		if len(full) > maxExpandedRunName {
			f.warn("R18", sw, fmt.Sprintf("expanded step name %q is %d>%d chars — the controller will hash-truncate it (less readable); shorten the step or template name", full, len(full), maxExpandedRunName))
		}
	}
}

func lintTestDirected(planIsTest bool, s map[string]any, sw string, f *Findings) {
	t, ok := asMap(s["test"])
	if !ok {
		return
	}
	directed := asStr(t["directed"])
	if directed == "" {
		return
	}
	if !planIsTest {
		f.warn("R19", sw, "step sets test.directed but spec.test is not true — the directive is ignored")
		return
	}
	if !validDirected(asStr(s["kind"]), directed) {
		f.err("R19", sw, fmt.Sprintf("test.directed %q is not valid for kind %q (pr→merged|closed_unmerged|opened, check→pass|fail, apply→succeed|error)", directed, normalizeKind(asStr(s["kind"]))))
	}
}

func lintTemplate(spec map[string]any, where string, f *Findings) {
	// R9 templates declare params + steps; template steps are concrete (no nested
	// use: — expansion is depth-1) and carry kind+agentType (unless fan-in).
	if _, ok := asSlice(spec["params"]); !ok {
		f.warn("R9", where, "spec.params should be a list of {name, required}")
	}
	// R20 no unknown template-spec fields.
	lintUnknown(spec, templateSpecKeys, "spec", where, f)
	for _, raw := range sliceOf(spec["params"]) {
		if p, ok := asMap(raw); ok {
			lintUnknown(p, paramKeys, "param", where, f)
		}
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
		// R20 no unknown step fields.
		lintUnknown(s, stepKeys, "step", sw, f)
		if has(s, "use") {
			f.err("R9", sw, "template steps must NOT nest `use:` (expansion is depth-1)")
		}
		if truthy(s["fanIn"]) {
			continue // fan-in shape checked in graphChecks
		}
		if k := asStr(s["kind"]); !stepKinds[k] {
			f.err("R9", sw, fmt.Sprintf("kind must be one of %v (got %q)", sortedKinds, k))
		}
		if at := asStr(s["agentType"]); at != "" {
			lintInputs(at, s, sw, f) // R21 inputs shape by agentType
		}
		if asStr(s["agentType"]) == "" {
			f.err("R9", sw, "template step needs agentType")
		}
	}

	graphChecks(steps, where, f)
}

// graphChecks runs the DAG-level rules shared by Plan and PlanTemplate steps:
// R7 unique names, R8 dependsOn resolves, R11 no cycle, R12 no self-dependency,
// R13 fan-in shape, R17 triggeredWhen well-formed. Ported from the controller's
// validateDAG / detectCycle / validateFanInShape.
func graphChecks(steps []any, where string, f *Findings) {
	names := map[string]bool{}
	var dupes []string
	for _, raw := range steps {
		n := asStr(mapOf(raw)["name"])
		if n == "" {
			continue
		}
		if names[n] {
			dupes = append(dupes, n)
		}
		names[n] = true
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		f.err("R7", where, fmt.Sprintf("duplicate step names: %v", uniq(dupes)))
	}

	for i, raw := range steps {
		s := mapOf(raw)
		name := asStr(s["name"])
		sw := fmt.Sprintf("%s step[%d]=%s", where, i, orQ(name))
		// R8 + R12 dependsOn resolves and isn't self-referential
		for _, dep := range strSlice(s["dependsOn"]) {
			if !names[dep] {
				f.err("R8", sw, fmt.Sprintf("dependsOn references unknown step %q", dep))
			}
			if dep == name && name != "" {
				f.err("R12", sw, "step depends on itself")
			}
		}
		// R17 triggeredWhen well-formed
		if tw, ok := asMap(s["triggeredWhen"]); ok {
			target := asStr(tw["step"])
			if target != "" && !names[target] {
				f.err("R17", sw, fmt.Sprintf("triggeredWhen references unknown step %q", target))
			}
			if target == name && name != "" {
				f.err("R17", sw, "triggeredWhen references itself")
			}
			if ph := asStr(tw["phase"]); ph != "" && !triggerPhase[ph] {
				f.err("R17", sw, fmt.Sprintf("triggeredWhen.phase %q must be one of [AwaitingApproval AwaitingReview Running Succeeded]", ph))
			}
		}
		// R13 fan-in shape
		lintFanIn(s, name, sw, f)
	}

	if cyc := detectCycle(steps); cyc != "" {
		f.err("R11", where, cyc)
	}
}

// lintFanIn mirrors controller validateFanInShape: fan-in ⇒ ≥1 dependsOn AND ≥1
// fanInValidate; non-fan-in ⇒ no fanInValidate.
func lintFanIn(s map[string]any, name, sw string, f *Findings) {
	fanIn := truthy(s["fanIn"])
	validate := strSlice(s["fanInValidate"])
	deps := strSlice(s["dependsOn"])
	if fanIn {
		if len(deps) == 0 {
			f.err("R13", sw, "fan-in step must declare at least one dependsOn (nothing to fan in from)")
		}
		if len(validate) == 0 {
			f.err("R13", sw, "fan-in step must declare at least one fanInValidate path (an empty gate silently passes)")
		}
		return
	}
	if len(validate) > 0 {
		f.err("R13", sw, "non-fan-in step must not declare fanInValidate (set fanIn: true)")
	}
	_ = name
}

// detectCycle runs a DFS over the dependsOn graph. Returns "" when acyclic, else
// a message naming a step involved in the cycle. Ported from the controller's
// detectCycle (white/grey/black tri-colour DFS).
func detectCycle(steps []any) string {
	deps := map[string][]string{}
	var order []string
	for _, raw := range steps {
		s := mapOf(raw)
		n := asStr(s["name"])
		if n == "" {
			continue
		}
		order = append(order, n)
		deps[n] = strSlice(s["dependsOn"])
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(string) string
	visit = func(name string) string {
		color[name] = grey
		for _, dep := range deps[name] {
			switch color[dep] {
			case grey:
				return fmt.Sprintf("cycle detected involving step %q", dep)
			case white:
				if _, known := deps[dep]; known {
					if msg := visit(dep); msg != "" {
						return msg
					}
				}
			}
		}
		color[name] = black
		return ""
	}
	for _, n := range order {
		if color[n] == white {
			if msg := visit(n); msg != "" {
				return msg
			}
		}
	}
	return ""
}

// --- small typed accessors over the generic YAML tree -----------------------

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func has(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
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

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
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

func normalizeKind(kind string) string {
	switch kind {
	case "apply":
		return "apply"
	case "check":
		return "check"
	default:
		return "pr"
	}
}

func validDirected(kind, directed string) bool {
	switch normalizeKind(kind) {
	case "check":
		return directed == "pass" || directed == "fail"
	case "apply":
		return directed == "succeed" || directed == "error"
	default: // pr
		return directed == "merged" || directed == "closed_unmerged" || directed == "opened"
	}
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
