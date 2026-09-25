---
name: Design System
description: The design workflow for sprout's design/ tree — brief, tokens, wireframes, flows, screens — with validation between every step. Use for any design work (new designs, extending an existing tree, or reconciling design with code). Persona-agnostic.
---

# Design System — Workflow Knowledge

You are working in sprout's design idiom: **open, text-first formats, in the
workspace `design/` directory, versioned by git alongside the code they
describe.** There is no design→dev handoff; the tree and the implementation
co-evolve. Keep the tree truthful at every step.

This skill is persona-agnostic — activate it for any design work, whoever you
are. The `designer` persona is the specialist, but the workflow rules below
apply whenever the tree is touched.

## Core rules (read these first)

1. **Everything design-shaped lives under `design/` at the workspace root.**
   Nothing design-shaped goes anywhere else.
2. **Inventory before you write.** If `design/` exists, read
   `design/README.md` first, then run `design_assets` to see what is really
   there. Never guess the tree.
3. **Validate between every step.** After each artifact you write or edit, run
   `design_validate` and fix every `error` finding before moving on. A design
   iteration is not done because files were written.
4. **Extend, don't restructure.** In a brownfield tree, add to what exists.
   Do not reorganize someone else's naming, tiers, or layout on your own
   initiative — propose, then ask.
5. **Tokens are authoritative.** Literal colors and font strings in
   wireframes/screens are drift. Refer to tokens; keep the token files the
   single source of truth so export and sync stay clean — export before UI
   work, `design_sync` after it, and keep the tree truthful (see **Sync**).

## The directory contract

```
design/
  README.md                 # manifest: inventory, links, status, frames:
  tokens/                   # W3C DTCG design tokens (*.tokens.json)
  brand/                    # brand.md + logo SVGs
  icons/                    # icon SVGs + optional sprite.svg
  wireframes/               # one SVG per screen, stem == screen name
  screens/                  # hi-fi HTML/CSS screens, self-contained
  runtime/                  # FIXED screen-kit assets (scaffold-copied; never hand-edited)
  flows/                    # mermaid flow sources, one .mmd per flow
  feedback/                 # human annotations, JSON per target
  .cache/                   # render PNGs + critique scratch (gitignored)
```

Screen names (wireframe/screen/icon file stems) match `^[a-z0-9]+(-[a-z0-9]+)*$`.
The stem **is** the canonical screen name — flows reference it, README lists
it, feedback keys on it.

## Format charter (the non-obvious conventions)

The full charter is SP-140-1; the rules you must not get wrong:

- **Tokens** (`design/tokens/*.tokens.json`) — W3C DTCG. Top level is groups;
  leaves carry `$value` and `$type`. Allowed `$type`: `color`, `dimension`,
  `fontFamily`, `fontWeight`, `number`, `duration`, `cubicBezier`,
  `strokeStyle`, `border`. Aliases use `{group.token}` dot paths; dangling and
  cyclic references are **errors**. One file per tier (`color`, `typography`,
  `spacing`, `sizing`, `motion`) plus project tiers. **No `index.json`
  aggregation — consumers glob the directory.** `$extensions` passes through
  untouched.
- **Tier self-nesting (the rule most often missed).** Each tier file's top
  level contains a group **named for the tier**: `color.tokens.json` holds
  `{"color": {...}}`, `typography.tokens.json` holds `{"typography": {...}}`.
  References walk from that group — `{color.dark.bg.primary}` resolves because
  `color.tokens.json` has a top-level `color` group containing `dark.bg.primary`.
  A file with bare sub-groups (`{"dark": {...}}` with no `color` wrapper) parses
  and exports cleanly, but **every `{color.*}` reference against it dangles** —
  the brief reports the refs unknown and export names vars without the tier
  prefix. The validator's `consistency_token_ref_dangling` finding names this
  exact failure; if you see it, self-nest the tier file, do not rewrite the
  references.
