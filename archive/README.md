# Archive — historical Plans, NOT patterns to copy

These 162 Plans were extracted from the `gcp` and `az` clusters on 2026-09-05 and then
deleted from both. **They are kept for provenance, not as guidance.**

## Read this before using anything in here as an example

Most of these predate the current platform. They were authored when there was no catalog
gate, a thinner controller, and different conventions — so they contain shapes that
**would fail the gate today** and behaviours that no longer occur. The concrete problem
this archive exists to solve is that they were being read as current practice: a model
(or a person) looking for "how do we write a Plan" would find these on-cluster and copy
patterns the linter now rejects.

For how to write a Plan today, read `plans/` and `templates/`, and run the gate:

    go run ./cmd/plan-lint

`archive/` is deliberately outside every tool's scope. `plan-lint` walks `{plans,
templates}`, `kubeconform` is given `plans/ templates/`, and `plan-submit` runs with
`-dir plans`. Nothing here is linted, schema-checked, or submitted — and it must stay
that way. If you add a directory here, do not widen those three globs.

## The most common way these mislead: step `kind`

**225 of the 333 steps in this archive declare no `kind` at all.** That was valid then
and is still valid at the apiserver — `steps[].required` is `['name']` and `kind` has an
enum but no default — and the controller's `NormalizedStepKind()` silently resolves an
absent kind to `pr`. The catalog's R6 rule now rejects it:

    FAIL [R6] step[0]=no-kind-here: kind must be one of [apply check pr] (got "")

So a Plan copied from here will fail the gate on a rule that did not exist when it was
written. That is the archive working as intended.

## Why the resolved kind is recorded, not just the raw YAML

`index.json` carries, per step, both what was declared and what the platform actually
used:

```json
{ "name": "wire-az", "kind_declared": null, "kind_effective": "pr", "kind_was_defaulted": true }
```

This is the point of the archive rather than a plain `kubectl delete`. "Did this run fail
to open a PR?" is only answerable if you know the step was meant to open one, and that
answer lived **only** on the Plan CR. Deleting the CRs without capturing it would have
made the question permanently unanswerable for the 200-odd corpus records whose Plan is
gone.

Worked example, from the analysis that prompted this cleanup. Nine records were
`kind_effective: pr`, `Succeeded`, and carried no `target_pr`. Eight were
`leartech-agent-infra` steps doing infra work that should never open a PR — they were
only "PR steps" because the kind was defaulted. One was real:
`fix-observer-envtest-quarantine / quarantine-envtest`, a `leartech-agent-go` dev agent
that succeeded without producing a PR. Without `kind_was_defaulted` you cannot separate
those two cases, and a rule built on the raw data would have been 8/9 false positives.

## What was and was not lost

| | |
|---|---|
| Plan specs | preserved here, resubmittable via `plan-submit` if ever needed |
| per-run records | unaffected — they live in `leartech-agent-run-reports` |
| Loki logs | unaffected — 720h retention, queryable by `run_id` |
| AgentRun CRs | **deleted with their Plans** (405 of 508 carried `blockOwnerDeletion: true`) |

The AgentRun deletion was checked before it happened: 149 of the owned runs had never
been recorded, and **all 149 were already past the recorder's 720h retention window**, so
no run that the recorder could still have captured was lost. No Plan in a non-terminal
phase was touched.

## Contents

- `plans/<name>.yaml` — the Plan spec, with volatile metadata (`resourceVersion`, `uid`,
  `managedFields`, `ownerReferences`, …) stripped and a header naming the clusters it
  existed on, its phase, and how many AgentRuns it owned.
- `index.json` — one entry per Plan: clusters, creation timestamps, phases, owned-run
  counts, and the per-step kind resolution described above.
