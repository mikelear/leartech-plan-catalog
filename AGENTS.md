# AGENTS.md — authoring Plans for the ShipProven Plan Catalog

You are (probably) an AI submitting a **Plan** or **PlanTemplate** to this catalog by
opening a PR. This file tells you, in machine-legible terms, how to produce one that
passes the gate — and how to read the gate's feedback and self-correct.

## The loop

1. Write a Plan to `plans/<name>.yaml` (or a PlanTemplate to `templates/<name>.yaml`).
2. Open a PR. Two checks run:
   - **`plan-lint`** — the deterministic HARD gate (must pass to merge).
   - **`plan-ai-review`** — an advisory model review (never blocks).
3. Read the feedback (see "Reading feedback" below), fix, push again.
4. A human maintainer merges. Merging does **not** run the Plan — execution is a
   separate, human-gated promote/unpause.

## Machine-readable references (read these to author + repair)

- **Rule catalog:** [`docs/rules.json`](docs/rules.json) — every rule as
  `{id, title, why, fix}`. [`docs/rules.md`](docs/rules.md) is the same for humans;
  the PR comment deep-links each failure to it (e.g. `#r11`).
- **JSON Schema:** [`schemas/plan_v1alpha1.json`](schemas/plan_v1alpha1.json) and
  [`schemas/plantemplate_v1alpha1.json`](schemas/plantemplate_v1alpha1.json) —
  validate/repair your YAML against these (the gate runs `kubeconform` against them).
- **Canonical examples:** everything under [`plans/`](plans/) and
  [`templates/`](templates/) is lint-clean — pattern-match them.
- **Review format:** [`docs/review-format.md`](docs/review-format.md) — the one shape
  (`leartech.review/v1`) every review comment uses (human, model, or tool). Parse the
  fenced `json` block in any review to get `verdict` + `findings[{severity, fix, refs}]`.

## Reading feedback

The `plan-lint` PR comment contains a fenced ```json block with this shape — parse
**that**, not the prose:

```json
{ "gate": "plan-lint", "pass": false,
  "errors": [
    { "rule": "R8", "where": "plans/x.yaml step[0]=a",
      "message": "dependsOn references unknown step \"ghost\"",
      "fix": "Point dependsOn at existing step names, or remove the dangling entry.",
      "doc": ".../docs/rules.md#r8" } ] }
```

For each error: apply `fix`, consult `doc`, re-validate against the schema, push.
`plan-ai-review` adds advisory design feedback (verdict + specifics) — address it
where sound, but only `plan-lint` blocks the merge.

## Hard requirements (the ones that most often fail)

- **`spec.paused: true`** on every Plan (R5) — hold-by-default; a catalog Plan is a proposal.
- **`apiVersion: agent.leartech.io/v1alpha1`**, **kind** `Plan`|`PlanTemplate` (R2/R3).
- **Short DNS-1123 name**, ≤58 chars (R4).
- Each concrete step has a **name**, **kind** (`pr`|`apply`|`check`) and **agentType** (R6);
  a `use:` step carries only name/use/with/dependsOn (R14).
- **Acyclic** `dependsOn` that resolves to real steps; no self-deps (R7/R8/R11/R12).
- **No laptop/absolute paths** — reference only repo-committed or defined-store artifacts (R10).

## Step `inputs` by agent type (the shape that most often bites)

A step's `inputs` is projected verbatim into the AgentRun and consumed by the agent
runtime — so it must match what that `agentType` expects, or the agent Job fails at
run time (there is no `goal` field at step level; it goes **inside** `inputs`). R20
(no unknown CRD fields) + R21 (inputs shape per agentType) enforce this.

- **Dev agents** — `leartech-agent-go`, `leartech-agent-ng`, `leartech-agent-rust`.
  `inputs` is an **Initiative**:
  ```yaml
  inputs:
    name: kebab-id            # required — short kebab id (also the branch suffix)
    repo: owner/name          # required — owner defaults to mikelear if omitted
    branch: feat/thing        # optional — defaults from name
    goal: |                   # required — what the agent must achieve + the done-check
      …
  ```
- **Infra agent** — `leartech-agent-infra`. `inputs` is **action-driven** (a fixed
  vocabulary, not a free-form goal):
  ```yaml
  inputs:
    action: chart-config             # or: release-health-check, …
    cluster: gcp                     # action-specific fields
    service: leartech-plan-api
    goal: | …                        # config actions take a goal; health checks don't
  ```
  Known actions: `chart-config` (`cluster`, `service`, `goal`), `release-health-check`
  (`service`, `namespace`, `budgetMinutes`). Use a canned action — the infra agent does
  not run arbitrary free-form goals.

The full rule list with rationale + fixes is `docs/rules.md`. A Plan the catalog accepts
is one the controller will admit **and** the agents can actually run.