- **Wireframes** (`design/wireframes/<screen-name>.svg`) — root
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 W H">` with integer
  `W H` matching a device frame declared in `design/README.md`. Self-contained:
  no `<script>`, no external `href`/`src` (embedded rasters are data URIs
  only). Text stays real `<text>` — never outlined paths (it must stay
  greppable and diffable). Interactive elements carry stable `id` attributes;
  navigation targets carry `data-nav="<screen-name>"` pointing at another
  wireframe's file stem. One screen per file.
- **Flows** (`design/flows/<flow-name>.mmd`) — one `flowchart` per file.
  Screen flows use **wireframe file stems as node ids** — that id == stem rule
  is what ties the graph to the screens, and the validator enforces it. Edge
  labels carry trigger semantics (`-- "tap Submit" -->`). Mermaid is the
  source of truth; rendered images are derived artifacts and are never edited.
- **Screens** (`design/screens/<screen-name>.html`) — self-contained HTML +
  inline or workspace-relative CSS. No network `<script>`, no CDN/font
  references. Same slug rule as wireframes.
- **Brand** (`design/brand/brand.md`) — palette references **into tokens**
  (`{color.brand.primary}`), never raw hex. That rule is what keeps the token
  file authoritative.
- **Icons** (`design/icons/*.svg`) — same self-containment and slug rules;
  optional `sprite.svg` holds `<symbol id="icon-name">` entries.
- **README manifest** (`design/README.md`) — human- and agent-readable
  inventory: purpose per screen and flow, status markers (`draft` / `review` /
  `ready`), links. Device frames are machine-parseable:

  ```
  frames:
    desktop: 1440x900
    mobile: 390x844
  ```

## The design workflow

```
brief → tokens → wireframes → flows → screens
```

Each arrow has a **validate** step. Do not skip ahead: screens built on
unvalidated tokens/wireframes inherit the errors, and drift compounds.

### Step 0 — Brief

Before producing artifacts, establish what is being designed and why. A brief
is intent, not output:

- The goal of this design iteration (new flow? extend a screen? fix drift?).
- Which screens/flows are in scope, and their names (stems).
- The device frames in play (drives every wireframe's `viewBox`).
- The primary action per screen (hierarchy needs one primary per view).

When `design/README.md` already exists, the brief is usually "extend the tree
to cover X" — read the tree first, then confirm scope. Later specs add a
`design_brief` tool that assembles this contract from the tree; until it
exists, assemble it by reading `design/README.md` + `design_assets` output.

For **brownfield** trees, the brief starts with an inventory (next section).

### Step 1 — Tokens

Write `design/tokens/*.tokens.json` — one file per tier, **self-nested under
the tier's own group** (`color.tokens.json` → top-level `"color"`; see the
format charter above). Every value that later artifacts reference (palette,
type scale, spacing steps, radii, motion) belongs here first, because tokens
are what screens and wireframes consume.

Write token files with a **structured-file tool** (`write_structured_file` /
`patch_structured_file`), never freehand text edits — token JSON is
machine-parsed by export, the brief, and the validator, and a hand-indented
rewrite ships valid-but-mangled formatting that survives all of them.

Then **validate**: `design_validate` (or the token path).

- Fix `error` findings: bad JSON, unknown `$type`, dangling/cyclic aliases.
- Resolve every alias you introduce — a reference to a token you have not
  written yet is an error today.
- Fix any `consistency_token_ref_dangling` finding by self-nesting the tier
  file (add the missing top-level group), never by rewriting the references
  in wireframes/components.
- If the validator emits the `.gitattributes` `diff=html` fix finding, apply
  it (append, never clobber an existing `.gitattributes`).

### Step 2 — Wireframes

Write `design/wireframes/<screen-name>.svg`, one file per screen, at the
briefed frame size. Structure first, styling later: wireframes are sketches,
so literal colors are allowed but tracked — prefer token references where a
value is already a token.

