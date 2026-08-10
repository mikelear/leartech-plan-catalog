# Plan Catalog rules (R1–R24)

_Generated from `pkg/planlint` — do not edit by hand; run `make rules`._

Every rule the deterministic `plan-lint` gate enforces. Errors block merge; a failing PR comment links each rule here by its anchor (e.g. `#r11`).

| Rule | Title | What it checks |
|---|---|---|
| [R1](#r1) | Valid document | A Plan must parse as YAML and have a spec mapping, or the controller can't admit it. |
| [R2](#r2) | apiVersion | Only agent.leartech.io/v1alpha1 is served by the platform CRD. |
| [R3](#r3) | kind | The catalog accepts only Plan and PlanTemplate. |
| [R4](#r4) | Name shape | metadata.name becomes a DNS-1123 label and part of the child AgentRun name; >58 chars risks the 63-byte pod-label cap. |
| [R5](#r5) | Hold-by-default | A catalog Plan is a PROPOSAL — it must not execute on merge. Execution is a separate, human-gated promote/unpause. |
| [R6](#r6) | Step shape | Each step is either a template include (use:) or a concrete step; a concrete non-fan-in step needs a kind and an agentType. |
| [R7](#r7) | Unique step names | Step names must be unique — they compute the child AgentRun name and dependsOn edges. |
| [R8](#r8) | dependsOn resolves | A dependsOn that names no declared step leaves the step permanently blocked. |
| [R9](#r9) | Template shape | A PlanTemplate declares params and steps; its steps are concrete (kind+agentType) and must not nest use: (expansion is depth-1). |
| [R10](#r10) | No laptop paths | A Plan runs in a K8s Job with only the target repo checked out — laptop/absolute paths never resolve. |
| [R11](#r11) | No cycle | A dependsOn cycle makes the DAG un-runnable; the controller marks the Plan Failed. |
| [R12](#r12) | No self-dependency | A step depending on itself (via dependsOn or triggeredWhen) can never start. |
| [R13](#r13) | Fan-in shape | A fanIn step is a no-agent gate: it must fan in from >=1 dependsOn and assert >=1 fanInValidate path; a non-fan-in step must not set fanInValidate. |
| [R14](#r14) | use-step fields | A use: step must not smuggle runtime fields — the template supplies them; expansion hard-fails otherwise. |
| [R15](#r15) | Template resolvable | A use: references a PlanTemplate; if it isn't in this PR it must already exist on the target cluster. |
| [R16](#r16) | Required params | A template param marked required must be supplied, or expansion fails. |
| [R17](#r17) | triggeredWhen | triggeredWhen must name a declared step and a valid phase, or the trigger never fires. |
| [R18](#r18) | Expanded name length | An over-long expanded child name (<plan>-<useStep>-<tmplStep>) is hash-truncated by the controller — it still runs but the name is unreadable. |
| [R19](#r19) | Test coherence | test.directed vocabulary is kind-specific; a mismatch is a silent no-op, and a directive without spec.test is ignored. |
| [R20](#r20) | No unknown fields | Fields not in the Plan/PlanTemplate CRD are strict-decoded out by the apiserver at apply, so a Plan carrying them cannot be instantiated. Common offenders: a step-level `goal` (belongs under `inputs`) and a spec-level `description` (not a CRD field). |
| [R21](#r21) | Inputs by agent type | A step's inputs are consumed by the agent runtime, so they must match the agentType's contract or the agent Job fails at run time. Dev agents (go/ng/rust/py) consume an Initiative (name + goal + repo + branch, or a repos: list); the infra agent is action-driven (action + per-action fields), not goal-driven. |
| [R22](#r22) | Infra steps live in templates | Infra steps (agentType leartech-agent-infra) are privileged, cross-cutting operations — release-verify, deploy-health, chart-config. In the PUBLIC catalog a plain Plan must not hand-author them: they belong in curated, OWNERS-gated PlanTemplates and are composed via use:. Hand-authored infra in a submitted Plan is a governance + security hole (a submitter declaring privileged infra work directly) and duplicates the standard verification the deployment path injects. Infra steps are allowed ONLY in kind: PlanTemplate. |
| [R23](#r23) | verify-release-flow is auto-injected | The platform auto-composes verify-release-flow after every deployable PR step (kind:pr carrying a repo), wired to the REAL merged PR (pr=${steps.<step>.pr}). A hand-authored `use: verify-release-flow` that dependsOn such a step therefore duplicates that injection — two verify-release-flow expansions — and typically also carries a broken hand-set sha. (A standalone verify-release-flow that does NOT dependsOn a PR step — verifying an already-deployed service by explicit sha — is legitimate and allowed.) |
| [R24](#r24) | Resolvable commit sha | A supplied `with.sha` must be a real commit sha (7–40 hex) so the release/verify checks can resolve the commit; a ref like HEAD or a branch name never resolves at check time and the check fails. With auto-injection you normally omit sha entirely — the injected verify uses the merged PR. |

## R1 — Valid document

**Why:** A Plan must parse as YAML and have a spec mapping, or the controller can't admit it.

**Fix:** Fix the YAML syntax; ensure top-level apiVersion/kind/metadata/spec are present and spec is a mapping.

## R2 — apiVersion

**Why:** Only agent.leartech.io/v1alpha1 is served by the platform CRD.

**Fix:** Set apiVersion: agent.leartech.io/v1alpha1.

## R3 — kind

**Why:** The catalog accepts only Plan and PlanTemplate.

**Fix:** Set kind to Plan or PlanTemplate.

## R4 — Name shape

**Why:** metadata.name becomes a DNS-1123 label and part of the child AgentRun name; >58 chars risks the 63-byte pod-label cap.

**Fix:** Use a lowercase DNS-1123 name (letters, digits, hyphens) of at most 58 characters.

## R5 — Hold-by-default

**Why:** A catalog Plan is a PROPOSAL — it must not execute on merge. Execution is a separate, human-gated promote/unpause.

**Fix:** Set spec.paused: true on every Plan.

## R6 — Step shape

**Why:** Each step is either a template include (use:) or a concrete step; a concrete non-fan-in step needs a kind and an agentType.

**Fix:** Give the step a name; for a concrete step set kind to pr|apply|check and set agentType; or make it a use:/fanIn step.

## R7 — Unique step names

**Why:** Step names must be unique — they compute the child AgentRun name and dependsOn edges.

**Fix:** Rename duplicate steps so every step name is unique.

## R8 — dependsOn resolves

**Why:** A dependsOn that names no declared step leaves the step permanently blocked.

**Fix:** Point dependsOn at existing step names, or remove the dangling entry.

## R9 — Template shape

**Why:** A PlanTemplate declares params and steps; its steps are concrete (kind+agentType) and must not nest use: (expansion is depth-1).

**Fix:** Give the template params + steps; make each step concrete (kind+agentType) with no nested use:.

## R10 — No laptop paths

**Why:** A Plan runs in a K8s Job with only the target repo checked out — laptop/absolute paths never resolve.

**Fix:** Reference only repo-committed or defined-store artifacts; remove /Users/, /home/, or ~/ paths.

## R11 — No cycle

**Why:** A dependsOn cycle makes the DAG un-runnable; the controller marks the Plan Failed.

**Fix:** Break the cycle so the dependsOn graph is acyclic.

## R12 — No self-dependency

**Why:** A step depending on itself (via dependsOn or triggeredWhen) can never start.

**Fix:** Remove the self-reference.

## R13 — Fan-in shape

**Why:** A fanIn step is a no-agent gate: it must fan in from >=1 dependsOn and assert >=1 fanInValidate path; a non-fan-in step must not set fanInValidate.

**Fix:** For fanIn: true add dependsOn + fanInValidate; otherwise remove fanInValidate.

## R14 — use-step fields

**Why:** A use: step must not smuggle runtime fields — the template supplies them; expansion hard-fails otherwise.

**Fix:** On a use: step keep only name, use, with, dependsOn (and triggeredWhen); remove agentType/inputs/repo/budgetIter/hold/fanIn/fanInValidate.

## R15 — Template resolvable

**Why:** A use: references a PlanTemplate; if it isn't in this PR it must already exist on the target cluster.

**Fix:** Co-submit the referenced template, or confirm it is deployed on the cluster.

## R16 — Required params

**Why:** A template param marked required must be supplied, or expansion fails.

**Fix:** Add the missing param(s) to the use-step's with: block.

## R17 — triggeredWhen

**Why:** triggeredWhen must name a declared step and a valid phase, or the trigger never fires.

**Fix:** Point triggeredWhen.step at an existing step and set phase to Running|AwaitingReview|AwaitingApproval|Succeeded.

## R18 — Expanded name length

**Why:** An over-long expanded child name (<plan>-<useStep>-<tmplStep>) is hash-truncated by the controller — it still runs but the name is unreadable.

**Fix:** Shorten the plan, use-step, or template step names to keep the expanded name under 57 chars.

## R19 — Test coherence

**Why:** test.directed vocabulary is kind-specific; a mismatch is a silent no-op, and a directive without spec.test is ignored.

**Fix:** Match test.directed to the step kind (pr→merged|closed_unmerged|opened, check→pass|fail, apply→succeed|error) and set spec.test: true.

## R20 — No unknown fields

**Why:** Fields not in the Plan/PlanTemplate CRD are strict-decoded out by the apiserver at apply, so a Plan carrying them cannot be instantiated. Common offenders: a step-level `goal` (belongs under `inputs`) and a spec-level `description` (not a CRD field).

**Fix:** Use only CRD fields. Put a step goal under `inputs:` (inputs.goal); drop spec-level `description` (use a YAML comment for human context).

## R21 — Inputs by agent type

**Why:** A step's inputs are consumed by the agent runtime, so they must match the agentType's contract or the agent Job fails at run time. Dev agents (go/ng/rust/py) consume an Initiative (name + goal + repo + branch, or a repos: list); the infra agent is action-driven (action + per-action fields), not goal-driven.

**Fix:** Dev steps: inputs {name, repo, branch, goal} (branch required for the legacy single-repo shape; or use repos:[{repo,branch}]). Infra steps: inputs {action, …} (e.g. release-health-check / chart-config). See AGENTS.md.

## R22 — Infra steps live in templates

**Why:** Infra steps (agentType leartech-agent-infra) are privileged, cross-cutting operations — release-verify, deploy-health, chart-config. In the PUBLIC catalog a plain Plan must not hand-author them: they belong in curated, OWNERS-gated PlanTemplates and are composed via use:. Hand-authored infra in a submitted Plan is a governance + security hole (a submitter declaring privileged infra work directly) and duplicates the standard verification the deployment path injects. Infra steps are allowed ONLY in kind: PlanTemplate.

**Fix:** Remove the infra step from the Plan and compose the right PlanTemplate via a use: step instead (e.g. `use: verify-release-flow`). If no template covers your need, propose a new PlanTemplate (which OWNERS review) rather than hand-authoring the infra step.

## R23 — verify-release-flow is auto-injected

**Why:** The platform auto-composes verify-release-flow after every deployable PR step (kind:pr carrying a repo), wired to the REAL merged PR (pr=${steps.<step>.pr}). A hand-authored `use: verify-release-flow` that dependsOn such a step therefore duplicates that injection — two verify-release-flow expansions — and typically also carries a broken hand-set sha. (A standalone verify-release-flow that does NOT dependsOn a PR step — verifying an already-deployed service by explicit sha — is legitimate and allowed.)

**Fix:** Remove the `use: verify-release-flow` step that dependsOn your PR step — the platform injects it on submission. Keep a hand-authored verify-release-flow only to verify a service NOT produced by a PR step in this Plan (and then supply an explicit commit sha).

## R24 — Resolvable commit sha

**Why:** A supplied `with.sha` must be a real commit sha (7–40 hex) so the release/verify checks can resolve the commit; a ref like HEAD or a branch name never resolves at check time and the check fails. With auto-injection you normally omit sha entirely — the injected verify uses the merged PR.

**Fix:** Set with.sha to a full commit sha (40 hex), or omit it and rely on the injected PR token. Do not use HEAD or a branch name.

