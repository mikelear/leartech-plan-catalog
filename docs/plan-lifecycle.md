# Plan lifecycle — from catalog to running

How a **concrete Plan** (not a PlanTemplate) goes from a catalog PR to actually
running. Three gates, with `paused` enforced at every layer. Merging a Plan
**publishes** it; it does **not** run, deploy, or instantiate anything.

> PlanTemplates are different — see the auto-release path in the
> [README](../README.md#after-merge-templates-auto-release-via-gitops). A merged
> concrete Plan triggers **no** controller PR (the postsubmit syncs `templates/`
> only).

## Gate 1 — Submit (catalog PR)

`plan-lint` (hard) + `plan-ai-review` (advisory) + **strict human merge**.
- **R5** blocks the merge unless `spec.paused: true`.
- Result: a **published, quality-approved, paused-by-declaration proposal**. Nothing
  is created in any cluster.

## Gate 2 — Instantiate (create the Plan CRD) — EXPLICIT

A merged Plan is a *library entry*. Turning it into a running-capable Plan CRD is a
**deliberate, explicit act** — never automatic on merge:

- Via **MCP `create_plan`** (an AI/human session) or the **Portal**, always through
  **`plan-api`** (the single writer).
- **`plan-api` force-creates it `paused`** — `spec.paused` is set to `true`
  *regardless of what the caller sent* (`validator.go` / `dto/create.go`). A
  catalog Plan can never be instantiated running, even by a direct API call.
- The **target** (cluster / namespace / tenant) and any **params** are chosen at
  instantiation, because a catalog Plan is generic.
- Requires the `leartechapi:mcp:write` (or `internal-services` / PlatformAdmin) scope.

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
