# ShipProven Plan Catalog

> _Autonomous code, proven before it ships — applied to the Plans themselves._

A **public, submittable catalog** of Plans and PlanTemplates for the ShipProven /
LearTech agent platform (`agent.leartech.io/v1alpha1`). Anyone — human or AI — can
open a PR proposing a Plan. Every submission is **gated by the same pipeline-as-judge
philosophy we apply to code**:

1. **`plan-lint` — the hard gate (deterministic, Go).** Two layers, both
   non-zero-exit-blocks-merge:
   - **Semantic + safety + DAG rules** (`pkg/planlint`, R1–R22) — ported from the
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
   models that score design quality a linter can't. It is fed the catalog's
   available PlanTemplates and **points submitters at the templates they should
   compose via `use:`** — e.g. flagging a `kind:pr` step that lands a deployed
   service but has no `use: verify-release-flow` gate after it, and showing where
   to add it. Posts a sticky PR comment. Never blocks — but every call feeds a
   **proprietary Plan-quality dataset** that trains models we own. (Those accepted
   suggestions are the same policy the deployment path will later auto-inject.)

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

Concrete **Plans** reach the estate through `plan-api` — the single writer that
validates, applies the auto-composition injection policy, and creates the Plan CRD.
Three front doors converge on it: MCP `create_plan` (forwards a user bearer), the
Portal, and — on merge — this repo's release postsubmit
([`cmd/plan-submit`](cmd/plan-submit)), which submits every `plans/*.yaml`
(excluding `example-*`) to plan-api with an s2s token. The release runs on **both
clusters**, each self-submitting to its local plan-api, so both estates receive the
proposal. However it arrives, a Plan always lands **paused** — a second,
human-gated promote/unpause controls execution. The full three-gate lifecycle
(with `paused` enforced at every layer) is documented in
[`docs/plan-lifecycle.md`](docs/plan-lifecycle.md).

> The catalog auto-submit leg stays **dormant** until the `plan-catalog-release`
> s2s secret is provisioned (keys: `client-id`, `client-secret`, `token-url`,
> `plan-api-url`, `audience`) — the client needs the `internal_services` scope
> (one of the three `plan-api` accepts for `POST /plans`). Until then the step
> self-skips, so merging this repo is safe before provisioning lands.

## Strict human merge — no auto-merge

There is **no auto-merge** in this repo. Merges are made by a small, explicitly
trusted set of maintainers (`OWNERS`), which grows deliberately over time. Passing
gates makes a PR _eligible_; a human still decides. In the future, tenants running
our product can host their own catalog and manage their own merge policy.

### Judged by current rules, never a stale copy

The linter is Go that lives **in this repo**, and the presubmit builds it from the
submission — so a PR that changes `pkg/planlint` tests its own change. To keep that
from letting a Plan pass against an _old_ contract, `main` requires every PR to be
**up to date before merge** (branch-protection strict checks). A branch that forked
before a rule landed cannot merge until it pulls `main`, which rebuilds the linter
from current source and re-lints against the current rules. So a merged Plan is
always evaluated against the rules **as they stand at merge** — the "accepted ⇒ the
controller will admit it and the agents can run it" claim holds even as rules grow.

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
pkg/planlint/   the rules engine (R1–R22) + rule metadata + unit tests
schemas/         CRD-derived JSON Schema (kubeconform) + vendored CRDs under crd/
docs/            rules.json + rules.md (generated) · flywheel.md
scripts/         plan-ai-review.sh + plan-lint-comment.sh + plan-cluster-verify.sh (live-CRD dry-run)
.lighthouse/     Tekton presubmits: plan-lint (hard) + plan-cluster-verify (live-CRD dry-run) + plan-ai-review (advisory)
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
- **`plan-lint`** (`pullrequest.yaml`) — the deterministic Go gate (R1–R22) + kubeconform. **Required.**
- **`plan-cluster-verify`** — server-side dry-run of every Plan/PlanTemplate against the
  **live CRD** (structural schema + admission; persists nothing). The authoritative,
  drift-free check that a submission will actually apply — complements the offline Go
  rules. **Required.**
- **`plan-ai-review`** — advisory (`optional: true`).

## Running the gate locally

```sh
make test     # rule unit tests
make lint     # rules engine + CRD-schema conformance (kubeconform)
make verify   # fail if schemas/ is stale vs the vendored CRDs
make schemas  # regenerate schemas/ after refreshing the CRDs
```

Same code runs in CI and on a laptop — no drift.
