# ShipProven Plan Catalog

> _Autonomous code, proven before it ships — applied to the Plans themselves._

A **public, submittable catalog** of Plans and PlanTemplates for the ShipProven /
LearTech agent platform (`agent.leartech.io/v1alpha1`). Anyone — human or AI — can
open a PR proposing a Plan. Every submission is **gated by the same pipeline-as-judge
philosophy we apply to code**:

1. **`plan-lint` — the hard gate (deterministic, Go).** Two layers, both
   non-zero-exit-blocks-merge:
   - **Semantic + safety + DAG rules** (`internal/lint`, R1–R20) — ported from the
     controller's own reconcile-time validation, so a Plan the catalog accepts is
     one the controller will actually run (hold-by-default, cycle detection,
     fan-in shape, template expansion, dependsOn resolution, …).
   - **CRD-schema conformance** (`kubeconform`) — every submission is validated
     against a JSON Schema auto-derived from the Plan CRD (`schemas/`), catching
     wrong types, bad enums, missing required fields, and pattern violations. The
     schema tracks the CRD automatically, so structural rules never drift.

   This is the un-bypassable governance surface, and it _grows_ — every
   deterministic (non-LLM) check is Go, with unit tests.
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

## After merge: templates auto-release via GitOps

Merging **publishes** to the library; it doesn't run anything. On merge, a
postsubmit ([`scripts/sync-templates-to-controller.sh`](scripts/sync-templates-to-controller.sh))
mirrors each `templates/*.yaml` into the controller's PlanTemplate library as a
**GitOps PR** — the single install path for templates. That PR re-runs the
controller's own quality gates + a human merge → controller release → GitOps
renders and applies the CRD. So a template is double-gated (here + there) and
never bypasses GitOps.

Concrete **Plans** are instantiated explicitly (via MCP `create_plan` → `plan-api`,
or the Portal) and always land **paused** — a second, human-gated promote/unpause
controls execution. The full three-gate lifecycle (with `paused` enforced at every
layer) is documented in [`docs/plan-lifecycle.md`](docs/plan-lifecycle.md).

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
AGENTS.md        agent-facing authoring guide (how an AI submits + repairs a Plan)
plans/           concrete Plans (hold-by-default proposals)
templates/       reusable PlanTemplates (composed via `use:` + `with:`)
cmd/plan-lint/   the deterministic hard gate (Go entrypoint; -json for agents)
cmd/crd2schema/  generates schemas/ JSON Schema from the vendored CRDs
cmd/rulesdoc/    generates docs/rules.{json,md} from the rule catalog
internal/lint/   the rules engine (R1–R20) + rule metadata + unit tests
schemas/         CRD-derived JSON Schema (kubeconform) + vendored CRDs under crd/
docs/            rules.json + rules.md (generated) · flywheel.md
scripts/         plan-ai-review.sh (advisory, LLM via the owned gateway) + plan-lint-comment.sh
.lighthouse/     Tekton presubmits: pullrequest.yaml (plan-lint hard gate) + plan-ai-review (advisory)
OWNERS           trusted maintainers (no auto-merge)
```

## Feedback is dual-audience

Both checks post a **sticky PR comment** built for humans _and_ the AI that submitted
the Plan: colored GitHub callouts + a badge + an error table (rule → location →
problem → fix) for humans, and an embedded machine-readable JSON verdict (each error
carrying a `fix` hint + a `doc` anchor) an agent parses to self-correct. The comment
links the machine-legible references — [`AGENTS.md`](AGENTS.md), the generated rule
catalog ([`docs/rules.md`](docs/rules.md) / [`docs/rules.json`](docs/rules.json)), the
JSON schema, and the examples — so an agent can read the failure and repair its Plan.

Every review — from a human, a model, or a tool — follows one documented shape,
[`leartech.review/v1`](docs/review-format.md): a colored human section plus a
machine-readable `json` block (`verdict` + `findings[{severity, fix, refs}]`). Uniform
across all reviewers, so each review is one clean row for the Plan-quality flywheel.

The presubmits surface as separate GitHub check contexts:
- **`pr`** (`pullrequest.yaml`) — the deterministic Go gate (R1–R21) + kubeconform. Required.
- **`plan-cluster-verify`** — server-side dry-run of every Plan/PlanTemplate against the
  **live CRD** (structural schema + admission; persists nothing). The authoritative,
  drift-free check that a submission will actually apply — complements the offline Go
  rules. Required-eligible.
- **`plan-ai-review`** — advisory (`optional: true`).

## Running the gate locally

```sh
make test     # rule unit tests
make lint     # rules engine + CRD-schema conformance (kubeconform)
make verify   # fail if schemas/ is stale vs the vendored CRDs
make schemas  # regenerate schemas/ after refreshing the CRDs
```

Same code runs in CI and on a laptop — no drift.
