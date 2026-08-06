# The Plan-quality flywheel

> The proprietary moat: a dataset of **Plan design → real run outcome** that no
> one else has, feeding models we own, running on our own gateway.

The advisory `plan-ai-review` scores each submission's design quality. On its own
that's just an opinion. What makes it compounding is closing the loop with the one
label almost no LLM-eval pipeline has for free: **did the Plan actually run
green when it was promoted?** That deterministic outcome is distant supervision —
ground truth we get at zero labelling cost — and it turns every review into a
training/eval example.

This is the same shape as our code-review flywheel (`leartech-dockerfiles/
ai-review-worker`: `feedback.py` capture → `leartech-llm-training-data` →
`export_dataset.py` → LoRA train → A/B eval-gate). We reuse that machinery; the
one field it doesn't have — and the one we add — is `run_outcome`.

## The record

One record per submitted Plan, accreted over its lifetime. `lint_result` is the
`plan-lint -json` verdict (already emitted); `ai_review` is the advisory verdict;
`run_outcome` is filled in later, when the merged Plan is promoted/unpaused and
the controller reaches a terminal phase.

```jsonc
{
  "schema_version": "v1",
  "plan_name": "…",
  "plan_type": "Plan | PlanTemplate",
  "plan_yaml": "…",                    // the submitted spec
  "submitted_by": "human | ai | <id>",
  "pr": "mikelear/leartech-plan-catalog#123",

  "lint_result": {                      // from `plan-lint -json`
    "pass": true,
    "errors": [ { "rule": "R11", "where": "…", "message": "…" } ],
    "warnings": [ … ]
  },

  "ai_review": {                        // advisory, via the owned gateway
    "verdict": "PASS | CONCERNS",
    "models": ["claude-sonnet-4-6", "…"],
    "issues": [ "…" ],
    "score": 0.0
  },

  "run_outcome": {                      // the GROUND TRUTH — filled in later
    "status": "Succeeded | Failed | Resolved | Cancelled | not-yet-run",
    "plan_verdict": "…",                // controller Plan.status.phase
    "failed_step": "…",                 // first step that Failed, if any
    "observed_at": "…"
  },

  "created_at": "…",
  "updated_at": "…"
}
```

`lint_result` and `ai_review` (the labels a *reviewer* would produce) are the
inputs; `run_outcome` is the target. A reviewer that flagged CONCERNS on a Plan
that later Failed at the exact step it warned about is a *correct* reviewer — and
now we can measure that.

## The pipeline (reuses ai-review-worker)

1. **Capture** — `plan-ai-review` writes the record (minus `run_outcome`) to the
   data repo, exactly as `ai-review-worker/feedback.py` captures code reviews.
2. **Close the loop** — when a merged Plan is promoted/unpaused and reconciles to
   a terminal phase, a small job (or the controller's Maestro `plan.*` event
   stream) stamps `run_outcome` onto the matching record.
3. **Export** — `export_dataset.py`-equivalent emits instruction/input/output
   JSONL: *input* = the Plan YAML + lint result; *output* = the review; the run
   outcome is the eval label.
4. **Train + eval-gate** — LoRA-tune the reviewer, then A/B it against a held-out
   set, gating on "does the reviewer's verdict agree with the eventual run
   outcome?" Only ship a reviewer that beats the incumbent on that metric.

Every model call — capture, review, training data — flows through the owned
gateway on a per-repo virtual key, so the data and the attribution are ours.

## Prerequisites (why this is sequenced last)

The flywheel needs live inputs before it can turn:

1. **The virtual key** (`leartech-plan-catalog-ai-key`) so `plan-ai-review`
   actually produces reviews to capture.
2. **CI live** (repo registered in source-config) so reviews run on real PRs.
3. **Real run outcomes** — Plans that have been promoted/unpaused and executed,
   so `run_outcome` isn't empty.

The self-contained foundation already exists: `plan-lint -json` emits the
structured `lint_result`, and this schema fixes the capture contract. The capture
+ close-the-loop + training wiring lands once the three prerequisites are met —
reusing `ai-review-worker`, not rebuilt from scratch.