Then **validate**: `design_validate design/wireframes`.

- Every `data-nav` target must exist as a wireframe stem. If you point at a
  screen you have not drawn yet, draw it before moving on (or remove the nav).
- Keep text as `<text>`; keep interactive `id`s stable across edits — flows
  and feedback reference them.

### Step 3 — Flows

Write `design/flows/<flow-name>.mmd`. For screen flows, node ids are the
wireframe stems from Step 2 — this is the machine-checkable tie between the
graph and the screens.

Then **validate**: `design_validate design/flows`.

- Every screen-flow node id must have a matching wireframe. A node with no
  wireframe is an error; a wireframe with no flow node is an `info` (orphan —
  decide whether it belongs to a flow).
- Label edges with trigger semantics.

### Step 4 — Screens

Write `design/screens/<screen-name>.html` — hi-fi, self-contained, consuming
the tokens from Step 1. Root container width should match a declared frame.

Then **validate**: `design_validate`.

- No network `<script>`, no CDN references, no external fonts.
- Literal colors where a token exists are drift — replace with the token.

### Step 5 — Close the loop

- **Export, then sync**: after the artifacts are settled, run
  `design_export_tokens` so the theme matches the tokens (design-ahead), and if
  this turn also changed implementation code that consumes the design, end it
  with `design_sync` (code-ahead) so the tree adopts what the code learned. See
  **Sync — keep the tree truthful** below.
- Run the **critique-and-revise loop** (below) on the rendered output: write →
  `design_validate` → `design_critique` → fix → repeat, until the stopping
  rule holds (critique findings all `info` or explicitly accepted).
- Update `design/README.md`: add each new screen/flow with its purpose and a
  status marker (`draft` / `review` / `ready`), and update the manifest if it
  changed.
- Run a final whole-tree `design_validate`; accept `warn`/`info` findings
  explicitly (say why) or fix them.

## Greenfield scaffold sequence

Bootstrapping a `design/` tree from nothing. **Order matters** — each artifact
is referenced by the next, so validate as you go. The canonical order:

1. **`design/README.md` first.** Declare the device frames (`frames:` block),
   list the screens and flows you intend to build with purposes and initial
   `draft` status. This is the manifest every later step writes into and the
   file the agent reads first when `design/` exists. Nothing else can be
   validated against frames until frames are declared.
2. **`design/tokens/`** — one `*.tokens.json` per tier. Validate.
3. **`design/wireframes/`** — one SVG per screen at a declared frame. Validate
   (`data-nav` targets must resolve).
4. **`design/flows/`** — mermaid `.mmd`, node ids == the wireframe stems.
   Validate.
5. **`design/screens/`** — hi-fi HTML consuming tokens. Validate.
6. **`design/brand/brand.md`** + icons (`design/icons/`), as the brand is
   pinned down — palette entries reference tokens, never raw hex. Validate.
7. **`.gitattributes`** — append `design/**/*.svg diff=html` for readable
   markup diffs. The scaffold **appends**; user workspaces already carry rules
   that must not be clobbered. Add `design/.cache/` to `.gitignore` (render
   PNGs and critique scratch are not committed); do **not** auto-ignore
   `design/generated/` — committing generated artifacts is the project's
   choice.

The scaffold directories: `mkdir -p design/{tokens,brand,icons,wireframes,screens,flows,feedback}`.

**`design/runtime/` — the fixed screen-kit assets.** The scaffold copies these
into the tree (device chrome `chrome.css` + the base documents
`base/phone.html` / `base/desktop.html`); they are versioned, tool-owned
assets — referenced by screens, never hand-edited, and a re-scaffold never
overwrites a copy the project has pinned. If `design/runtime/` is missing on
an existing tree, copy the kit in (the scaffold's `ScaffoldRuntimeAssets` is
the byte source); do not author lookalikes. New screens start as a copy of a
base document saved as `design/screens/<stem>.html` — the base carries the
token/runtime reference lines, the device attribute, and the declared-states
attribute; what is marked fixed in its header stays, everything inside
`<body>` is authored.

