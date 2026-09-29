#!/usr/bin/env bash
# Self-test for strip_yaml_comments in sync-templates-to-controller.sh.
#
# WHY IT LIVES HERE AND NOT IN go test. The Go test image is golang:1.26-alpine,
# which has neither bash nor python3, so a Go test could only ever skip — and a
# check that skips in CI reports the same green as one that ran. The sync itself
# runs in python:3.12-alpine, so the tools exist there; this runs in the same
# kind of image the thing under test runs in.
#
# NO YAML PARSER. pyyaml is not in python:3.12-alpine, and installing it is how
# the plan review silently lost a criterion for four months. These three
# assertions need only the stdlib, and together they pin what matters:
#
#   1. nothing is invented   — the output is a subsequence of the input, so the
#                              filter can only ever REMOVE lines
#   2. nothing is left       — no whole-line comment survives except the exempt
#                              generated marker, which is the point of the change
#   3. nothing is over-taken — a # line inside a block scalar is content and
#                              must survive; this is the case that corrupts an
#                              agent's brief, and neither real template has one
set -euo pipefail
cd "$(dirname "$0")/.."

SYNC=scripts/sync-templates-to-controller.sh
[ -f "$SYNC" ] || { echo "FAIL: $SYNC not found — this self-test cannot pass by failing to find it"; exit 1; }
eval "$(sed -n '/^strip_yaml_comments() {/,/^}/p' "$SYNC")"
eval "$(sed -n '/^emit_template() {/,/^}/p' "$SYNC")"
for f in strip_yaml_comments emit_template; do
  type "$f" >/dev/null 2>&1 || { echo "FAIL: could not extract $f from $SYNC — this self-test cannot pass by examining nothing"; exit 1; }
done

fail=0
say() { printf '  %s\n' "$*"; }

# Asserts the output is a subsequence of the input: the filter may remove a
# line but may never add, alter or reorder one.
SUBSEQ='
import os, sys
out = sys.stdin.read().split("\n")
src = open(os.environ["SRC"]).read().split("\n")
i = 0
for line in out:
    if line.strip() == "":
        continue
    while i < len(src) and src[i] != line:
        i += 1
    if i == len(src):
        sys.stdout.write("  FAIL: %s - not in the source, in order: %r\n" % (os.environ["SRC"], line))
        sys.exit(1)
    i += 1
'


echo "==> 1/4 the filter only removes lines (subsequence check)"
checked=0
for src in templates/*.yaml; do
  [ -e "$src" ] || { echo "FAIL: no templates to examine"; exit 1; }
  checked=$((checked + 1))
  out=$(strip_yaml_comments < "$src")
  # The program goes in -c and the DATA goes on stdin. Written as two heredocs
  # on one command it silently fed the YAML to python as its program, and the
  # SyntaxError that produced looked like a broken template rather than a
  # broken test.
  if ! SRC="$src" python3 -c "$SUBSEQ" <<<"$out"; then fail=1; else say "$src: ok"; fi
done
say "examined $checked template(s)"

echo "==> 2/4 no whole-line comment survives"
for src in templates/*.yaml; do
  left=$(strip_yaml_comments < "$src" | grep -cE "^[[:space:]]*#" || true)
  if [ "$left" != "0" ]; then
    say "FAIL: $src leaves $left whole-line comment(s), which the gate counts"
    fail=1
  else
    say "$src: 0 counted prose lines"
  fi
done

echo "==> 3/4 a # inside a block scalar is content, not a comment"
fixture=$(cat <<'YAML'
# a real comment, must go
apiVersion: agent.leartech.io/v1alpha1
kind: PlanTemplate
spec:
  steps:
    - name: dev
      inputs:
        goal: |
          Fix the build.
          # not a comment: this line is part of the prompt
          Run make test.
        other: value
YAML
)
got=$(strip_yaml_comments <<<"$fixture")
case "$got" in
  *"# not a comment"*) say "block scalar content preserved" ;;
  *) say "FAIL: the # line was taken out of a block scalar — the agent would get a different brief"; fail=1 ;;
esac
case "$got" in
  *"# a real comment"*) say "FAIL: a real comment survived"; fail=1 ;;
  *) say "real comment removed" ;;
esac
case "$got" in
  *"other: value"*) ;;
  *) say "FAIL: the key after the block scalar was lost"; fail=1 ;;
esac

echo "==> 4/4 the FILE the sync writes costs nothing against the comment gate"
# The assertion that was missing. 1-3 test the filter; this tests the artefact.
# controller#199 went red a second time with a correct filter, because the
# emitted file still opened with a `---` separator and the gate's commentRe
# includes `--` (for SQL), so the YAML document separator counted as prose.
for src in templates/*.yaml; do
  b=$(basename "$src" .yaml)
  counted=$(emit_template "$b" "$src" | grep -vE "Code generated" | grep -cE "^[[:space:]]*(//|#|--)" || true)
  if [ "$counted" != "0" ]; then
    say "FAIL: the generated $b file has $counted line(s) the gate counts as prose:"
    emit_template "$b" "$src" | grep -vE "Code generated" | grep -nE "^[[:space:]]*(//|#|--)" | sed 's/^/      /'
    fail=1
  else
    say "$b: generated file has 0 gate-counted lines"
  fi
  case "$(emit_template "$b" "$src")" in
    *"Code generated"*) ;;
    *) say "FAIL: the generated $b file has no exempt marker"; fail=1 ;;
  esac
done

[ "$fail" = 0 ] && echo "==> strip self-test: PASS" || echo "==> strip self-test: FAIL"
exit "$fail"
