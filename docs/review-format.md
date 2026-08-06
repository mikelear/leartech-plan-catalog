# Plan review format — `leartech.review/v1`

Every review posted to a Plan Catalog PR — whether by a **human**, a **model**, or a
**tool** (`plan-lint`, `plan-ai-review`) — follows this one format so it is:

- **readable by humans** (colored, structured),
- **parseable by the AI that submitted the Plan** (a machine block it can act on), and
- **harvestable for training** — every review is one row of `{input: plan, output:
  review, verdict, outcome}` for our Plan-quality flywheel (see [flywheel.md](flywheel.md)).

Any future method that posts a review — a new bot, a Portal action, an OpenAI/Claude
session — **should emit this shape**. It is the contract.

## A review comment has four parts

1. **Sticky marker** — the first line is an HTML comment identifying the reviewer, so
   the comment can be updated in place (one per reviewer) and harvested by prefix:

   ```
   <!-- leartech-review:<source>[-<cluster>] -->
   ```
   - `<source>` — a stable id: `plan-lint`, `plan-ai-review`, `model-claude`, `mikelear`, …
   - optional `-<cluster>` (`gcp`/`az`) when the review is per-cluster.

2. **Human section** — a heading + a **GitHub colored callout** for the verdict, then the
   findings (table or bullets), each tagged with a severity:
   - `> [!TIP]` — APPROVE / PASS (green)
   - `> [!WARNING]` — CONCERNS (amber)
   - `> [!CAUTION]` — REQUEST_CHANGES / FAIL (red)
   - `> [!NOTE]` — advisory / informational

3. **Machine block** — a fenced ` ```json ` block conforming to the schema below. AI
   submitters parse **this**, not the prose.

4. **References** — links to [`rules.md`](rules.md), the JSON schema, and
   [`AGENTS.md`](../AGENTS.md).

## The machine schema

```json
{
  "schema": "leartech.review/v1",
  "reviewer": "<source>",                  // matches the marker source
  "reviewer_kind": "human | model | tool",
  "target": "<owner/repo#pr>",
  "verdict": "APPROVE | CONCERNS | REQUEST_CHANGES | PASS | FAIL",
  "summary": "<one line>",
  "models": [                              // optional — for model/consensus reviews
    { "logical": "claude", "supplier": "Anthropic", "resolved": "claude-opus-4-8", "tokens": 4960 }
  ],
  "findings": [
    {
      "severity": "must-fix | should-fix | nit | praise",
      "area": "<free text: dag | security | deploy | integration | coverage | …>",
      "title": "<short>",
      "detail": "<why it matters>",
      "fix": "<concrete, actionable>",
      "refs": ["step:<name>", "docs/rules.md#r8", "file:<path>"]
    }
  ]
}
```

**Verdict vocabulary** — `PASS`/`FAIL` for deterministic tools (`plan-lint`);
`APPROVE`/`CONCERNS`/`REQUEST_CHANGES` for judgment reviews (humans, models). A
`plan-lint` block is a valid `leartech.review/v1` with `reviewer_kind: tool`, `verdict:
PASS|FAIL`, and one finding per rule violation (rule id in `refs`).

**Severity** — `must-fix` blocks a healthy run; `should-fix` is strongly advised;
`nit` is cosmetic; `praise` records what's *right* (useful positive signal for training).

## Notifying a submitter (the standard nudge)

A review lives on the PR; the submitting session self-serves from there. Whoever
relays (a human today, an auto-notify webhook tomorrow) sends the **same minimal
message** — everything the session needs is on the PR and in the repo docs:

> **Your PR has been reviewed.** Everything you need is on the PR and in the repo — no
> extra context from me:
> - Read the review comment(s) on your PR (marker `leartech-review:*`). Each carries a
>   `leartech.review/v1` JSON block — parse `verdict` + `findings[]` (`severity`, `fix`, `refs`).
> - Address every `must-fix` (and ideally `should-fix`), then push — `plan-lint` and
>   `plan-ai-review` re-run automatically.
> - Reference: [`AGENTS.md`](../AGENTS.md) (author + repair), this file
>   (`docs/review-format.md`, the review shape), [`docs/rules.md`](rules.md) (the rules).
>
> Fix, push, repeat until green + a maintainer approves.

The relay is deliberately thin: no findings restated, no back-and-forth. The PR is the
channel; the docs are the contract. When OpenAI/Claude sessions webhook to their own
PRs, they receive this same pointer automatically and act on the machine block.

## Why the machine block matters

The block is the training signal. `input` = the Plan YAML (+ `plan-lint`'s structured
result); `output` = the review's `verdict` + `findings`; the eventual **run outcome**
(did the merged/unpaused Plan run green) is the label. Uniform `leartech.review/v1`
across humans, models, and tools means every review — no matter who wrote it — folds
into the same dataset. See [flywheel.md](flywheel.md).
