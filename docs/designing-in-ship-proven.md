# Designing in Ship Proven

How design work — look-and-feel, UI options, architecture-choice pitch pages —
runs in the shell. The short version: **the shell's own loop is the preferred
path, and design MCPs are supported, not fought.** Both end in the same place
when used well; only one does when they're not.

## The loop

1. **Research** — `gateway__search` (or `ship-proven websearch`) for
   look-and-feel options, pattern references, prior art. The calls run on the
   gateway: egress and spend are the platform's, not the laptop's.
2. **Render** — write HTML option pages under the working folder.
   Self-contained single files, more than one option, the
   `ml-architecture-options.html` shape. Check they actually render: either
   `testfiles/design/check_render.sh <file.html>`, or — if a **render plugin**
   is mounted — `plugin__render__screenshot` (Chrome headless screenshot,
   non-blank asserted — a page that renders blank is invisible to a text
   check). The plugin is the preferred path when present: it is the SAME
   headless-Chrome screenshot, but checked and advertised rather than assumed,
   and the model can call it itself to SEE what it drew before asking for
   feedback. See "Local tool plugins" below.
3. **Feedback** — the operator looks at the options and says what they think.
   Revise and repeat. This is the part no tool does for you; it's a
   conversation.
4. **Publish** — `ship-proven design publish <folder>` — DESIGN.md (the
   written rationale, required) plus every `*.html` beside it become a
   **versioned artifact** in the registry, with lineage. A re-publish with
   `--artifact-id` opens a new revision on the sealed one rather than a new
   artifact.

## Return the links when a round finishes

When a render round is done, **list every option back in the reply as
pasteable links** — one `file://` absolute path per HTML file (or the
`http://` URL if they're served), so the operator can copy-paste straight into
a browser instead of hunting the working folder. Do it every time options are
written or revised, before asking for feedback: step 3 is a conversation, and
it can't start until the operator can actually see what was built. One link per
option, in the order they were created, is enough.

## Why publish is the point

The registry is where dev agents already look. A design that ends as a file on
a vendor's disk ends nowhere; one that ends as an artifact is consumed by the
same loop that builds the thing. The artifact MCP's cycle is
create → presigned PUT → finalize (the bytes go **direct to GCS** — the MCP is
a control plane and never carries them; `ship-proven design publish` owns that
PUT step for you). A draft created and never uploaded stays **pending
forever** — finalise or set the `ttl` hint; the loop does the first.

## Design MCPs (stitch, or one you add)

Fine to use. `mcp__stitch__*` generates screen variants,
`apply_design_system` applies tokens; a generated screen is a legitimate
starting point for step 2. What the MCPs don't do is end the loop: the
publish step is still the shell's. Generated screens published as artifacts
keep lineage and reach the dev agents; ones left in the vendor's project
don't.

- What's loaded is in the shell's startup lines (`skills: …`) and `/mcp`.
- `ship-proven mcp add <name> --url U` registers one.
- `ship-proven design guidance` prints the loop with the loaded design MCPs
  named honestly.

## Local tool plugins (a renderer, a simulator)

The shell has a fourth tool lane beside `local__`, `mcp__…` and `gateway__`:
**`plugin__<name>__<tool>`** — a capability the operator installed on THIS
machine, wrapped so the model can drive it. For design work two matter:

- **`plugin__render__screenshot`** — turns an HTML option into a PNG with
  whatever headless Chrome this machine has. This is how the model *sees* what
  it drew: render the options (step 2), screenshot each, reason over the image,
  then ask for feedback. It replaces the old assumption that a Chrome is simply
  present — the plugin is health-checked, so if no browser is installed it is
  skipped-and-named at startup and the loop proceeds on the `file://` links
  alone. Nothing breaks; the "see it" step is just checked, advertised and
  gated now.
- **a simulator plugin** (`plugin__ios-sim__…`, `plugin__android-sim__…`, a
  `plugin__godot__export`) — for a mobile-game or app design, the HTML loop
  pitches the *look*; a mounted simulator plugin lets the model show the *thing
  running*. Neither is required; both are used if present.

How to tell what is available, and to add one:

- `ship-proven plugin list` — the plugins this machine knows about, with live
  health; `plugin doctor` says why any is unavailable.
- `ship-proven plugin tools <name>` — the tools a plugin offers the model
  (the `mcp tools` analogue).
- `ship-proven plugin add --manifest <file.json>` — register one; it is
  health-checked at add and again at every shell startup.

A plugin that shells out is gated exactly like `local__bash`: it asks before
running unless the shell was started `--allow-exec`.

## The preference, stated once

The preferred Ship Proven way is the shell design tooling: research → HTML
options → feedback → `design publish`. Design MCPs coexist and are supported.
If a session starts sketching options without the prompt line, the model
picks whatever design tool it was trained on, and the result lands outside
the registry — that's the failure the preference exists to prevent, not a
judgement about the tools.
