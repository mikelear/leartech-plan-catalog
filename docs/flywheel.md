# The Plan-quality flywheel

> The proprietary moat: a dataset of **Plan design → real run outcome** that no
> one else has, feeding models we own, running on our own gateway.

The advisory `plan-ai-review` scores each submission's design quality. On its own
that's just an opinion. What makes it compounding is closing the loop with a label
almost no LLM-eval pipeline has for free: **what actually happened when the Plan
ran?** That is distant supervision — ground truth at zero labelling cost — and it
turns every review into a training/eval example.

This is the same shape as our code-review flywheel (`leartech-dockerfiles/
ai-review-worker`: `feedback.py` capture → `leartech-llm-training-data` →
`export_dataset.py` → LoRA train → A/B eval-gate). We reuse that machinery; the
one field it doesn't have — and the one we add — is `run_outcome`.

## `run_outcome` is the recorder's record, NOT `Plan.status.phase`

Read this before implementing anything below. An earlier revision of this doc
defined the label as "did the Plan run green", sourced from the controller's
terminal phase. **That label would have trained the reviewer backwards**, and the
first pair we closed by hand proves it.

Catalog PR #77 reviewed `plans/shipproven-scene-stage-and-content.yaml`. Both
models returned CONCERNS. The corpus holds ten records for that Plan and every one
reads `phase: Succeeded`. Under phase agreement the reviewer scores WRONG. Then
read one of those successes:

    "message": "stop-on-merge: PR 23 merged while agent still running;
                controller terminated the overrun agent so the step could complete"

The agent overran, the PR merged underneath it, and the controller killed it.
Phase: Succeeded. CONCERNS was RIGHT. Score that as a false alarm often enough and
the reviewer learns to stop flagging exactly the failure classes the phase field
cannot express — costly success, brute-forced discovery, a `check` step that
cannot fail, a respawn that rubber-stamps an unmerged PR. Those are the classes
worth catching, so a metric that penalises catching them inverts the flywheel
instead of refining it.

`Plan.status.phase` is a liveness signal. It is not a quality signal, and it is
demonstrably true of runs that wasted an hour.

**The label is the per-run record in `leartech-agent-run-reports`**, which already
carries the richer truth: `costly_success` (semantic failures, probe sweeps and
whether each converged), `attempts_availability`, self-verdict availability,
`target_pr` presence, wall clock, recurring tool errors, and the controller
message quoted above. The recorder computes all of it every hour and commits it.

This also removes a component. Step 2 below no longer needs "a small job or
Maestro events" to stamp the outcome — **the recorder is the close-the-loop
mechanism**, and the join key is `plan_name` plus `plan_step`, both of which it
already records.

## Score at step level, and only on authored steps

Of those ten records, eight are auto-injected `*-verify-release-*` steps running
on `leartech-agent-infra-go`; two are the authored `leartech-agent-ng` steps. R23
composes that verification after every deployable PR step, so a Plan-level label
mostly measures the platform's own injected verification rather than the
submission's design.

The reviewer never saw the injected steps. Scoring it on them is scoring it on
work it could not review. So:

- join at `(plan_name, plan_step)`, not `plan_name`
- score only steps present in the submitted YAML
- keep the injected steps in the record as context, flagged as not-reviewable

## Label vintage is part of the label

`costly_success` is `null` on all ten of those records — the detector shipped
after they were written. Detector coverage improves most weeks, so a naive
export mixes vintages and trains against a moving target: the same run recorded
in August and in September carries different labels.

Every record therefore carries `label_schema_version`, and an export states which
versions it drew from. A held-out set spanning two versions is not a held-out set.

## The record

One record per submitted Plan, accreted over its lifetime. `lint_result` is the
`plan-lint -json` verdict (already emitted); `ai_review` is the advisory verdict;
`run_outcome` is filled in later by the recorder.

