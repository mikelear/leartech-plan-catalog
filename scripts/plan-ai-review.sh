#!/usr/bin/env bash
# plan-ai-review — the JUDGMENT layer for the ShipProven Plan Catalog.
#
# Sends each submitted Plan/PlanTemplate to an ML model (or a quorum of models)
# THROUGH OUR OWN AI GATEWAY, using a per-repo VIRTUAL KEY, and posts the review as
# a sticky PR comment. This is ADVISORY (never fails the gate — the deterministic
# plan-lint is the hard block); it scores design quality a linter can't judge.
#
# Why this matters to the product (ShipProven): every model call routes through the
# gateway we own — so this step (a) demonstrates consensus governance applied to
# Plans, and (b) generates PROPRIETARY training data on Plan quality vs. real run
# outcomes, feeding our own fine-tuning flywheel. Over time the Plan-quality models
# become ours, tuned on our own system's data — measurable model growth we control.
#
# GROWTH PATH (kept deliberately simple for v0): REVIEW_MODELS is a comma-list; today
# it can be one model, tomorrow N models across providers with a meta-judge quorum —
# same shape as our code-review consensus. Adding models = editing the list.
#
# Env (Tekton-injected / secret):
#   AI_GATEWAY_URL      base URL of the owned gateway (OpenAI-compatible /v1)
#   AI_GATEWAY_API_KEY  our per-repo VIRTUAL KEY (secret leartech-plan-catalog-ai-key)
#   REVIEW_MODELS       comma-list of model ids (default: a single review model)
#   GIT_TOKEN, REPO_OWNER, REPO_NAME, PULL_NUMBER  for the sticky PR comment
#
# NON-FATAL by construction: the whole comment path runs under `set +e` so a gateway
# hiccup / SIGPIPE can never fail a PR whose plan-lint is green (lesson: a cosmetic
# step must never override the real verdict). Exits 0 always.

set -uo pipefail

GW="${AI_GATEWAY_URL:-}"
KEY="${AI_GATEWAY_API_KEY:-}"
# The gateway routes by its own LOGICAL model names (claude, deepseek, codestral,
# qwen, azure_openai — the gateway resolves each to a provider), NOT provider model
# ids like "claude-sonnet-4-6". Multi-model consensus = a comma-list, e.g. "claude,deepseek".
MODELS="${REVIEW_MODELS:-claude}"

if [ -z "$GW" ] || [ -z "$KEY" ]; then
  echo "plan-ai-review: AI_GATEWAY_URL / AI_GATEWAY_API_KEY unset — skipping advisory review (provision the virtual key to enable)."
  exit 0
fi

SYS='You are the Plan-quality reviewer for the ShipProven Plan Catalog. You review agent.leartech.io Plans/PlanTemplates (declarative DAGs of agent steps). Judge ONLY what a linter cannot: (1) sound decomposition + DAG; (2) every step has a DETERMINISTIC done-check, not "trust the agent exit code"; (3) correct step kinds (pr=PR-lifecycle, apply=idempotent, check=verdict); (4) NO known anti-patterns — opening-a-PR treated as success, ghost-prone/absent teardown, version-blind release checks, three-writers-one-branch, missing webhook/wiring post-conditions; (5) safety — hold-by-default, scoped perms, budgets; (6) proper Template reuse vs reinvention. Reply with a one-line VERDICT: PASS or CONCERNS, then <=6 terse bullets of specifics. Be concrete and short.'

shopt -s nullglob
FILES=(plans/**/*.yaml plans/*.yaml templates/**/*.yaml templates/*.yaml)
[ ${#FILES[@]} -eq 0 ] && { echo "plan-ai-review: no Plan YAML to review."; exit 0; }

set +e  # everything below is best-effort advisory — never fail the gate
REVIEW_MD="## :robot: Plan ai-review (advisory — via ShipProven AI gateway)\n"
for file in "${FILES[@]}"; do
  content=$(cat "$file")
  for model in ${MODELS//,/ }; do
    payload=$(python3 -c "import json,sys; print(json.dumps({'model':sys.argv[1],'messages':[{'role':'system','content':sys.argv[2]},{'role':'user','content':'Review this Plan file '+sys.argv[3]+':\n\n'+sys.argv[4]}],'max_tokens':400,'temperature':0}))" "$model" "$SYS" "$file" "$content")
    resp=$(curl -fsS -m 60 -X POST "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d "$payload" 2>/dev/null)
    verdict=$(printf '%s' "$resp" | python3 -c "import json,sys
try: print(json.load(sys.stdin)['choices'][0]['message']['content'])
except Exception as e: print('(review unavailable: '+str(e)+')')" 2>/dev/null)
    REVIEW_MD="${REVIEW_MD}\n### \`${file}\` — model \`${model}\`\n${verdict}\n"
  done
done
REVIEW_MD="${REVIEW_MD}\n---\n_Advisory only. The deterministic \`plan-lint\` is the hard gate; merge is a strict human decision (no auto-merge). Routed through the owned gateway — this run also feeds our Plan-quality model flywheel._"

echo -e "$REVIEW_MD"

# Sticky PR comment (best-effort; marker keeps one comment)
if [ -n "${GIT_TOKEN:-}" ] && [ -n "${PULL_NUMBER:-}" ] && [ -n "${REPO_OWNER:-}" ] && [ -n "${REPO_NAME:-}" ]; then
  MARKER="<!-- plan-ai-review -->"
  BODY=$(printf '%s\n\n%b' "$MARKER" "$REVIEW_MD")
  API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments?per_page=100"
  AUTH="Authorization: token ${GIT_TOKEN}"
  EXISTING=$(curl -fsSL -H "$AUTH" "$API" 2>/dev/null | python3 -c "import json,sys
try:
 [print(c['id']) for c in json.load(sys.stdin) if c.get('body','').startswith('$MARKER')]
except Exception: pass" | head -1)
  PAYLOAD=$(python3 -c "import json,sys; print(json.dumps({'body':sys.stdin.read()}))" <<<"$BODY")
  if [ -n "$EXISTING" ]; then
    curl -fsSL -X PATCH -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/comments/${EXISTING}" >/dev/null 2>&1
  else
    curl -fsSL -X POST -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments" >/dev/null 2>&1
  fi
fi
exit 0
