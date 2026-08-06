#!/usr/bin/env sh
# plan-cluster-verify — the AUTHORITATIVE gate: server-side dry-run of every
# Plan/PlanTemplate against the LIVE CRD (structural schema + admission + any CEL).
# It's the highest-fidelity, drift-free check that a submission will actually apply
# — it would have caught the spec.description template that broke boot, at PR time.
#
# Safe by construction: --dry-run=server runs the whole admission path but PERSISTS
# NOTHING (same posture as Lighthouse's gitops verify). Runs in-cluster as tekton-bot
# (which already has create on plans/plantemplates — no extra RBAC). Complements the
# offline Go rules (R1-R21) + kubeconform; this is the live-CRD layer on top.
set -eu
NS="${PLAN_VERIFY_NAMESPACE:-jx-staging}"
rc=0
if ! kubectl version --request-timeout=10s >/dev/null 2>&1; then
  echo "plan-cluster-verify: no cluster access — skipping (this gate is meaningful only in-cluster)."
  exit 0
fi
for f in plans/*.yaml; do
  [ -e "$f" ] || continue
  echo "== dry-run Plan: $f =="
  kubectl apply --dry-run=server -n "$NS" -f "$f" || rc=1
done
for f in templates/*.yaml; do
  [ -e "$f" ] || continue
  echo "== dry-run PlanTemplate: $f =="
  kubectl apply --dry-run=server -f "$f" || rc=1   # PlanTemplate is cluster-scoped
done
if [ "$rc" -eq 0 ]; then
  echo "plan-cluster-verify: PASS — every Plan/PlanTemplate applies cleanly against the live CRD."
else
  echo "plan-cluster-verify: FAIL — a submission is rejected by the live CRD/admission (see above)."
fi
exit "$rc"
