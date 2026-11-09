# Opening a pull request here

Read this BEFORE you write a PR into any `leartech-*` repo from the shell. A PR
here does not meet a light CI — it meets an intense, deterministic set of Tekton
gates that run on BOTH clusters (gcp and az), and the single thing that fails
hand-written PRs most often is **prose comments that do not name a proof**. Most
wasted PR cycles are one of the rules below, caught on the second or third push
instead of before the first.

## The gate that catches you first: comment-gate

A deterministic checker (`leartech-pipeline-catalog/go/commentgate`) runs on
every PR and fails it on two rules:

1. **A comment that asserts behaviour must name the test that proves it.**
   If an added comment contains a *claim word* — `must`, `never`, `always`,
   `ensures`, `guarantees`, `cannot`, `prevents`, `is safe`, `no longer`,
   `impossible` — it has to carry a `// proven-by: TestName`, and that test
   has to exist in the same repo. The gate greps for the test. No test, no
   claim: either add the test or delete the sentence.

2. **A change may not add more non-functional comment lines than test lines**
   (the ratchet). A diff that is all narration and no test fails even without
   a claim word. If you are adding explanation, you are also adding a test, or
   you are over-commenting.

   Exempt (not counted): functional directives — `go:build`, `nolint`,
   `Code generated`, shebangs, `renovate:`, SPDX, `swagger:`/`@Router` etc.,
   and `proven-by:` itself. A claim inside a `_test.go` file needs no
   proven-by — the file is the proof. A claim inside quotation marks (a log
   line, an error message) is being reported, not asserted, and is ignored.

### What this means for how you write a PR from the shell

- **Write code, not essays.** The shell's model tends to over-comment. In a
  config or chart change (`values.yaml`, RBAC, a Helm template) the fix is
  almost always: delete the explanatory comment. The diff should be the rule
  or the line, nothing narrating it. Put the "why" in the PR description, which
  no gate scores.
- **If a comment earns its place, prove it.** Keep the sentence only if you add
  the `// proven-by: TestThatProvesIt` and the test. A claim you cannot prove is
  a claim you delete.
- **Run the gate before you push.** It is reproducible locally — the repo's
  `make lint` fetches and runs the same `commentgate` binary CI runs, against
  `origin/main`. `make lint` green locally means comment-gate green in CI. A
  push to find out is a wasted cycle.

## The other deterministic gates on the same PR

All of these run per-cluster (`gcp/*` and `az/*`) and all must be green before
Keeper merges:

- **lint** — golangci-lint against the repo's merged config, PLUS the repo's
  own extra gates wired into `make lint`: `swag-check` (regenerate `docs/` with
  `make swag` if you touched swagger annotations, or this fails stale),
  `auth-conformance` (go-common auth only — no hand-rolled JWKS/JOSE),
  `job-reaping`, `file-size`, and comment-gate above.
- **test** — `go test` with a non-negative coverage delta. A new exported
  symbol usually needs a test anyway; the comment-gate ratchet quietly enforces
  that too.
- **govulncheck, security-scan, image-scan, dynamic-scan, end2end** — supply
  chain + runtime. Rarely a surprise for a focused change, but they gate.
- **ai-review** — a judgement gate, not deterministic; it reads the diff and
  the PR description.

## The shortest path to a green PR from the shell

1. Make the change as small as it can be.
2. Delete narration. Keep a comment only if you will `proven-by:` it with a real
   test.
3. Regenerate anything generated (`make swag` if swagger annotations moved).
4. Run `make lint` and `go test ./...` locally — the same gates CI runs.
5. Only then push and open the PR, with the "why" in the description.

This is the same standard `ship_proven` reports for Plans (the `plan-lint`
rules); here it is the repo-PR half. Reading it first is how you stop paying for
it in failed Tekton cycles.

## After the push: fail fast, do not wait for all-green

Do not wait for every check to pass before looking again. The gates finish at
very different times — lint answers in minutes, end2end in tens of them — and a
failure found at the END of a full green wait costs a whole second lap of every
gate on the fix commit.

- **Call `wait_for_first_failure_or_all_pass` (the `jx3_flow` MCP) after every
  push.** It returns the moment ANY check fails, so you iterate on the fastest
  signal instead of the slowest, exactly as the agents are instructed to.
- **A first failure is a FIX LOOP, not a restart.** Read the failing step's log
  (`step_logs` on the `tekton` MCP, or the `target_url` the row carries), fix
  only what it names, push, and call the wait again. The other checks keep
  running across your fix commits; you never re-pay the ones already green.
- **Only `wait_for_terminal` when you are done changing things** — the final
  "every check settled" before handing to review.
- **If the tool is not mounted**, the shell's own `local__wait` with a command
  that succeeds once the first check reports (e.g. a `gh pr checks` grep for
  `fail`) is the fallback — never a blind `sleep`, which is killed at its
  timeout without telling you.

The rule of thumb: the FIRST red check is the cheapest moment to learn anything.
Waiting for all-green before looking is how one lint failure costs an hour of
end2end time, twice.
