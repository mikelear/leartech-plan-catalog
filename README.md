# ShipProven Plan Catalog

> _Autonomous code, proven before it ships — applied to the Plans themselves._

A **public, submittable catalog** of Plans and PlanTemplates for the ShipProven /
LearTech agent platform (`agent.leartech.io/v1alpha1`). Anyone — human or AI — can
open a PR proposing a Plan. Every submission is **gated by the same pipeline-as-judge
philosophy we apply to code**:

1. **`plan-lint` — the hard gate (deterministic, Go).** Structural + safety checks
   on every Plan/PlanTemplate. Non-zero exit blocks the merge. This is the
   un-bypassable governance surface. It _grows_ — each rule encodes a lesson paid
   for in a real run (see `internal/lint`, rules R1–R19 — structural + safety +
   DAG/expansion checks ported from the controller's own reconcile-time
   validation, so a Plan the catalog accepts is one the controller will run).
   Every deterministic (non-LLM) check in this catalog is Go, with unit tests.
2. **`plan-ai-review` — the judgment layer (advisory).** Routes each submission
   through **our own AI gateway**, with **our own virtual key**, to one or more ML
   models that score design quality a linter can't. Posts a sticky PR comment.
   Never blocks — but every call feeds a **proprietary Plan-quality dataset** that
   trains models we own.

## Why this exists (the product story)

This repo is a live demonstration of the ShipProven pitch:

- **Un-bypassable governance.** The pipeline is the judge. A Plan cannot enter the
  catalog without passing deterministic gates, and it cannot _execute_ without a
  separate human decision (see hold-by-default below).
- **The owned gateway is the moat.** Every model call — including these Plan
  reviews — goes through the gateway we control. That means consensus governance,
  cost/routing control, and, crucially, **our own data**.
- **A compounding flywheel.** Plan reviews + real run outcomes become training data
  no one else has. Over time the Plan-quality models become _ours_, fine-tuned on
  our own system. Model growth we can measure and show.
- **A growable safety catalog.** We add guarantees by adding lint rules and, later,
  whole new Tekton verification steps. The catalog gets safer as we learn.

## Hold-by-default (safety)

Every Plan in this catalog **MUST** set `spec.paused: true`. A catalog Plan is a
**proposal**, not a running job. Merging it does **not** execute it. Execution is a
separate, explicitly human-gated promote/unpause — the same boundary `plan-api`
enforces. `plan-lint` rule **R5** makes this un-skippable.

## Strict human merge — no auto-merge

There is **no auto-merge** in this repo. Merges are made by a small, explicitly
trusted set of maintainers (`OWNERS`), which grows deliberately over time. Passing
gates makes a PR _eligible_; a human still decides. In the future, tenants running
our product can host their own catalog and manage their own merge policy.

## Submitting a Plan

1. Add a Plan to `plans/` or a PlanTemplate to `templates/` (one YAML per file).
2. Ensure `spec.paused: true` (Plans) and keep names short (≤58 chars — an AgentRun
   whose name overflows the 63-byte pod label never spawns).
3. Open a PR. `plan-lint` must pass; read the `plan-ai-review` comment and address
   concerns.
4. A maintainer reviews and merges. Promotion/execution is a separate step.

## Layout

```
plans/           concrete Plans (hold-by-default proposals)
templates/       reusable PlanTemplates (composed via `use:` + `with:`)
cmd/plan-lint/   the deterministic hard gate (Go entrypoint)
internal/lint/   the rules engine (R1–R19) + unit tests
scripts/         plan-ai-review.sh (advisory, LLM via the owned gateway)
.lighthouse/     Tekton presubmit wiring both steps
OWNERS           trusted maintainers (no auto-merge)
```

## Running the gate locally

```sh
go test ./...          # the rule unit tests
go run ./cmd/plan-lint # lint plans/ + templates/
```

Same code runs in CI and on a laptop — no drift.
