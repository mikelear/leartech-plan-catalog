#!/usr/bin/env bash
# plan-ai-review — the ADVISORY judgment layer for the ShipProven Plan Catalog.
#
# Sends each submitted Plan/PlanTemplate to one or more ML models THROUGH OUR OWN
# AI GATEWAY (per-repo virtual key), and posts a DUAL-AUDIENCE sticky PR comment:
# a colored, per-supplier panel for humans AND a machine-readable verdict for the
# AI that submitted the Plan (see AGENTS.md). Transparent about which suppliers +
# models were consulted, their token usage, and what each one said. Never blocks
# (the deterministic plan-lint is the hard gate); this feeds the Plan-quality flywheel.
#
# Per-cluster: the comment is stamped with the cluster and uses a per-cluster
# marker so gcp + az each keep their own sticky comment (like leartech-gate).
#
# Env: AI_GATEWAY_URL, AI_GATEWAY_API_KEY, REVIEW_MODELS (comma logical names),
#      CLUSTER_ID, GIT_TOKEN, REPO_OWNER, REPO_NAME, PULL_NUMBER.
set -uo pipefail

GW="${AI_GATEWAY_URL:-}"
KEY="${AI_GATEWAY_API_KEY:-}"
# Gateway LOGICAL model names (it resolves each to a provider). Default to a
# two-supplier panel so the consensus/owned-gateway story is visible.
MODELS="${REVIEW_MODELS:-claude,deepseek}"
CLUSTER="${CLUSTER_ID:-unknown}"

if [ -z "$GW" ] || [ -z "$KEY" ]; then
  echo "plan-ai-review: AI_GATEWAY_URL / AI_GATEWAY_API_KEY unset — skipping advisory review (provision the virtual key to enable)."
  exit 0
fi

# Review criteria live in docs/review-criteria.md, NOT inline here. They used to be a
# ~1400-char single-line SYS='…' bash literal: every edit was a quote-escaping hazard, the
# diff was one unreadable line, and a single criterion could not be reviewed on its own.
# That is how criterion (7) drifted out of lockstep with lint rule R23 and spent releases
# telling authors to add a step the deterministic gate fails them for.
#
# This file is the model-half counterpart to docs/rules.md + rules.json. Everything below
# its `## Criteria` heading is sent as the system message, verbatim.
CRITERIA_FILE="docs/review-criteria.md"
if [ ! -f "$CRITERIA_FILE" ]; then
  echo "plan-ai-review: $CRITERIA_FILE missing — cannot review without criteria. Skipping."
  exit 0
fi
SYS=$(python3 - "$CRITERIA_FILE" <<'PY'
import re, sys
t = open(sys.argv[1]).read()
m = re.search(r'^##\s+Criteria\s*$(.*)', t, re.M | re.S)
sys.stdout.write((m.group(1) if m else t).strip())
PY
)
if [ -z "${SYS//[[:space:]]/}" ]; then
  echo "plan-ai-review: $CRITERIA_FILE has no '## Criteria' body — skipping rather than reviewing with an empty prompt."
  exit 0
