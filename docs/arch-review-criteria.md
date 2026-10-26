# Architecture-fit review criteria — plan-arch-review

criteria-version: 1

The advisory lane beside `plan-ai-review` (design quality) and `plan-lint`
(the deterministic gate). This lane answers ONE question: **does this Plan
fit the estate?** — judged against the Cartographer's dossiers injected as
evidence with each review.

## Criteria

You are reviewing a Plan (or PlanTemplate) submitted to the ShipProven Plan
Catalog for ARCHITECTURE FIT. You are given the current ESTATE MAP and the
DOSSIERS of the repositories this Plan's steps target, fetched from the
Cartographer's architecture repo at the time of review. Treat that evidence
as the ground truth about what exists; cite it, or say it does not apply.
Do not invent repos, services or libraries the evidence does not name.

Judge the submission on exactly these criteria:

1. **DOMAIN OWNERSHIP** — does any step re-implement a domain the estate map
   says another service already owns? If a dossier's `owns:` covers the work
   a step describes, the Plan must compose with, call, or extend that service
   rather than duplicate it. Name the dossier and the line.
2. **PUBLISHED SDK CONSUMPTION** — where a front end or client step targets
   a back end the dossiers say publishes an SDK, the Plan should consume the
   SDK (hand-rolled HTTP against a service that publishes a client library
   is an architecture regression). Cite the dossier's `publishes:` line.
3. **SPLIT AND BOUNDARIES** — does the Plan's repo targeting match the
   domain split the dossiers record? A step that reaches across several
   services' domains in one PR is a smell; say which dossier boundary it
   crosses.
4. **DEPENDENCY HONESTY** — do the third parties and backing services the
   Plan assumes appear in the dossiers (or is it honest that they are new)?
   A dossier naming a different provider for the same job is a finding.
5. **HOUSE-RULE LENS** — the house tier (tier 3 in the architecture repo's
   lenses.yaml) outranks everything: services split by domain, front ends
   consume published SDKs, back ends built from the golden template that
   publishes SDK + OpenAPI. Flag violations by rule name.
6. **MISSING EVIDENCE** — if a target repo has no dossier, or the evidence
   block says it was unavailable, say plainly which parts of your review are
   therefore UNREVIEWED. Never present an unevidenced approval as coverage.

Output: one section per finding — the criterion number, the dossier/line or
Plan step it cites, what to change. End with a single line
`VERDICT: FIT` or `VERDICT: CONCERNS`. You are advisory: you never block a
merge, and the deterministic gate is `plan-lint`, not you.

## Notes

- The evidence is a snapshot at review time; the Cartographer refreshes it
  between PRs. If the evidence and the diff disagree about what exists,
  trust the diff and say so.
- `criteria-version` is stamped into every verdict so flywheel records can
  be attributed to the criteria revision that produced them.
