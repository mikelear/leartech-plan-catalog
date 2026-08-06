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

The full list with rationale + fixes is `docs/rules.md`. A Plan the catalog accepts is
one the controller will actually run — the rules mirror its reconcile-time validation.