If `design_validate` reports no `design/` directory, that is the scaffold cue
— `design_assets` will also return `{exists: false}` with this same sequence.

## Brownfield inventory

When `design/` may already exist, **inventory before writing anything**:

1. **Read `design/README.md`.** It is the manifest — declared frames, screen
   and flow purposes, status markers, and links. If it is stale, that itself
   is a finding to fix.
2. **Run `design_assets`.** It returns the real inventory as structured JSON:
   per-asset rows `{path, kind, name, status?, summary?}` (kinds: `manifest`,
   `token`, `brand`, `icon`, `wireframe`, `screen`, `flow`, `feedback`), token
   group counts, flow node/edge counts, the manifest summary, and current
   `design_validate` findings. This is the ground truth — never fabricate a
   tree.
   - Optional `path` narrows to a subtree; optional `format` filters by kind.
   - It also surfaces pending `design/feedback/*.json` targets with their
     annotation counts (`feedback.pending`), and drift direction
     (design-ahead vs code-ahead) where those specs have landed. A pending
     target starts a feedback round (see **Human feedback** below).
3. **Reconcile the inventory against the brief.** Note what exists, what is
   missing, and what is inconsistent (README listing a screen that is not on
   disk, a flow node with no wireframe, literal colors where tokens exist).
4. **Extend in the tree's own idiom.** Follow the existing tiers, naming, and
   structure. New screens/flows get added to the README. If the tree's
   conventions conflict with this skill, ask before restructuring — respect
   the existing work.
5. **Validate the whole tree** after the change and fix errors.

A workspace with no `design/` returns `{exists: false}` plus scaffold
guidance — that means greenfield, not broken.

## Critique-and-revise loop

A design iteration is not done because files were written; it is done when the
output has been *seen*, validated, and the tree reflects it. Run this loop:

1. **Write / edit** the artifact with normal file tools.
2. **Static check** — `design_validate` (whole tree, or a path for a focused
   change). Fix every `error`. Decide explicitly about `warn`/`info`. Do not
   start the visual pass while `error`s remain.
3. **Visual check** — `design_critique` renders the target(s) via
   `design_render`, attaches the images, and returns structured findings
   `{target, area, severity, note, suggestion}` in the critique vocabulary
   below. Scope the pass with the `rubric` argument — `consistency`,
   `accessibility`, `hierarchy`, or `all` (the default; a typo is rejected
   rather than silently narrowing) — and use `compare_to` to review a second
   screen as a delta (same spacing, labels, component shapes) instead of
   judging it in isolation. A whole-tree critique is capped at 20 screens with
   an explicit notice; run narrowed critiques (a subtree, a slug, a file) for
   the rest. When no vision tier is reachable it degrades to
   `design_validate`-style static findings marked `visual: false` instead of
   failing the turn. Reach for `design_render` directly when you want the
   rendered image without a rubric pass; use `analyze_ui_screenshot` for
   screenshots and local HTML you did not author.
4. **Fix**, then **repeat** from step 2 — a fix can break a different check,
   so the loop re-enters at the static pass.

**Stopping rule:** the loop stops when `design_validate` is clean of `error`
findings (every `warn`/`info` fixed or explicitly accepted, with a stated
reason) **and** the latest `design_critique` findings are all `info` or
explicitly accepted (say which and why). Critique findings carry their own
severity vocabulary — `blocker` (a user cannot complete the task), `major`
(clearly wrong but usable), `minor` (polish), `info` (an observation, not a
defect) — so fix every `blocker`/`major`/`minor` finding; only `info` may be
left standing, and only when you say why you accept it. Do not iterate for its
own sake — a specific finding is actionable; vague polish is noise and burns
vision calls.

