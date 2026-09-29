#!/usr/bin/env bash
# sync-templates-to-controller — auto-release for merged PlanTemplates (GitOps PR).
#
# Postsubmit (on merge to catalog main): mirror every templates/*.yaml into the
# controller's PlanTemplate library as a GitOps PR. The controller is the SINGLE
# SOURCE for PlanTemplate CRDs (charts/.../templates/plantemplates.yaml renders to
# the gitops config-root), so this keeps ONE install path. The PR then runs the
# controller's own quality gates + a human merge → controller release → GitOps
# apply. Net: a merged catalog template is double-gated (catalog plan-lint/ai-review
# here + the controller's gates there) and never bypasses GitOps.
#
# Idempotent: one fixed branch, force-pushed; a PR is opened only if one isn't
# already open; no diff → no-op. Runs on ONE cluster only (avoids gcp+az dupes).
#
# Env: GIT_TOKEN + GIT_USER (tekton-git), CLUSTER_ID.
set -uo pipefail

CLUSTER="${CLUSTER_ID:-unknown}"
PRIMARY="${TEMPLATE_SYNC_CLUSTER:-gcp}"
CTRL_REPO="mikelear/leartech-orchestrator-controller"
CAT_REPO="mikelear/leartech-plan-catalog"
TDIR="charts/leartech-orchestrator-controller/templates"
BRANCH="catalog-plantemplate-sync"
API="https://api.github.com"

# Only the primary cluster opens the PR — the postsubmit fires on both clusters.
if [ "$CLUSTER" != "$PRIMARY" ]; then
  echo "sync-templates: cluster=$CLUSTER (primary=$PRIMARY) — skipping to avoid duplicate PRs."
  exit 0
fi
if [ -z "${GIT_TOKEN:-}" ] || [ -z "${GIT_USER:-}" ]; then
  echo "sync-templates: GIT_TOKEN/GIT_USER unset — skipping (tekton-git not mounted)."
  exit 0
