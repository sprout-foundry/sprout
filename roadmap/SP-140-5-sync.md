# SP-140-5 — Design↔Code Sync: Continuous Bidirectional Loop

> **Status (2026-09-22):** Shipped — merged to `main` via `fe93ae98a`
> (released in v0.18.12).
> Parent: [SP-140](./SP-140-design-workspace.md). Depends on SP-140-1 only
> for the core (token export); the sync and reconciliation parts want
> SP-140-4's consistency data.

## Premise: there is no handoff

The industry term "handoff" presumes design finishes, then code begins,
then design goes quiet. Sprout rejects that framing. The design system and
the implementation co-evolve for the life of the product: design leads to
dev and dev leads back to design. Competitors institutionalize the wall
because their users sit on opposite sides of it — the agent works both
sides, in one loop, on one set of files. This spec's mechanisms make the
loop bidirectional where SP-140-1…4 made it productive in the design
direction.

The asymmetry to preserve: `design/` is the *semantic* source of truth
(tokens, structure, flows, intent). Implementation is the *rendered*
truth and changes fastest. When they diverge, the default is to reconcile
the semantic layer — import what the dev turn learned — not to treat the
code as contamination.

## Problem

With the tree and agent surfaces in place, the loop still runs one way:
`design/` → generated theme → code. Three gaps break the return path:

1. **Dev changes are invisible to design.** An implementation turn that
   adjusts spacing, renames a component, or adds a screen produces no
   trace in `design/`. Next design turn re-reads a stale tree and
   "corrects" toward the past.
2. **No capture-the-change mechanism.** Even a willing agent has no
   seam for updating the semantic layer after dev work — no command
   that reads implementation and proposes token/wireframe/flow updates.
3. **The loop isn't in the definition of done.** Nothing in prompts or
   skills tells an agent doing UI work that `design/` is part of its
   job now.

## Design

### 5a. Token export — same as before

Unchanged from the previous draft: `design_export_tokens` (css/ts/
tailwind/swift/kotlin, deterministic byte-identical output, provenance
headers, refuses dirty alias graphs, targets `design/generated/`).
Export is still the design→code half; what's new is everything after it.

### 5b. Dev-turn awareness — `design_sync` tool

New agent tool, invoked at the end of a UI-affecting dev turn (or any
time by the agent or user):

- Args: optional `files` (the turn's touched code files; defaults to
  the turn's ChangeTracker set), `mode` (`analyze` | `apply`,
  default `analyze`).
- **Analyze mode** reads the touched UI code and the design tree, and
  returns a sync report: detected semantic deltas, each with the design
  files it would touch — `{delta, kind: token|wireframe|flow|feedback,
  design_files, confidence, basis: literal|structural|inferred}`.
  Three bases:
  - **literal** — a token variable was renamed/revalued in code;
    maps 1:1 to the DTCG entry.
  - **structural** — a new route/screen appeared in the router; a
    component was added under an existing screen; a nav target moved.
    Maps to wireframe/flow changes.
  - **inferred** — styling exists with no token counterpart (raw hex,
    magic spacing). Maps to a *proposed* new token or a proposal to
    switch to an existing one.
  - Confidence governs automation: literal deltas are safe to apply
    mechanically; inferred ones are proposals.
- **Apply mode** writes the semantic-layer updates for the report's
  safe subset (tokens renamed/revalued, wireframe attribute/sidecar
  updates, flow edge additions), marks inferred ones as proposals, and
  leaves the agent/user to resolve the rest via normal file edits. All
  writes are ordinary workspace file edits — ChangeTracker-visible,
  revertible.
- No AST/deep-analysis requirement in v1: the tool works from file
  diffs and the convention vocabulary (CSS vars, Tailwind classes,
  router files, component file names) rather than parsing source
  trees. Deep static analysis is explicitly deferred.
- "Defaults to the turn's ChangeTracker set": `ToolEnv` has no typed
  ChangeTracker accessor — the sanctioned seam is
  `env.ResolveToolFuncs().ListChanges` (string output, parsed). Name
  this in the handler rather than inventing new plumbing.
- Gate-1 `PrecheckFileAccess` on all touched paths; apply-mode writes
  are confined to `design/` (asserted by test).

### 5c. Drift direction — two different meanings of "stale"

Current spec (5c) treated staleness as a defect to eliminate. In the
loop model, drift is *signal*. Two states, reported separately:

- **Design-ahead** (`design/` changed, generated/code behind): expected
  healthy state of an active project. Generated outputs are regenerable
  (`design_export_tokens`); code catching up is normal work-in-progress.
  Surfaced as actionable ("regenerate theme; 2 screens ahead of build").
- **Code-ahead** (implementation changed, semantic layer behind): the
  state `design_sync` exists to fix. Surfaced with a run-me pointer
  ("run `design_sync` to import 3 semantic deltas").

