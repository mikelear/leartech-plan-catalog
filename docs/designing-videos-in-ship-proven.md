# Designing videos in Ship Proven

How explainer-video work — a terminal, a PR, a pipeline shown side by side, with
a moving spotlight and narration — runs in the shell. It is the **design loop
again**, with a video-specific standard: research → render a self-playing HTML
page → feedback → publish as a versioned artifact. Nothing new is invented; the
features a video may use are a fixed, reviewed vocabulary.

## Where the standard lives

The feature vocabulary and worked examples live in the
[`leartech-demo-videos`](https://github.com/mikelear/leartech-demo-videos) repo:

- `FEATURES.md` — one name per effect (Spotlight, Highlight, Zoom, Pin, Wire,
  Reveal, Dismiss, Handoff, Typewriter, Swap, Caption/Word-reveal, Voiceover,
  Scrubber, Auto-advance) and the beat field that turns each on.
- `AUTHORING.md` — the process and the beat shape.
- `site/*.html` — reference pages, one or more per feature, that actually render.

A new feature is a **reviewed PR to that repo** (a named entry in FEATURES.md +
a reference page that renders). That is the slow "feature loop". The design loop
below never adds a feature that isn't already in the repo.

## The loop

1. **Research** — `gateway__search` for look-and-feel references, AND the real
   data the video will show: `mcp__plan__*` (plans/steps/PRs/gates), `gh pr
   view|diff|checks`, and a read-only clone of the repo a screen belongs to.
   Golden rule: **never fabricate a PR number, SHA, or check result.** Reference
   the demo-videos repo's examples so you copy the closest one rather than
   inventing structure.
2. **Render** — write ONE self-contained HTML page under the working folder: the
   self-playing video (a timeline of beats driving the spotlight, pins, zoom,
   reveal/dismiss, caption and the scrubber/auto-advance chrome). Give every
   element you reference a `data-k` key. Optional `audio/*.mp3` for a recorded
   voiceover; otherwise the browser speech API reads each beat.
3. **Feedback** — the operator watches the page and says what they think. Revise
   and repeat. No tool does this.
4. **Publish** — publish the folder as a **versioned artifact** with
   `kind: video-bundle`. This is the SAME artifact cycle design uses —
   create → presigned PUT (bytes go **direct to GCS**; the MCP never carries
   them) → finalize. The files are the page, any `audio/*`, and a `SPEC.md` (the
   video's written rationale + provenance, the equivalent of design's DESIGN.md).

## Why publish is the point

The registry is where consumers already look. A video that ends as a file on a
laptop ends nowhere; one published as a `video-bundle` artifact has lineage, a
sealed immutable revision, and is embeddable anywhere — iframe the artifact, or
copy its folder into a site's `assets/`. Embedding it in a product (e.g. the
Angular website) is then an ordinary `kind: pr` Plan on `leartech-agent-ng`
whose goal is "consume video-bundle artifact `<id>` and embed it": the Plan
holds the artifact id, not the bytes.

## Two loops, kept separate

- **Feature loop** (slow, reviewed): web research/ideas → PR to
  `leartech-demo-videos` → a named feature + a rendering reference page → merge.
  This is where "are there new effects out there?" is answered — once, into the
  standard.
- **Design loop** (fast, the shell): the four steps above. It consumes the repo
  + real data and publishes a bundle. It stays stateless: no feature registry,
  no stored repo list — the repo a video is about is a per-call input, and
  discovery is live reads.

## The preference, stated once

The preferred Ship Proven way is: research → self-playing HTML → feedback →
publish as a `video-bundle` artifact, against the fixed feature vocabulary in
`leartech-demo-videos`. A session that sketches a bespoke animation outside that
vocabulary lands outside both the standard and the registry — that is the
failure this preference exists to prevent.