fi
shopt -s nullglob
SRCS=(templates/*.yaml)
if [ ${#SRCS[@]} -eq 0 ]; then
  echo "sync-templates: no templates/ to sync."
  exit 0
fi

# Credential helper (keeps the token out of URLs/logs).
git config --global credential.helper store
printf 'https://%s:%s@github.com\n' "$GIT_USER" "$GIT_TOKEN" > "$HOME/.git-credentials"
git config --global user.email "ci@leartech.io"
git config --global user.name "leartech-ci"

# Provenance: resolve the catalog PR that triggered this sync (the merge commit),
# so the controller PR + each synced file link back to who proposed + reviewed it.
AUTH="Authorization: token ${GIT_TOKEN}"
TRIG_SHA=$(git rev-parse HEAD 2>/dev/null)
prForSha() { # $1=sha -> "url|number" of the catalog PR that introduced it (or empty)
  curl -fsSL -H "$AUTH" "$API/repos/${CAT_REPO}/commits/$1/pulls" 2>/dev/null | python3 -c "import json,sys
try:
 p=json.load(sys.stdin)
 print('%s|%d'%(p[0]['html_url'],p[0]['number'])) if p else print('')
except Exception: print('')"
}
TRIG=$(prForSha "$TRIG_SHA")
CAT_PR_URL="${TRIG%%|*}"; CAT_PR_NUM="${TRIG##*|}"
CAT_COMMIT_URL="https://github.com/${CAT_REPO}/commit/${TRIG_SHA}"
if [ -n "$CAT_PR_URL" ]; then
  PROV_LINE="catalog PR #${CAT_PR_NUM} (${CAT_PR_URL}), commit ${TRIG_SHA:0:8}"
else
  PROV_LINE="catalog commit ${TRIG_SHA:0:8} (${CAT_COMMIT_URL})"
fi
echo "sync-templates: provenance — $PROV_LINE"

# strip_yaml_comments removes whole-line YAML comments from stdin.
#
# BLOCK SCALARS ARE CONTENT, NOT COMMENTS. A naive line filter corrupts exactly
# what it is copying: inside a `goal: |` block every line is literal text handed
# to the agent, and one beginning with # is part of the prompt. The bootstrap
# template's dev goal is that shape. So the filter tracks block scalars by the
# indentation of the key that opened them and copies their body verbatim.
#
# Trailing comments (`required: true # the service name`) are deliberately kept:
# the gate's commentRe is anchored at the start of a line, so they cost nothing,
# and they are the per-field context worth having next to the field.
strip_yaml_comments() {
  python3 -c '
import sys
out, block_indent = [], None
for raw in sys.stdin.read().split("\n"):
    stripped = raw.lstrip(" ")
    indent = len(raw) - len(stripped)
    if block_indent is not None:
        if stripped == "" or indent > block_indent:
            out.append(raw)
            continue
        block_indent = None
    if stripped.startswith("#"):
        continue
    out.append(raw)
    body = raw.split("#")[0].rstrip()
    if ":" in body:
        val = body.split(":", 1)[1].strip()
        if val and val[0] in "|>" and all(c in "|>+-0123456789" for c in val):
            block_indent = indent
collapsed, blank = [], False
for ln in out:
    if ln.strip() == "":
        if blank:
            continue
        blank = True
    else:
        blank = False
    collapsed.append(ln)
sys.stdout.write("\n".join(collapsed).strip("\n") + "\n")
'
}

# emit_template writes the chart file for one template: $1 = base name, $2 = source path.
#
# A FUNCTION SO THE SELF-TEST CAN CHECK WHAT IS ACTUALLY WRITTEN. The first
# version of this change tested strip_yaml_comments alone, which passed while
# the file the script emitted still tripped the gate — the component was right
# and the artefact was not. controller#199 went red a second time on it.
#
# NO `---` SEPARATOR. The gate's commentRe is `^\s*(//|#|--|...)`, where `--`
# is there for SQL, so a YAML document separator matches it and counts as a
# prose line. The file holds one document and Helm renders it identically
# without the separator, so the line bought nothing and cost the gate.
emit_template() {
  local name="$1" source="$2"
  echo '{{- if .Values.planTemplates.install }}'
  echo "# Code generated by sync-templates-to-controller from templates/${name}.yaml. DO NOT EDIT."
  # NB: keep this file's content a pure function of the source template — do NOT
  # embed the triggering commit here, or every catalog merge would rewrite it and
  # spuriously re-sync. Volatile provenance (which catalog PR triggered the sync)
  # lives in the controller PR body + commit message instead.
  strip_yaml_comments < "$source"
  echo '{{- end }}'
}

WORK=$(mktemp -d)
if ! git clone --depth 1 "https://github.com/${CTRL_REPO}.git" "$WORK/ctrl" 2>/dev/null; then
  echo "sync-templates: cannot clone $CTRL_REPO (bot may lack access) — skipping."; exit 0
fi
cd "$WORK/ctrl" || exit 0
BASE=$(git rev-parse --abbrev-ref HEAD)   # controller default branch (master/main)

git checkout -B "$BRANCH" >/dev/null 2>&1
mkdir -p "$TDIR"
count=0
for src in "$OLDPWD"/templates/*.yaml; do
  b=$(basename "$src" .yaml)
  # Never sync example/reference templates into the shared controller library —
  # they're documentation, not runtime building blocks. (An example with the old
  # spec.description shape broke the build-cluster boot once; belt-and-braces.)
  case "$b" in example-*|*-example) echo "sync-templates: skipping example template $b"; continue;; esac
  dst="$TDIR/catalog-${b}-plantemplate.yaml"
  # ONE HEADER LINE, IN THE CANONICAL GENERATED FORM, AND NO COPIED PROSE.
  #
  # The controller runs the comment gate, which fails when a change adds more
  # prose comment lines than test lines. A sync PR is a machine copy of YAML and
  # can never add a test line, so ANY commented template failed it permanently —
  # controller#199 was the first sync after the gate reached that repo, and it
  # went red on 49 prose lines it had faithfully copied.
  #
  # Stripping is the right answer rather than a gate exemption, because the
  # reasoning belongs in THIS repo: it is reviewed here by plan-lint and
  # ai-review, and this is the file someone edits. Copying it into a file
  # stamped DO NOT EDIT gives the same explanation two homes and lets them
  # drift. The header points at the source instead.
  #
  # "Code generated ... DO NOT EDIT." is the canonical Go form and what the
  # gate's functional-directive exemption already recognises, so the one line
  # that remains costs nothing against the ratchet.
  emit_template "$b" "$src" > "$dst"
  count=$((count + 1))
done

git add "$TDIR"/catalog-*-plantemplate.yaml
if git diff --cached --quiet; then
  echo "sync-templates: controller library already up to date ($count template(s)); nothing to PR."
  exit 0
fi

git commit -q -m "Sync PlanTemplate(s) from the ShipProven Plan Catalog

Auto-proposed from mikelear/leartech-plan-catalog (reviewed by plan-lint +
ai-review). Re-gated by this repo's checks before merge."
git push -f origin "$BRANCH" >/dev/null 2>&1 || { echo "sync-templates: push failed"; exit 0; }

# Build the PR body (with provenance) once — used to create OR refresh.
body=$(printf 'Auto-proposed PlanTemplate(s) from the **ShipProven Plan Catalog** (mikelear/leartech-plan-catalog).\n\n**Originating:** %s\n\nAlready passed the catalog gate (plan-lint R1-R19 + CRD-schema + ai-review) and a human merge there. This PR re-gates them through this repo before they enter the shared PlanTemplate library. Merging releases the controller → GitOps renders + applies.\n\n_Generated by the catalog release postsubmit._' "$PROV_LINE")

# Create the PR, or refresh the existing one's body so provenance stays current.
existing=$(curl -fsSL -H "$AUTH" "$API/repos/${CTRL_REPO}/pulls?head=mikelear:${BRANCH}&state=open" 2>/dev/null | python3 -c "import json,sys
try:
 p=json.load(sys.stdin); print(p[0]['number']) if p else print('')
except Exception: print('')")
if [ -n "$existing" ]; then
  payload=$(python3 -c "import json,sys; print(json.dumps({'body':sys.stdin.read()}))" <<<"$body")
  code=$(curl -sSL -o /dev/null -w "%{http_code}" -X PATCH -H "$AUTH" -d "$payload" "$API/repos/${CTRL_REPO}/pulls/${existing}" 2>/dev/null)
  echo "sync-templates: refreshed controller PR #${existing} (body provenance) -> HTTP $code"
else
  payload=$(python3 -c "import json,sys; print(json.dumps({'title':'Sync PlanTemplates from the Plan Catalog','head':'$BRANCH','base':'$BASE','body':sys.stdin.read()}))" <<<"$body")
  code=$(curl -sSL -o /tmp/pr-resp.json -w "%{http_code}" -X POST -H "$AUTH" -d "$payload" "$API/repos/${CTRL_REPO}/pulls" 2>/dev/null)
  url=$(python3 -c "import json;print(json.load(open('/tmp/pr-resp.json')).get('html_url',''))" 2>/dev/null)
  echo "sync-templates: opened controller PR (base=$BASE) -> HTTP $code $url"
fi
