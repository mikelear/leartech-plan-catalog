#!/usr/bin/env bash
# plan-lint-comment — post the DETERMINISTIC gate's findings as a sticky PR comment.
#
# The hard gate (go test + plan-lint + schema drift + kubeconform) runs first and
# writes its verdict to two files in the workspace:
#   .plan-lint.json    the `plan-lint -json` report (files_checked, pass, errors[], warnings[])
#   .plan-lint.status  overall exit code of the whole gate (0 = all checks passed)
# This script turns that into an actionable inline comment so a contributor sees
# WHICH rule failed without digging into the Tekton log, then exits with the gate's
# status so the check still goes red on failure.
#
# Env: GIT_TOKEN (tekton-git password), REPO_OWNER, REPO_NAME, PULL_NUMBER (Lighthouse).
set -uo pipefail

JSON_FILE="${1:-.plan-lint.json}"
STATUS_FILE="${2:-.plan-lint.status}"
status=$(cat "$STATUS_FILE" 2>/dev/null || echo 1)
MARKER="<!-- plan-lint -->"

# Build the markdown body from the structured report.
BODY=$(python3 - "$JSON_FILE" "$status" <<'PY'
import json, sys
path, status = sys.argv[1], sys.argv[2]
try:
    r = json.load(open(path))
except Exception:
    r = None
out = [f"{'<!-- plan-lint -->'}"]
if r is None:
    out.append("## :triangular_ruler: plan-lint (deterministic gate)")
    out.append("")
    out.append("The gate failed before producing a report — see the `plan-lint` pipeline log "
               "(likely `go test`, schema drift, or kubeconform).")
    print("\n".join(out)); sys.exit(0)
errs, warns, n = r.get("errors") or [], r.get("warnings") or [], r.get("files_checked", 0)
passed = r.get("pass", False) and status == "0"
head = ":white_check_mark: plan-lint passed" if passed else ":x: plan-lint failed"
out.append(f"## :triangular_ruler: {head}")
out.append("")
out.append(f"Checked **{n}** Plan/PlanTemplate file(s) against rules R1–R19 + CRD-schema conformance.")
out.append("")
if errs:
    out.append(f"**{len(errs)} error(s) — must fix before merge:**")
    out.append("")
    for e in errs:
        out.append(f"- `[{e.get('rule','?')}]` `{e.get('where','')}` — {e.get('message','')}")
    out.append("")
if warns:
    out.append(f"<details><summary>{len(warns)} warning(s)</summary>")
    out.append("")
    for w in warns:
        out.append(f"- `[{w.get('rule','?')}]` `{w.get('where','')}` — {w.get('message','')}")
    out.append("")
    out.append("</details>")
    out.append("")
if not errs and status != "0":
    out.append("`plan-lint` rules passed, but another gate step failed "
               "(`go test`, schema drift, or kubeconform) — see the pipeline log.")
    out.append("")
out.append("---")
out.append("_Deterministic hard gate — this blocks merge. See also the advisory `plan-ai-review` comment._")
print("\n".join(out))
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

# Preserve the gate's verdict so the check still goes red on failure.
exit "$status"