`design_assets` and `design_validate` report both. The skill and
persona prompts explain the difference. The drift guard from the
previous draft survives, reinterpreted: it reports direction and
remedy, not guilt.

### 5d. Loop in the definition of done

Prompt/skill wiring (no machinery):

- The base prompts' UI-work guidance (extended by SP-140-2d's
  conditional block): a UI-affecting turn ends with `design_sync` the
  way a turn that edits code ends with tests.
- The `design-system` skill's handoff section is replaced by a
  **sync section**: export before UI work, `design_sync` after, brief
  (`design_brief`) whenever building a screen — in any order, since
  work starts from either side.
- The designer prompt drops "hand off to coding personas" for
  "developers read the tree; keep it truthful, run sync after your
  dev-side work."

### 5g. `design_brief` — the screen scaffold contract

New agent tool, invoked before a dev turn builds a screen (the
"brief whenever building a screen" step in §5d):

- Args: `screen_name` (wireframe stem), optional `depth`
  (`summary` | `full`, default `summary`).
- Reads the design tree for that screen — wireframe, flow edges touching
  it, tokens referenced, README purpose, pending feedback — and returns
  a structured brief: purpose, wireframe path, flows in/out with
  triggers, token paths to consume, open feedback annotations, status.
- Output is advisory context for the implementing agent (returned as
  text/JSON in the `ToolResult`); it writes no files. It is a contract,
  not a generator — no component code is produced (Non-goals).
- Gate-1 on any path resolution, per SP-140 invariant 7.

### 5f. Git: one commit carries both sides

Baked-in process, not convention-if-remembered:

- **Co-commit rule:** the skill's sync section ends with the co-commit
  — the dev change and its `design_sync --apply` adoption land in one
  commit, so every point in history is internally consistent and
  `git revert` never desyncs the loop.
- **Commit-type convention:** design-led iterations use a `design:`
  Conventional-Commit type (extension, not fork, of the house
  convention); dev-led iterations keep `feat:`/`fix:` and simply
  include the `design/` adoption in the same commit. The git history
  then *reads* as the loop's log: which iterations were design-led vs
  dev-led.
- **Provenance at any ref:** generated artifacts carry
  content-hash headers (5a), so any checkout — any commit, any
  branch, a PR diff — can verify design/code consistency offline with
  no database, no tags, no CI. The commit hash is the version pin
  between the disciplines; there is nothing else to keep in step.

### 5e. Reconciliation conflict rule

When a dev delta contradicts the semantic layer (raw hex in code vs.
token value, new screen with no wireframe), the rule is: the semantic
layer is *invited to adopt*, never auto-enforced onto code. `design_sync
--apply` writes design files; it never rewrites the implementation to
match `design/`. Enforcement lives in review (critique/consistency
checks), not in the sync tool. This keeps dev velocity untouched while
the design tree absorbs reality.

## Non-goals

- No two-way *mechanical* sync. `design_sync` propagates semantics;
  it does not regenerate code from design. Code remains written by
  agents/humans with normal tools; the semantic layer adopts what they
  did.
- No AST/deep static analysis in v1 (diff + convention-based only).
- No watch-mode generation, no build-step integration (unchanged).
- No component-code generation from wireframes (unchanged from the
  previous draft — `design_brief` is a contract, not a generator).

## Acceptance criteria

- [ ] Fixture: token rename in code → `design_sync` analyze reports a
      literal delta naming the DTCG file and entry; apply updates it;
      second run is a no-op.
- [ ] Fixture: new route + screen component in code → analyze reports
      structural deltas proposing a wireframe stem and flow edges;
      apply creates a skeleton wireframe + flow edge with `draft`
      status.
- [ ] Fixture: raw-hex styling in code → analyze reports an inferred
      delta as a *proposal* (new token or switch-to-existing); apply
      does not auto-create tokens without the literal/structural
      confidence bar.
- [ ] Drift direction: design-ahead and code-ahead states report
      distinct rows with distinct remedies in `design_assets` output.
- [ ] End-to-end loop: design turn (new token) → export → dev turn
      (component consumes + tweaks it) → `design_sync` → design tree
      reflects the tweak; next design turn builds on it, not against
      it.
- [ ] `design_sync --apply` never modifies files outside `design/`
      (test asserts the workspace diff is confined to `design/`).
- [ ] Co-commit: the skill-driven loop produces a single commit
      containing both the code change and the `design/` adoption;
      reverting that commit removes both (test on a fixture repo).
- [ ] Provenance offline check: at an arbitrary checkout, the hash in
      a generated artifact's header matches the token inputs at that
      commit (staleness is decidable from the tree alone).
- [ ] `go test ./...`, `make vet && make lint && make build-all` clean.
