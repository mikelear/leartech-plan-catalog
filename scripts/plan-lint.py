#!/usr/bin/env python3
"""plan-lint — the DETERMINISTIC gate for the ShipProven Plan Catalog.

This is the "golangci for Plans": deterministic, un-bypassable structural + safety
checks on every Plan / PlanTemplate submitted to the catalog. It is the HARD gate
(exit non-zero = the PR cannot be merged); the multi-model ai-review step is the
advisory judgment layer on top.

Design: the safety surface GROWS WITH THE PIPELINE — add a new guarantee by adding
a rule here (and, later, a whole new Tekton step). Each rule below encodes a lesson
we've paid for in real runs; the rule numbers are stable so failures are greppable.

Scope: validates every *.yaml under plans/ and templates/. Provider-agnostic pure
Python (only PyYAML) so it runs identically in CI and on a laptop.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

import yaml

API_VERSION = "agent.leartech.io/v1alpha1"
STEP_KINDS = {"pr", "apply", "check"}
# AgentRun jobName goes into a 63-byte pod-template label; <plan>-<step>-<attempt>
# must fit. Keep names short so the run can actually start (lesson: a too-long name
# yields an AgentRun that never spawns — empty phase, no Job, no events).
MAX_NAME = 58
NAME_RE = re.compile(r"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")  # DNS-1123 label
LAPTOP_PATH_RE = re.compile(r"(/Users/|/home/|~/)")


class Findings:
    def __init__(self) -> None:
        self.errors: list[str] = []
        self.warns: list[str] = []

    def err(self, rule: str, where: str, msg: str) -> None:
        self.errors.append(f"[{rule}] {where}: {msg}")

    def warn(self, rule: str, where: str, msg: str) -> None:
        self.warns.append(f"[{rule}] {where}: {msg}")


def _step_names(steps: list) -> list[str]:
    return [s.get("name", "") for s in steps if isinstance(s, dict)]


def lint_doc(doc: dict, where: str, f: Findings) -> None:
    # R2 apiVersion
    if doc.get("apiVersion") != API_VERSION:
        f.err("R2", where, f"apiVersion must be {API_VERSION!r}, got {doc.get('apiVersion')!r}")
    kind = doc.get("kind")
    # R3 kind
    if kind not in ("Plan", "PlanTemplate"):
        f.err("R3", where, f"kind must be Plan|PlanTemplate, got {kind!r}")
    # R4 name present, DNS-1123, length
    name = (doc.get("metadata") or {}).get("name", "")
    if not name:
        f.err("R4", where, "metadata.name is required")
    else:
        if not NAME_RE.match(name):
            f.err("R4", where, f"metadata.name {name!r} is not a DNS-1123 label")
        if len(name) > MAX_NAME:
            f.err("R4", where, f"metadata.name {name!r} is {len(name)}>{MAX_NAME} chars — the AgentRun will never spawn (63-byte label cap)")
    spec = doc.get("spec") or {}
    if not isinstance(spec, dict) or not spec:
        f.err("R1", where, "spec is required and must be a mapping")
        return

    if kind == "Plan":
        _lint_plan(spec, where, f)
    elif kind == "PlanTemplate":
        _lint_template(spec, where, f)

    # R10 no laptop/absolute-path artifact refs anywhere in the spec (plans run in a
    # K8s Job with only the target repo checked out — laptop paths never resolve).
    blob = yaml.safe_dump(spec)
    for m in set(LAPTOP_PATH_RE.findall(blob)):
        f.err("R10", where, f"spec references a laptop/absolute path ({m!r}) — reference only repo-committed or defined-store artifacts")


def _lint_plan(spec: dict, where: str, f: Findings) -> None:
    # R5 HOLD-BY-DEFAULT — the load-bearing safety rule. A catalog Plan is a PROPOSAL:
    # it must NOT be able to execute on merge. Execution is a separate, human-gated
    # promote/unpause. The reconciler honors spec.paused=true (MUST NOT spawn AgentRuns).
    if spec.get("paused") is not True:
        f.err("R5", where, "spec.paused MUST be true — catalog Plans are hold-by-default proposals; execution is a separate human promote/unpause")

    steps = spec.get("steps")
    if not isinstance(steps, list) or not steps:
        f.err("R6", where, "spec.steps must be a non-empty list")
        return
    names = _step_names(steps)
    # R7 unique step names
    dupes = {n for n in names if names.count(n) > 1}
    if dupes:
        f.err("R7", where, f"duplicate step names: {sorted(dupes)}")
    known = set(names)
    for i, s in enumerate(steps):
        sw = f"{where} step[{i}]={s.get('name','?') if isinstance(s,dict) else '?'}"
        if not isinstance(s, dict) or not s.get("name"):
            f.err("R6", sw, "each step needs a name")
            continue
        # R6 a step is either a template include (use:) OR a concrete step (kind+agentType)
        if "use" in s:
            if "kind" in s:
                f.warn("R6", sw, "a `use:` step should not set `kind` (the template's steps carry kind)")
        else:
            kind = s.get("kind")
            if kind not in STEP_KINDS:
                f.err("R6", sw, f"kind must be one of {sorted(STEP_KINDS)} (got {kind!r})")
            if not s.get("agentType"):
                f.err("R6", sw, "concrete step needs agentType")
        # R8 dependsOn references must resolve
        for dep in s.get("dependsOn") or []:
            if dep not in known:
                f.err("R8", sw, f"dependsOn references unknown step {dep!r}")


def _lint_template(spec: dict, where: str, f: Findings) -> None:
    # R9 templates declare params + steps; template steps are concrete (no nested use:
    # — expansion is depth-1) and carry kind+agentType.
    if not isinstance(spec.get("params"), list):
        f.warn("R9", where, "spec.params should be a list of {name, required}")
    steps = spec.get("steps")
    if not isinstance(steps, list) or not steps:
        f.err("R9", where, "spec.steps must be a non-empty list")
        return
    for i, s in enumerate(steps):
        sw = f"{where} step[{i}]={s.get('name','?') if isinstance(s,dict) else '?'}"
        if not isinstance(s, dict) or not s.get("name"):
            f.err("R9", sw, "each step needs a name")
            continue
        if "use" in s:
            f.err("R9", sw, "template steps must NOT nest `use:` (expansion is depth-1)")
        if s.get("kind") not in STEP_KINDS:
            f.err("R9", sw, f"kind must be one of {sorted(STEP_KINDS)} (got {s.get('kind')!r})")
        if not s.get("agentType"):
            f.err("R9", sw, "template step needs agentType")


def main() -> int:
    roots = [Path("plans"), Path("templates")]
    files = sorted(p for r in roots if r.exists() for p in r.rglob("*.yaml"))
    f = Findings()
    if not files:
        print("plan-lint: no Plan/PlanTemplate YAML under plans/ or templates/ — nothing to check.")
        return 0
    for path in files:
        try:
            docs = list(yaml.safe_load_all(path.read_text()))
        except yaml.YAMLError as e:
            f.err("R1", str(path), f"invalid YAML: {e}")
            continue
        for doc in docs:
            if isinstance(doc, dict) and doc:
                lint_doc(doc, str(path), f)

    print(f"plan-lint: checked {len(files)} file(s)")
    for w in f.warns:
        print(f"  WARN {w}")
    for e in f.errors:
        print(f"  FAIL {e}")
    if f.errors:
        print(f"\nplan-lint: FAIL — {len(f.errors)} error(s), {len(f.warns)} warning(s)")
        return 1
    print(f"\nplan-lint: PASS — 0 errors, {len(f.warns)} warning(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