Judge rendered output with the shared critique vocabulary so findings are
specific and comparable across iterations — report each as **area +
observation + concrete fix**:

- **Hierarchy** — is visual weight ordered by importance? One primary action
  per view; secondary content recedes.
- **Affordance** — does an element look like what it does? Interactive
  elements must read as interactive and be targetable.
- **Consistency** — same pattern for the same job across screens; same
  spacing, labels, component shapes.
- **Spacing rhythm** — spacing comes from a scale, not ad-hoc values; related
  things group, unrelated things separate.
- **Contrast** — text and controls clear their backgrounds; check muted text
  and disabled states, not just the happy path.

### Human feedback

`design/feedback/<target>.json` carries human annotations
(normalized 0–1 `at` coordinates, per-annotation `resolved`, top-level
`status` and `resolution`) per SP-140-4 §4d.

**Starting a feedback round.** When `design_assets` reports pending feedback
(`feedback.pending` — a target whose `status` is `changes-requested` **or**
that still has at least one annotation with `resolved: false`), the round
starts with `design_assets`, then a **read of each pending target's feedback
file** with the normal file tools (`read_file design/feedback/<target>.json`)
to learn its annotations. `design_assets` gives you the counts (`pendingCount`,
each row's `annotations`/`pending`); the file gives you the notes. Order the
work by the annotations themselves — `area` (the critique vocabulary: hierarchy
/ affordance / consistency / spacing / contrast / touch-targets) and `note`
name what to change.

**Closing a feedback round.** After addressing each annotation on the
annotated screen:

1. **Edit the screen/wireframe** the target names, then run the critique-and-
   revise loop above on it (the annotations are findings, not a licence to skip
   validation).
2. **Write the `resolution` note** (a summary of what changed) into the
   feedback file with `write_file`/`edit_file` and flip `status` off
   `changes-requested` (e.g. `resolved`). The DesignView pane marks each
   annotation `resolved` (SP-140-4 item 4.8); the agent closes the loop with the
   resolution note.
3. The feedback file and the edit that resolved it land in the same repository
   — `git log -p design/feedback/` is a durable *why* for every design decision,
   so keep the note specific (which annotations, what changed).

No dedicated feedback tool exists — this is the skill loop over the existing
file tools plus `design_assets`' pending report.

## Sync — keep the tree truthful

There is no handoff. `design/` is the *semantic* source of truth (tokens,
structure, flows, intent) and the implementation is the *rendered* truth; the
two co-evolve for the life of the project, and work starts from either side.
Three steps keep them in step, in **any order**:

1. **Export before UI work.** Run `design_export_tokens` so code consuming the
   theme reads the current tokens (`design/generated/` is generated — never
   hand-edit it). This is the design→code half of the loop.
2. **Brief whenever building a screen.** Before a dev turn builds a screen,
   assemble its contract from the tree — purpose, wireframe path, flows in/out
   with triggers, tokens to consume, open feedback, status. Read
   `design/README.md` + `design_assets` for it today; SP-140-5 §5g adds a
   `design_brief` tool that assembles the same contract, so once that tool
   ships, call it instead. The brief is advisory context, not generated code.
3. **`design_sync` after dev-side UI work.** A UI-affecting turn ends with
   `design_sync` the way a turn that edits code ends with tests: it reads the
   touched UI files and the tree and returns the semantic deltas code
   introduced, so `design/` can adopt them. Analyze first; `apply` writes the
   safe subset (literal token renames/revalues, structural wireframe/flow
   additions) under `design/` and leaves inferred deltas as proposals. Sync
   never rewrites the implementation — the semantic layer is *invited to
   adopt*, never enforced onto code.

Then **co-commit** the code change with its `design/` adoption so every point
in history is internally consistent.

### Co-commit — one commit carries both sides

The loop ends in **one commit**, not two:

- **Co-commit rule.** Stage the dev change and its `design_sync --apply`
  adoption **together** — one commit carries both. A commit that has only the
  code (or only the `design/` side) leaves history internally inconsistent, and
  a later `git revert` of that half would desync the loop; committed together,
  revert removes both sides at once and the tree stays truthful at every ref.
- **Commit type.** Design-led iterations use a **`design:`** Conventional-Commit
  type (an extension of the house convention, not a fork); dev-led iterations
  keep `feat:`/`fix:` and simply include the `design/` adoption in the
  **same commit**. History then reads as the loop's log — which iterations were
  design-led vs dev-led:

  ```
  design: refresh the check-deposit flow tokens
  feat: add CheckDeposit screen (+ design/ adoption)
  ```
- **Human edits from Design mode** (SP-140-7: token values, screen status,
  feedback pins) follow the same rule — fold them into the same commit as the
  agent turn they belong with, or commit them as their own `design:` iteration.
  Their edits are ordinary workspace-file writes: git history, blame, and
  revert treat them exactly like agent edits.

### Drift has a direction — two meanings of "stale"

Drift is signal, not guilt. `design_assets` and `design_validate` report both
directions as distinct rows with distinct remedies:

- **Design-ahead** — `design/` changed, generator/code behind. The healthy
  state of active work in progress: generated outputs are regenerable, so the
  remedy is to **regenerate** with `design_export_tokens` ("2 screens ahead of
  build").
- **Code-ahead** — implementation changed, semantic layer behind. This is the
  state **`design_sync` exists to fix**: run it to import the dev-side change
  into `design/` ("run `design_sync` to import 3 semantic deltas").

Neither is an error. When the two disagree, the semantic layer is invited to
adopt reality; implementation is never rewritten to match `design/`.

## Tooling recap

| Tool | Use |
|---|---|
| `design_assets` | Inventory the tree (structured JSON); reports pending `design/feedback` targets with annotation counts; reports drift direction (design-ahead / code-ahead) with remedies; detects `exists:false` + scaffold guidance |
| `design_validate` | Static convention checks — run between **every** step; fix all `error`s; reports drift direction alongside `design_assets` |
| `design_export_tokens` | Regenerate `design/generated/` from the tokens — the design→code half of the loop; remedy for **design-ahead** |
| `design_sync` | End a UI-affecting turn here: analyze (default) returns the semantic deltas code introduced; `apply` writes the safe subset into `design/` — the remedy for **code-ahead** |
| `design_critique` | Visual pass: renders the target(s), attaches images, returns rubric findings `{target, area, severity, note, suggestion}` with severity `blocker`/`major`/`minor`/`info`; `rubric` (`consistency`/`accessibility`/`hierarchy`/`all`) and `compare_to` scope it. Degrades to `visual: false` static findings without vision |
| `design_render` | Render a wireframe SVG / screen HTML / flow `.mmd` to an image for critique |
| `design_import_sketch` | Turn a whiteboard/paper/photo/reference image into a `design/` starting point |
| `analyze_ui_screenshot` | Vision analysis of screenshots / local HTML you did not author |

`design_import_sketch` is a thin gatekeeper: it attaches the image to the
vision tier and the *model* extracts structure and writes the
convention-compliant files. The tool does **not** write files, and it does not
validate — running `design_validate` and fixing findings is your job.

## Never

- Never introduce a proprietary format or a network dependency into an asset.
- Never put design-shaped files outside `design/`.
- Never treat a rendered/exported artifact as a source of truth — edit the
  source (SVG/HTML/`.mmd`/`.tokens.json`).
- Never declare a design done without a clean `design_validate` run (errors)
  and a README that reflects the tree — and, once you have rendered anything,
  a `design_critique` pass whose findings are all `info` or explicitly
  accepted.
- Never end a UI-affecting turn without `design_sync` — code that changed the
  design without the tree adopting it leaves `design/` stale (code-ahead).
