#!/usr/bin/env bash
# plan-arch-review — the ADVISORY architecture-fit lane for the ShipProven
# Plan Catalog. Sibling of plan-ai-review.sh: same gateway/key/sticky-comment
# plumbing, DIFFERENT question. plan-ai-review scores design quality against
# docs/review-criteria.md; this lane asks "does this Plan fit the ESTATE?" —
# and answers it with evidence injected from the Cartographer's dossiers
# (leartech-architecture; the standing architecture model the Cartographer
# agent refreshes as reviewed PRs).
#
# THE DOSSIER INJECTION STEP (the reason this lane exists). The Plan YAML
# alone cannot say whether it duplicates a domain another service owns,
# ignores a published SDK, or splits against the estate's grain. The dossier
# repo can. So: fetch estate.md + the dossier of every repo the Plan's steps
# target, prepend them as an EVIDENCE block — cite these, or say they do not
# apply (the WithSearch shape from the BA researcher). A reviewer without the
# evidence invents context; one with it is accountable to it.
#
# ADVISORY ONLY, ALWAYS. The deterministic floor lives in plan-lint (R1–R27)
# and grows there only for what this lane proves it catches — the same
# judgement-into-rules path R5 documents. This script never blocks a merge.
#
# Env: AI_GATEWAY_URL, AI_GATEWAY_API_KEY, ARCH_REVIEWERS (comma logical
#      names; falls back to PLAN_REVIEWERS then REVIEW_MODELS), CLUSTER_ID,
#      GIT_TOKEN, REPO_OWNER, REPO_NAME, PULL_NUMBER, plus the diff-scoping
#      vars plan-ai-review.sh reads (PULL_BASE_SHA et al).
set -uo pipefail

GW="${AI_GATEWAY_URL:-}"
KEY="${AI_GATEWAY_API_KEY:-}"
# The panel is a SEPARATE list from PLAN_REVIEWERS, deliberately: that list
# is chosen for judging a Plan's DESIGN quality; architecture-fit against a
# dossier set is a different judgement. Same ConfigMap (ai-review-cluster-
# config), different key, so an operator repoints either panel without a
# code edit — the fix plan-ai-review already made once for its own list.
MODELS="${ARCH_REVIEWERS:-${PLAN_REVIEWERS:-${REVIEW_MODELS:-claude}}}"
CLUSTER="${CLUSTER_ID:-unknown}"

# The dossier repo the Cartographer writes (LEARTECH_ARCHITECTURE_REPO names
# the same repo for the MCP host; here it is a plain owner/name).
ARCH_REPO="${LEARTECH_ARCHITECTURE_REPO:-mikelear/leartech-architecture}"

if [ -z "$GW" ] || [ -z "$KEY" ]; then
  echo "plan-arch-review: AI_GATEWAY_URL / AI_GATEWAY_API_KEY unset — skipping advisory arch review (provision the virtual key to enable)."
  exit 0
fi

# ── the criteria: architecture-fit, in docs/arch-review-criteria.md ──────
# Kept in a file beside review-criteria.md for the same reason: a quoted
# bash literal is an escaping hazard no criterion can be reviewed inside.
CRITERIA_FILE="docs/arch-review-criteria.md"
if [ ! -f "$CRITERIA_FILE" ]; then
  echo "plan-arch-review: $CRITERIA_FILE missing — cannot review without criteria. Skipping."
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
  echo "plan-arch-review: $CRITERIA_FILE has no '## Criteria' body — skipping rather than reviewing with an empty prompt."
  exit 0