```jsonc
{
  "schema_version": "v2",
  "label_schema_version": 3,            // which detectors existed when labelled
  "plan_name": "…",
  "plan_type": "Plan | PlanTemplate",
  "plan_yaml": "…",                     // the submitted spec
  "plan_file_sha": "…",                 // the merged blob, not just the name
  "submitted_by": "human | ai | <id>",
  "pr": "mikelear/leartech-plan-catalog#123",

  "lint_result": {                      // from `plan-lint -json`
    "pass": true,
    "errors": [ { "rule": "R11", "where": "…", "message": "…" } ],
    "warnings": [ … ]
  },

  "ai_review": {                        // advisory, via the owned gateway
    "verdict": "PASS | CONCERNS",
    "models": ["claude", "deepseek"],   // gateway logical names
    "criteria_version": 2,
    "issues": [ "…" ],
    "score": 0.0,

    // QUANTIFIED PREDICTIONS. Prose cannot be scored, so the review states what
    // it expects and names the step it doubts. "Was it right?" needs the verdict;
    // "why did it miss this?" needs the step.
    "predictions": {
      "expected_outcome": "clean | degraded | failed",
      "suspect_steps": [ "step-name" ],
      "expected_run_cost_usd": 0.0,
      "expected_wall_clock_seconds": 0
    },

    "cost": {                           // from the gateway, not recomputed here
      "virtual_key": "leartech-plan-catalog-ai-key",
      "tokens": 11181,
      "usd": 0.0
    }
  },

  "run_outcome": {                      // THE LABEL — from the recorder's record
    "record_ref": "runs/<cluster>/<date>/<plan>-<step>.json",
    "plan_step": "…",                   // join key; authored steps only
    "step_authored": true,              // false = platform-injected, not scored
    "verdict": "clean | degraded | failed | not-yet-run",
    "degradation": [ "costly_success", "stop_on_merge_overrun" ],
    "phase": "Succeeded",               // kept for provenance, NOT the label
    "target_pr": "23",
    "wall_clock_seconds": 0,
    "run_cost_usd": 0.0,                // the agent's own spend, per its key
    "observed_at": "…"
  },

  "calibration": {                      // estimate vs actual, signed
    "outcome_agreed": false,
    "suspect_step_hit": true,           // did it name the step that degraded?
    "cost_error_usd": 0.0,
    "wall_clock_error_seconds": 0
  },

  "created_at": "…",
  "updated_at": "…"
}
```

`lint_result` and `ai_review` (what a *reviewer* produces) are the inputs;
`run_outcome` is the target; `calibration` is the derived error that makes the
reviewer improvable rather than merely advisory. A reviewer that flagged CONCERNS
and named the step that later degraded is a *correct* reviewer — and
`suspect_step_hit` measures exactly that, where verdict agreement alone cannot.

Anything the data cannot support renders as unavailable with a reason, never as
zero. A prediction with no measurable actual is not a correct prediction.

**The `predictions` block is a change to the review contract, not just to this
record.** [review-format.md](review-format.md) defines `leartech.review/v1` and
states that any reviewer — human, model or tool — emits that shape. Adding
quantified predictions means bumping it to `v2` there, with the block optional so
`v1` reviews stay harvestable and simply score without `suspect_step_hit`. Do that
in the PR that implements capture, not before: a contract nothing emits yet is the
hand-maintained-mirror trap in a new costume.

## Cost

Tokens are already captured per model per review — PR #77 spent 6776 on
`claude-opus-4-8` and 4405 on `deepseek-chat`. Cost is pricing × usage, and the
gateway owns both (its pricing tables and per-key usage in TSDB), so **query the
gateway rather than recomputing pricing here**.

Two costs, and they differ by orders of magnitude:

| | key | size | why it matters |
|---|---|---|---|
| review cost | `leartech-plan-catalog-ai-key` | ~11k tokens per plan per push, × models × **2 clusters** | the price of the gate |
| run cost | the agent's own key | a full agent session | what a bad Plan actually wastes |

The second is the one the flywheel is trying to reduce, so `expected_run_cost_usd`
against `run_cost_usd` is the calibration that pays for the exercise. Reviewing
every plan on every push already multiplies across files, models and both
clusters, so the review's own cost needs watching too.

## The pipeline (reuses ai-review-worker)

1. **Capture** — `plan-ai-review` writes the record (minus `run_outcome`) to the
   data repo, exactly as `ai-review-worker/feedback.py` captures code reviews.
2. **Close the loop** — the recorder stamps `run_outcome` and `calibration` onto
   the matching record, joining on `(plan_name, plan_step)`. No separate job.
3. **Export** — `export_dataset.py`-equivalent emits instruction/input/output
   JSONL: *input* = the Plan YAML + lint result; *output* = the review; the run
   outcome is the eval label. Exports state their `label_schema_version` range.
4. **Train + eval-gate** — LoRA-tune the reviewer, then A/B it against a held-out
   set. **Gate on `run_outcome.verdict` and `suspect_step_hit`, never on
   `phase`.** Only ship a reviewer that beats the incumbent on both.

Every model call — capture, review, training data — flows through the owned
gateway on a per-repo virtual key, so the data and the attribution are ours.

Capture and scoring are deterministic data work — no tools, no multi-turn, no
agent SDK — so they belong in Go. The export and training half stays in
`ai-review-worker` where it already works.

## Prerequisites — all three are now met

1. **The virtual key** — `leartech-plan-catalog-ai-key` exists on both clusters.
2. **CI live** — `plan-ai-review` posts on real PRs (four comments on #77).
3. **Real run outcomes** — the corpus holds hundreds of records, and the recorder
   adds more hourly.

What remains is the capture contract above, the recorder's join, and the export.
The blocker was never the machinery; it was having a label worth training on, and
that is what the sections above fix.