fi
# Stamped into the review output and the machine-readable verdict so a flywheel record can
# be attributed to the criteria that produced it. Without it, verdicts from different
# revisions cannot be compared against run_outcome — the one question the dataset exists to
# answer.
CRITERIA_VERSION=$(sed -n 's/.*criteria-version:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$CRITERIA_FILE" | head -1)
CRITERIA_VERSION="${CRITERIA_VERSION:-unknown}"
echo "plan-ai-review: criteria v${CRITERIA_VERSION} loaded from $CRITERIA_FILE ($(printf '%s' "$SYS" | wc -c | tr -d ' ') bytes)"

shopt -s nullglob
FILES=(plans/**/*.yaml plans/*.yaml templates/**/*.yaml templates/*.yaml)
[ ${#FILES[@]} -eq 0 ] && { echo "plan-ai-review: no Plan YAML to review."; exit 0; }

# AVAILABLE-TEMPLATE catalog: name + params for every PlanTemplate in the repo.
# Fed to the reviewer so it can point submitters at templates they should COMPOSE
# via `use:` (e.g. verify-release-flow) rather than hand-author — the near-term
# "suggest" half of the suggest→auto-inject plan. Advisory only.
TEMPLATES_CATALOG=$(python3 - <<'PY'
import glob, sys
try:
    import yaml
except Exception:
    print("(template catalog unavailable — pyyaml missing)"); sys.exit(3)
seen, lines = set(), []
for f in sorted(set(glob.glob("templates/**/*.yaml", recursive=True) + glob.glob("templates/*.yaml"))):
    try:
        for d in yaml.safe_load_all(open(f)):
            if not isinstance(d, dict) or d.get("kind") != "PlanTemplate":
                continue
            name = (d.get("metadata") or {}).get("name", "?")
            if name in seen:
                continue
            seen.add(name)
            params = [p.get("name") for p in ((d.get("spec") or {}).get("params") or []) if isinstance(p, dict)]
            req = [p.get("name") for p in ((d.get("spec") or {}).get("params") or []) if isinstance(p, dict) and p.get("required")]
            lines.append(f"- {name} (params: {', '.join(params) or 'none'}; required: {', '.join(req) or 'none'})")
    except Exception:
        continue
print("\n".join(lines) if lines else "(no PlanTemplates in catalog)")
PY
)
# A DEGRADED catalog must be VISIBLE, not just implied inside the prompt. Previously the
# only trace was the placeholder string reaching the model, so the reviewer would report
# "template catalog unavailable" as if it were a finding about the Plan — and the actual
# cause (pyyaml absent from the review image) went unnoticed across every review. Surfacing
# it here puts it in the pipeline log AND a banner on the sticky comment.
CATALOG_STATUS="ok"
case "$TEMPLATES_CATALOG" in
  *"pyyaml missing"*)
    CATALOG_STATUS="degraded: pyyaml not installed in the review image"
    echo "plan-ai-review: WARNING — template catalog UNAVAILABLE (pyyaml missing). Criterion 7 cannot function; the reviewer is told to make no template recommendations." ;;
  *"no PlanTemplates in catalog"*)
    CATALOG_STATUS="empty: no PlanTemplates found in templates/"
    echo "plan-ai-review: WARNING — no PlanTemplates discovered under templates/." ;;
  *)
    echo "plan-ai-review: template catalog loaded — $(printf '%s' "$TEMPLATES_CATALOG" | grep -c '^- ') template(s)" ;;
esac

set +e  # everything below is best-effort advisory — never fail the gate
BASE="https://github.com/${REPO_OWNER:-mikelear}/${REPO_NAME:-leartech-plan-catalog}/blob/main"
# The commit this review ran against — stamped in the footer so the sticky comment's
# freshness is unambiguous (it is edited in place, so its GitHub timestamp looks old).
SHA=$(printf '%s' "${PULL_PULL_SHA:-$(git rev-parse HEAD 2>/dev/null)}" | cut -c1-8)

# Per-response parser: reads the gateway JSON on stdin, emits one result line.
PARSE=$(mktemp)
cat > "$PARSE" <<'PY'
import json, sys
file, logical = sys.argv[1], sys.argv[2]
raw = sys.stdin.read()
try:
    d = json.loads(raw)
    content = d["choices"][0]["message"]["content"]
    resolved = d.get("model", logical)
    tokens = (d.get("usage") or {}).get("total_tokens", 0)
    verdict = ""
    for ln in content.splitlines():
        if "VERDICT" in ln.upper():
            verdict = ln.strip(); break
    ok = True
except Exception as e:
    content = "(review unavailable: %s)" % e
    resolved, tokens, verdict, ok = logical, 0, "UNAVAILABLE", False
print(json.dumps({"file": file, "logical": logical, "resolved": resolved,
                  "tokens": tokens, "verdict": verdict, "content": content, "ok": ok}))
PY

RESULTS=$(mktemp); : > "$RESULTS"
for file in "${FILES[@]}"; do
  content=$(cat "$file")
  for model in ${MODELS//,/ }; do
    payload=$(python3 -c "import json,sys; print(json.dumps({'model':sys.argv[1],'messages':[{'role':'system','content':sys.argv[2]},{'role':'user','content':'AVAILABLE PlanTemplates you may recommend via use: (name — params):\n'+sys.argv[5]+'\n\nReview this Plan file '+sys.argv[3]+':\n\n'+sys.argv[4]}],'max_tokens':500,'temperature':0}))" "$model" "$SYS" "$file" "$content" "$TEMPLATES_CATALOG")
    resp=$(curl -fsS -m 60 -X POST "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d "$payload" 2>/dev/null)
    printf '%s' "$resp" | python3 "$PARSE" "$file" "$model" >> "$RESULTS"
  done
done

# Build the dual-audience comment from all results.
BUILD=$(mktemp)
cat > "$BUILD" <<'PY'
import json, sys
results_file, cluster, models_csv, base = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
sha = sys.argv[5] if len(sys.argv) > 5 else ""
criteria_version = sys.argv[6] if len(sys.argv) > 6 else "unknown"
catalog_status = sys.argv[7] if len(sys.argv) > 7 else "ok"
rows = [json.loads(l) for l in open(results_file) if l.strip()]
SUP = {"claude": "Anthropic", "deepseek": "DeepSeek", "codestral": "Mistral",
       "qwen": "Alibaba (Qwen)", "azure_openai": "Azure OpenAI", "gpt-4": "OpenAI"}
sup = lambda l: SUP.get(l, "gateway-routed")
models = [m.strip() for m in models_csv.split(",") if m.strip()]

agg = {}
for r in rows:
    a = agg.setdefault(r["logical"], {"tokens": 0, "resolved": r["resolved"], "ok": False})
    a["tokens"] += r.get("tokens", 0) or 0
    a["resolved"] = r["resolved"]; a["ok"] = a["ok"] or r["ok"]

L = [f"<!-- plan-ai-review-{cluster} -->"]
L += [f"## 🤖 plan-ai-review — advisory · cluster `{cluster}`  "
      "![advisory](https://img.shields.io/badge/plan--ai--review-advisory-blue)", ""]
L += ["> [!NOTE]",
      "> Model design review via the **owned AI gateway**. Advisory only — it never blocks; "
      "the deterministic `plan-lint` gate decides merge-eligibility.", ""]

# A degraded template catalog is a TOOLING fault, not a finding about the Plan. Banner it so
# a reader knows the review ran with one criterion disabled, rather than reading the model's
# "catalog unavailable" bullet as a defect in their submission.
if catalog_status != "ok":
    L += ["> [!WARNING]",
          f"> **Template catalog {catalog_status}.** Criterion 7 (template guidance) could "
          "not run, so this review makes no template recommendations. This is a fault in the "
          "review tooling, not in the Plan.", ""]

L += ["**Models consulted (routed through the gateway):**", "",
      "| Logical | Supplier | Resolved model | Tokens | Status |", "|---|---|---|---|---|"]
for lg in models:
    a = agg.get(lg, {"tokens": 0, "resolved": lg, "ok": False})
    st = "✅" if a["ok"] else "⚠️ unavailable"
    L += [f"| `{lg}` | {sup(lg)} | `{a['resolved']}` | {a['tokens']} | {st} |"]
L += [""]

files = []
for r in rows:
    if r["file"] not in files:
        files.append(r["file"])
for f in files:
    L += [f"### `{f}`", ""]
    for r in rows:
        if r["file"] != f:
            continue
        head = (f"<b>{sup(r['logical'])}</b> · <code>{r['logical']}</code> → "
                f"<code>{r['resolved']}</code> · {r.get('tokens',0)} tok — {r.get('verdict') or ''}")
        L += [f"<details><summary>{head}</summary>", "", r["content"], "", "</details>", ""]

machine = {"gate": "plan-ai-review", "cluster": cluster,
           "criteria_version": criteria_version, "template_catalog": catalog_status,
           "models": [{"logical": lg, "supplier": sup(lg),
                       "resolved": agg.get(lg, {}).get("resolved", lg),
                       "tokens": agg.get(lg, {}).get("tokens", 0)} for lg in models],
           "reviews": [{"file": r["file"], "logical": r["logical"], "supplier": sup(r["logical"]),
                        "verdict": r.get("verdict", ""), "tokens": r.get("tokens", 0)} for r in rows]}
L += [f'<details><summary>🤖 Machine-readable verdict (for AI submitters — see '
      f'<a href="{base}/AGENTS.md">AGENTS.md</a>)</summary>', "",
      "```json", json.dumps(machine, indent=2), "```", "", "</details>", ""]
L += [f"**References:** [rule catalog]({base}/docs/rules.md) · [AGENTS.md]({base}/AGENTS.md) · "
      f"[examples]({base}/plans)", ""]
stamp = (f"plan-ai-review · reviewed `{sha}`" + (f" · cluster `{cluster}`" if cluster else "")
         + f" · criteria v{criteria_version} · updated in place on each push")
L += ["---", "_Advisory — routed through the owned gateway; the per-model verdicts above feed our "
      "Plan-quality flywheel. The deterministic `plan-lint` comment is the hard gate._",
      "", f"<sub>{stamp}</sub>"]
print("\n".join(L))
PY

BODY=$(python3 "$BUILD" "$RESULTS" "$CLUSTER" "$MODELS" "$BASE" "$SHA" "$CRITERIA_VERSION" "$CATALOG_STATUS")
echo "$BODY"

# Sticky PR comment, PER CLUSTER (gcp + az keep separate comments).
MARKER="<!-- plan-ai-review-${CLUSTER} -->"
echo "plan-ai-review: post-context — cluster=$CLUSTER token=$([ -n "${GIT_TOKEN:-}" ] && echo set || echo MISSING) pr=${PULL_NUMBER:-MISSING} owner=${REPO_OWNER:-MISSING} repo=${REPO_NAME:-MISSING}"
if [ -n "${GIT_TOKEN:-}" ] && [ -n "${PULL_NUMBER:-}" ] && [ -n "${REPO_OWNER:-}" ] && [ -n "${REPO_NAME:-}" ]; then
  AUTH="Authorization: token ${GIT_TOKEN}"
  EXISTING=$(curl -fsSL -H "$AUTH" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments?per_page=100" 2>/dev/null | python3 -c "import json,sys
try:
 [print(c['id']) for c in json.load(sys.stdin) if c.get('body','').startswith('$MARKER')]
except Exception: pass" | head -1)
  PAYLOAD=$(python3 -c "import json,sys; print(json.dumps({'body':sys.stdin.read()}))" <<<"$BODY")
  if [ -n "$EXISTING" ]; then
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X PATCH -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/comments/${EXISTING}" 2>/dev/null)
    echo "plan-ai-review: updated sticky comment [$CLUSTER] -> HTTP $code"
  else
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X POST -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments" 2>/dev/null)
    echo "plan-ai-review: posted sticky comment [$CLUSTER] -> HTTP $code"
  fi
else
  echo "plan-ai-review: skipping PR comment (missing context) — review is in this log"
fi