fi
CRITERIA_VERSION=$(sed -n 's/.*criteria-version:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$CRITERIA_FILE" | head -1)
CRITERIA_VERSION="${CRITERIA_VERSION:-unknown}"
echo "plan-arch-review: criteria v${CRITERIA_VERSION} loaded from $CRITERIA_FILE ($(printf '%s' "$SYS" | wc -c | tr -d ' ') bytes)"

shopt -s nullglob
ALL_FILES=(plans/**/*.yaml plans/*.yaml templates/**/*.yaml templates/*.yaml)

# DIFF-SCOPED, same contract as plan-ai-review: findings must be about what
# this PR proposes, and a stale-file artifact reads as a submitter defect.
BASE="${PULL_BASE_SHA:-}"
if [ -z "$BASE" ] && git -c safe.directory='*' rev-parse --verify --quiet origin/main >/dev/null 2>&1; then
  BASE="origin/main"
fi
FILES=()
if [ -n "$BASE" ] && git -c safe.directory='*' rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  while IFS= read -r f; do
    case "$f" in
      plans/*.yaml|plans/*/*.yaml|templates/*.yaml|templates/*/*.yaml)
        [ -f "$f" ] && FILES+=("$f") ;;
    esac
  done < <(git -c safe.directory='*' diff --name-only --diff-filter=d "$BASE"...HEAD 2>/dev/null)
  echo "plan-arch-review: scoped to ${#FILES[@]} changed file(s) vs ${BASE}."
else
  FILES=("${ALL_FILES[@]}")
  echo "plan-arch-review: WARNING — no diff base resolvable; falling back to ALL ${#FILES[@]} file(s)."
fi
[ ${#FILES[@]} -eq 0 ] && { echo "plan-arch-review: no Plan/PlanTemplate YAML changed — nothing to review."; exit 0; }

# ── dossier injection ────────────────────────────────────────────────────
# Extract every repo: target the changed Plans name, fetch its dossier (plus
# estate.md, always). Unavailable dossiers are DEGRADED-EVIDENCE, reported on
# the panel — never silently omitted, the shipprovenserver staleness rule.
TARGETS=$(python3 - "${FILES[@]}" <<'PY'
import re, sys
repos, seen = [], set()
for path in sys.argv[1:]:
    try:
        text = open(path).read()
    except OSError:
        continue
    for m in re.finditer(r'^\s*repo:\s*[\x22\x27]?([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)', text, re.M):
        r = m.group(1)
        if r not in seen:
            seen.add(r); repos.append(r)
print("\n".join(repos))
PY
)
DOSSIER_BLOCK=""
EVIDENCE_STATUS="ok"
fetch_doc() {  # fetch_doc <path> -> raw body or empty
  local path="$1" accept="application/vnd.github.raw"
  curl -fsS -m 15 -H "Accept: $accept" ${ARCH_GH_TOKEN:+-H "Authorization: Bearer $ARCH_GH_TOKEN"} \
    "https://api.github.com/repos/${ARCH_REPO}/contents/${path}" 2>/dev/null || true
}
EVIDENCE=$(mktemp)
echo "# ESTATE MAP (estate.md)" > "$EVIDENCE"
if ESTATE_DOC=$(fetch_doc "estate.md"); [ -n "$ESTATE_DOC" ]; then
  printf '%s\n' "$ESTATE_DOC" >> "$EVIDENCE"
else
  EVIDENCE_STATUS="degraded: estate.md unavailable from ${ARCH_REPO}"
  echo "plan-arch-review: WARNING — estate.md unavailable; the review runs without the cross-repo map." >> "$EVIDENCE"
fi
MISSED=0
if [ -n "$TARGETS" ]; then
  while IFS= read -r repo; do
    [ -z "$repo" ] && continue
    SHORT="${repo##*/}"
    echo "" >> "$EVIDENCE"; echo "# DOSSIER ${repo} (dossiers/${SHORT}.md)" >> "$EVIDENCE"
    if D=$(fetch_doc "dossiers/${SHORT}.md"); [ -n "$D" ]; then
      printf '%s\n' "$D" >> "$EVIDENCE"
    else
      MISSED=$((MISSED+1))
      echo "(no dossier for ${repo} — review its fit from the Plan text alone and say so)" >> "$EVIDENCE"
    fi
  done <<< "$TARGETS"
fi
[ "$MISSED" -gt 0 ] && EVIDENCE_STATUS="degraded: ${MISSED} dossier(s) unavailable from ${ARCH_REPO}"
echo "plan-arch-review: evidence assembled (${TARGETS:+targets: $(echo "$TARGETS" | tr '\n' ' ')}; status: ${EVIDENCE_STATUS})"
DOSSIER_BLOCK=$(cat "$EVIDENCE")

set +e  # advisory — never fail the gate
BASEURL="https://github.com/${REPO_OWNER:-mikelear}/${REPO_NAME:-leartech-plan-catalog}/blob/main"
SHA=$(printf '%s' "${PULL_PULL_SHA:-$(git rev-parse HEAD 2>/dev/null)}" | cut -c1-8)

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
    payload=$(python3 -c "import json,sys; print(json.dumps({'model':sys.argv[1],'messages':[{'role':'system','content':sys.argv[2]},{'role':'user','content':'ARCHITECTURE EVIDENCE (cite these, or say they do not apply):\n\n'+sys.argv[3]+'\n\nReview this Plan file '+sys.argv[4]+' for ARCHITECTURE FIT against that evidence:\n\n'+sys.argv[5]}],'max_tokens':500,'temperature':0}))" "$model" "$SYS" "$DOSSIER_BLOCK" "$file" "$content")
    resp=$(curl -fsS -m 60 -X POST "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d "$payload" 2>/dev/null)
    printf '%s' "$resp" | python3 "$PARSE" "$file" "$model" >> "$RESULTS"
  done
done

BUILD=$(mktemp)
cat > "$BUILD" <<'PY'
import json, sys
results_file, cluster, models_csv, base = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
sha = sys.argv[5] if len(sys.argv) > 5 else ""
criteria_version = sys.argv[6] if len(sys.argv) > 6 else "unknown"
evidence_status = sys.argv[7] if len(sys.argv) > 7 else "ok"
arch_repo = sys.argv[8] if len(sys.argv) > 8 else "?"
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

L = [f"<!-- plan-arch-review-{cluster} -->"]
L += [f"## 🏛️ plan-arch-review — advisory · cluster `{cluster}`  "
      "![advisory](https://img.shields.io/badge/plan--arch--review-advisory-blue)", ""]
L += ["> [!NOTE]",
      f"> Architecture-fit review against the **Cartographer's dossiers** ({arch_repo}), "
      "routed through the owned gateway. Advisory only — it never blocks; `plan-lint` "
      "decides merge-eligibility.", ""]

if evidence_status != "ok":
    L += ["> [!WARNING]",
          f"> **Evidence {evidence_status}.** Findings below ran without the full dossier set — "
          "treat missing-evidence areas as unreviewed, not approved.", ""]

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

machine = {"gate": "plan-arch-review", "cluster": cluster,
           "criteria_version": criteria_version, "evidence": evidence_status, "arch_repo": arch_repo,
           "models": [{"logical": lg, "supplier": sup(lg),
                       "resolved": agg.get(lg, {}).get("resolved", lg),
                       "tokens": agg.get(lg, {}).get("tokens", 0)} for lg in models],
           "reviews": [{"file": r["file"], "logical": r["logical"], "supplier": sup(r["logical"]),
                        "verdict": r.get("verdict", ""), "tokens": r.get("tokens", 0)} for r in rows]}
L += [f'<details><summary>🤖 Machine-readable verdict (leartech.review/v1 shape — see '
      f'<a href="{base}/AGENTS.md">AGENTS.md</a>)</summary>', "",
      "```json", json.dumps(machine, indent=2), "```", "", "</details>", ""]
L += [f"**References:** [arch criteria]({base}/docs/arch-review-criteria.md) · "
      f"[dossier repo](https://github.com/{arch_repo}) · [AGENTS.md]({base}/AGENTS.md)", ""]
stamp = (f"plan-arch-review · reviewed `{sha}`" + (f" · cluster `{cluster}`" if cluster else "")
         + f" · criteria v{criteria_version} · evidence {evidence_status} · updated in place on each push")
L += ["---", "_Advisory — does this Plan fit the estate? The per-model verdicts above feed the "
      "Plan-quality flywheel alongside plan-ai-review. `plan-lint` remains the hard gate._",
      "", f"<sub>{stamp}</sub>"]
print("\n".join(L))
PY

BODY=$(python3 "$BUILD" "$RESULTS" "$CLUSTER" "$MODELS" "$BASEURL" "$SHA" "$CRITERIA_VERSION" "$EVIDENCE_STATUS" "$ARCH_REPO")
echo "$BODY"

MARKER="<!-- plan-arch-review-${CLUSTER} -->"
echo "plan-arch-review: post-context — cluster=$CLUSTER token=$([ -n "${GIT_TOKEN:-}" ] && echo set || echo MISSING) pr=${PULL_NUMBER:-MISSING}"
if [ -n "${GIT_TOKEN:-}" ] && [ -n "${PULL_NUMBER:-}" ] && [ -n "${REPO_OWNER:-}" ] && [ -n "${REPO_NAME:-}" ]; then
  AUTH="Authorization: token ${GIT_TOKEN}"
  EXISTING=$(curl -fsSL -H "$AUTH" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments?per_page=100" 2>/dev/null | python3 -c "import json,sys
try:
 [print(c['id']) for c in json.load(sys.stdin) if c.get('body','').startswith('$MARKER')]
except Exception: pass" | head -1)
  PAYLOAD=$(python3 -c "import json,sys; print(json.dumps({'body':sys.stdin.read()}))" <<<"$BODY")
  if [ -n "$EXISTING" ]; then
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X PATCH -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/comments/${EXISTING}" 2>/dev/null)
    echo "plan-arch-review: updated sticky comment [$CLUSTER] -> HTTP $code"
  else
    code=$(curl -sSL -o /dev/null -w "%{http_code}" -X POST -H "$AUTH" -d "$PAYLOAD" "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/issues/${PULL_NUMBER}/comments" 2>/dev/null)
    echo "plan-arch-review: posted sticky comment [$CLUSTER] -> HTTP $code"
  fi
else
  echo "plan-arch-review: skipping PR comment (missing context) — review is in this log"
fi
