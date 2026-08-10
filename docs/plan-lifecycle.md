# Plan lifecycle — from catalog to running

How a **concrete Plan** (not a PlanTemplate) goes from a catalog PR to actually
running. Three gates, with `paused` enforced at every layer. Merging a Plan
**publishes** it and **auto-submits it to `plan-api` as a paused proposal** — but
it does **not** run or deploy anything. Execution still requires a separate,
explicit unpause (Gate 3).

> PlanTemplates take a different path — see the auto-release path in the
> [README](../README.md#after-merge-templates-auto-release-via-gitops). On merge,
> the release postsubmit syncs `templates/` to the controller (a GitOps PR) **and**
> submits `plans/*.yaml` to `plan-api` (this doc). A merged concrete Plan triggers
> **no** controller PR.

## Gate 1 — Submit (catalog PR)

`plan-lint` (hard) + `plan-ai-review` (advisory) + **strict human merge**.
- **R5** blocks the merge unless `spec.paused: true`.
- Result: a **published, quality-approved, paused-by-declaration proposal**. Nothing
  is created in any cluster.

## Gate 2 — Instantiate (create the Plan CRD, always paused)

A merged Plan becomes a Plan CRD via **`plan-api`** — the single writer, which
**force-creates it `paused`** (`spec.paused` set `true` *regardless of what the
caller sent*, `validator.go` / `dto/create.go`). A catalog Plan can never be
instantiated running, even by a direct API call. Two front doors reach plan-api:

- **Automatic on merge** — the catalog release postsubmit
  ([`cmd/plan-submit`](../cmd/plan-submit)) submits every `plans/*.yaml`
  (excluding `example-*`) to plan-api on **both clusters**, using the
  `plan-catalog-internal-services` s2s client (scope `leartechapi.internal_services`,
  audience `leartech-plan-api`). This is the catalog GH route — the automation
  sibling of `create_plan`.
- **Explicit** — **MCP `create_plan`** or the **Portal** (a user/AI session), for
  targeted or parameterized instantiation where the **target** (cluster / namespace
  / tenant) and **params** are chosen per-call.

Either way the result is a **paused** Plan CRD. Write access requires the
`leartechapi.internal_services` scope (the catalog s2s client) OR
`leartechapi:mcp:write` / PlatformAdmin.

## Gate 3 — Approve & run (unpause) — ADMIN ONLY

The **only** way execution starts. A separate, higher-privilege action:
- `plan-api POST /plans/{name}/unpause` (PlatformAdmin) or a Portal "Approve & Run"
  button. _(Endpoint pending — interim is an operator `kubectl patch spec.paused=false`.)_
- Audited; distinct role from create.

## `paused` is a firm, multi-layer requirement

| Layer | Enforcement |
|---|---|
| Catalog PR | `plan-lint` R5 — merge blocked unless `spec.paused: true` |
| plan-api create | `spec.paused` **forced true** regardless of input — new plans always land paused |
| Run | unpause is a separate, admin-only, audited action |

Three independent layers must all be crossed before a catalog Plan runs — and only
the last one starts execution.

## Provenance

When a Plan is instantiated, record which catalog Plan + PR/commit it came from (the
same provenance the template auto-release stamps), so a running Plan traces back to
its reviewed source.
