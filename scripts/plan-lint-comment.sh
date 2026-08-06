#!/usr/bin/env bash
# plan-lint-comment — post the DETERMINISTIC gate's findings as a DUAL-AUDIENCE
# sticky PR comment: a colored, formatted view for humans AND a machine-readable
# JSON block (+ fix hints, doc anchors, reference files) for the AI that submitted
# the Plan. See AGENTS.md for the agent-facing contract.
#
# Inputs (written by the gate step):
#   .plan-lint.json    the enriched `plan-lint -json` report (errors[] carry fix+doc)
#   .plan-lint.status  overall exit code of the whole gate (0 = all checks passed)
# Exits with the gate's status so the check still goes red on failure.
#
# Env: GIT_TOKEN (tekton-git password), REPO_OWNER, REPO_NAME, PULL_NUMBER (Lighthouse).
set -uo pipefail

JSON_FILE="${1:-.plan-lint.json}"
STATUS_FILE="${2:-.plan-lint.status}"
status=$(cat "$STATUS_FILE" 2>/dev/null || echo 1)
MARKER="<!-- plan-lint -->"
REPO_SLUG="${REPO_OWNER:-mikelear}/${REPO_NAME:-leartech-plan-catalog}"

BODY=$(python3 - "$JSON_FILE" "$status" "$REPO_SLUG" <<'PY'
import json, sys
path, status, slug = sys.argv[1], sys.argv[2], sys.argv[3]
base = f"https://github.com/{slug}/blob/main"
try:
    r = json.load(open(path))
except Exception:
    r = None
L = ["<!-- plan-lint -->"]
if r is None:
    L += ["## 📐 plan-lint (deterministic gate)", "",
          "> [!CAUTION]", "> The gate failed before producing a report — see the `plan-lint` "
          "pipeline log (likely `go test`, schema drift, or kubeconform).", ""]
    print("\n".join(L)); sys.exit(0)

errs, warns, n = r.get("errors") or [], r.get("warnings") or [], r.get("files_checked", 0)
passed = r.get("pass", False) and status == "0"
badge = ("https://img.shields.io/badge/plan--lint-passing-brightgreen"
         if passed else "https://img.shields.io/badge/plan--lint-failing-red")
L += [f"## 📐 plan-lint — deterministic gate  ![plan-lint]({badge})", ""]

# Human callout (GitHub renders these colored).
if passed:
    L += ["> [!TIP]", f"> All **{n}** Plan/PlanTemplate file(s) pass rules R1–R19 + CRD-schema conformance.", ""]
else:
    L += ["> [!CAUTION]", f"> **{len(errs)} error(s)** must be fixed before this PR can merge "
          f"(checked {n} file(s)).", ""]

# Error table (human) — rule links to the catalog anchor.
if errs:
    L += ["| Rule | Location | Problem | Fix |", "|---|---|---|---|"]
    for e in errs:
        rule, doc = e.get("rule", "?"), e.get("doc", "")
        rl = f"[{rule}]({doc})" if doc else rule
        loc = (e.get("where", "") or "").replace("|", "\\|")
        msg = (e.get("message", "") or "").replace("|", "\\|")
        fix = (e.get("fix", "") or "").replace("|", "\\|")
        L += [f"| {rl} | `{loc}` | {msg} | {fix} |"]
    L += [""]

if warns:
    L += [f"<details><summary>⚠️ {len(warns)} warning(s)</summary>", ""]
    for w in warns:
        rule, doc = w.get("rule", "?"), w.get("doc", "")
        rl = f"[{rule}]({doc})" if doc else rule
        L += [f"- {rl} `{w.get('where','')}` — {w.get('message','')}"]
    L += ["", "</details>", ""]

if not errs and status != "0":
    L += ["> [!WARNING]", "> `plan-lint` rules passed, but another gate step failed "
          "(`go test`, schema drift, or kubeconform) — see the pipeline log.", ""]

# Machine-readable block for the AI submitter (parse THIS, not the prose).
machine = {"gate": "plan-lint", "pass": passed, "files_checked": n,
           "errors": errs, "warnings": warns}
L += ["<details><summary>🤖 Machine-readable verdict (for AI submitters — see "
      f"<a href=\"{base}/AGENTS.md\">AGENTS.md</a>)</summary>", "",
      "```json", json.dumps(machine, indent=2), "```", "", "</details>", ""]

# Reference files that "speak the agent's language".
L += ["**References:** "
      f"[rule catalog]({base}/docs/rules.md) · "
      f"[rules.json]({base}/docs/rules.json) · "
      f"[JSON schema]({base}/schemas/plan_v1alpha1.json) · "
      f"[examples]({base}/plans) · "
      f"[AGENTS.md]({base}/AGENTS.md)", ""]
L += ["---", "_Deterministic hard gate — this blocks merge. "
      "See also the advisory `plan-ai-review` comment._"]
print("\n".join(L))
PY
)

echo "plan-lint-comment: post-context — token=$([ -n "${GIT_TOKEN:-}" ] && echo set || echo MISSING) pr=${PULL_NUMBER:-MISSING} owner=${REPO_OWNER:-MISSING} repo=${REPO_NAME:-MISSING} gate_status=$status"

if [ -n "${GIT_TOKEN:-}" ] && [ -n "${PULL_NUMBER:-}" ] && [ -n "${REPO_OWNER:-}" ] && [ -n "${REPO_NAME:-}" ]; then
  AUTH="Authorization: token ${GIT_TOKEN}"
  EXISTING=$(curl -fsSL -H "$AUTH" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments?per_page=100" 2>/dev/null | python3 -c "import json,sys
try:
 [print(c['id']) for c in json.load(sys.stdin) if c.get('body','').startswith('$MARKER')]
except Exception: pass" | head -1)
  PAYLOAD=$(python3 -c "import json,sys; print(json.dumps({'body':sys.stdin.read()}))" <<<"$BODY")
  if [ -n "$EXISTING" ]; then
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X PATCH -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/comments/${EXISTING}" 2>/dev/null)
    echo "plan-lint-comment: updated sticky comment -> HTTP $code"
  else
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X POST -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments" 2>/dev/null)
    echo "plan-lint-comment: posted sticky comment -> HTTP $code"
  fi
else
  echo "plan-lint-comment: skipping PR comment (missing context) — findings are in this log"
fi
exit "$status"
