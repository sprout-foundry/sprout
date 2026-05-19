# SP-039: UI Package Consolidation — One Canonical Component Library

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** MEDIUM (maintenance debt; silent drift risk)
**Depends on:** None
**Related:** SP-009 (Component Library Maturation), SP-010 (Editor Modernization), SP-012 (UX Polish)

## Problem

There are **two parallel React component implementations** in the repo:

- `packages/ui/src/components/` — declared as `@sprout/ui` v0.1.0; published as ESM + CJS; has Storybook stories and its own Jest config; designed for external consumption.
- `webui/src/components/` — the actual application. Imports `@sprout/ui` via the local symlink `file:../packages/ui`, but also re-implements many of the same components in-place.

### Concrete duplications

A `comm -12` between the two component directories returns **30+ overlapping filenames**, including:

| Component | In `packages/ui` | In `webui/src/components` | Risk |
|-----------|------------------|---------------------------|------|
| `ChatMessageContextMenu.tsx` | ✓ | ✓ | Behavior drift on right-click |
| `CommandPalette.css` | ✓ | ✓ | Visual drift (recent CSS edits per `git status` touched the webui copy) |
| `ContextMenu.tsx` | ✓ | ✓ | Foundational UI primitive; divergence cascades |
| `FileTree.tsx` | ✓ | ✓ | One of the most-used components |
| `GitSidebarPanel.tsx` | ✓ | ✓ | Recently modified in webui (commit `b46bcada`) — unclear if `packages/ui` was synced |
| `Sidebar.tsx`, `StatusBar.tsx`, `Terminal.tsx`, `Notification.css`, `MessageBubble.tsx`, `MessageSegments.tsx`, `QueuedMessagesPanel.tsx`, `SelectionActionBar.tsx`, `LiveLog.tsx`, … | ✓ | ✓ | All have both copies |

### Why this is bad

1. **No canonical answer to "where do I edit X?"** A bugfix to `FileTree.tsx` could land in `packages/ui` and never reach the app (or vice versa), depending on which one the bisecting engineer found first.
2. **CSS drift is already happening.** `git status` at the start of this conversation showed 15+ modified CSS files in both `packages/ui/src/components/` and `webui/src/components/`. The two sets are not synchronized — one has the recent changes the other doesn't.
3. **Imports are ambiguous.** Some `webui/src/...` files import from `@sprout/ui`, others from `./components/Terminal`. There is no enforced direction.
4. **Storybook lies.** Stories live in `packages/ui` and exercise the library's components, not the app's. A passing Storybook snapshot says nothing about whether the app is correct.
5. **The "extraction for reuse" thesis is unverified.** `@sprout/ui` is declared as a publishable package, but it is consumed only by the webui in this repo. No third party imports it. The complexity of maintaining two copies is being paid for hypothetical reuse that may never materialize.

### What "good" looks like

Either:
- **Option A — One package.** Delete `packages/ui`; move everything into `webui/src/components`. Simple, fast, no library surface to maintain.
- **Option B — Strict layering.** `packages/ui` is the canonical component library. `webui/src/components` contains **only** app-specific composites (BillingPage, EditorPane, Chat, etc.) — not base primitives. Every duplicate primitive is deleted from webui; webui imports from `@sprout/ui` exclusively.

This spec recommends **Option B** because Storybook + library hygiene are already paid-for and have value during development. But the recommendation hinges on whether `@sprout/ui` will *actually* be consumed by a second app (e.g., a future Foundry frontend, an Electron-specific shell, a docs site).

## Goals / Non-Goals

**Goals**
- Zero overlap between `packages/ui/src/components/` filenames and `webui/src/components/` filenames for primitives.
- Every primitive component (`Terminal`, `FileTree`, `Sidebar`, `ContextMenu`, `Notification`, `StatusBar`, `CommandPalette`, base `Chat*` parts) lives in **exactly one** place.
- An ESLint rule blocks new duplicates and enforces that `webui/src/components/` does not contain primitive components (only composites).
- A documented "what's a primitive vs a composite" decision rubric so the boundary doesn't blur again.

**Non-Goals**
- Designing a new design system or tokens layer (out of scope; could be SP-012's domain).
- Splitting `@sprout/ui` into multiple sub-packages.
- Rewriting components — moves and deletes only, no behavior change.
- Publishing `@sprout/ui` to npm (still file-symlinked).

## Current State

Component count audit (rough):
- `packages/ui/src/components/`: ~86 TSX files + matching `.css` / `.test.tsx` / `.stories.tsx`.
- `webui/src/components/`: ~214 TSX files including composites like `BillingPage`, `EditorPane`, `Chat`, `SettingsPanel`, plus duplicated primitives.

Overlap: ~30 components have both copies.

Recent activity: per `git status`, both directories have ~15 modified CSS files. The CSS edits were not synchronized — one directory has changes the other lacks.

## Proposed Solution

### Track A — Decide the model

A1. **Confirm Option B.** Validate the "extraction for reuse" thesis: is there a planned second consumer (Foundry frontend, Electron shell, docs site)? If yes → Option B; if no → Option A.

A2. **Document the decision** in `roadmap/SP-039-DECISION.md` (or inline at the top of this spec) so future readers understand why the consolidation went the chosen way.

A3. **Define the primitive vs composite rubric**:
  - **Primitive** = reusable in isolation, no app-specific business logic, no domain types (no `chatSession`, no `Persona`, no API calls). Examples: `Terminal`, `FileTree`, `ContextMenu`, `Notification`, `Dropdown`, `Modal`, `Sidebar` shell.
  - **Composite** = wires primitives to app state, calls API, holds domain data. Examples: `BillingPage`, `EditorPane`, `Chat` (top-level), `SettingsPanel`, `GitSidebarPanel` if it depends on the GitApi, otherwise primitive.

### Track B — Consolidate primitives (assuming Option B)

B1. **For each of the 30+ duplicates**, diff the two copies (`packages/ui/src/components/X.tsx` vs `webui/src/components/X.tsx`):
  - If identical → delete the webui copy, update imports.
  - If webui has additions → port the additions into `packages/ui` (the library copy is canonical); delete the webui copy; update imports.
  - If `packages/ui` is stale → port the webui changes back; delete webui; update imports.
  - If divergent in behavior → resolve via a design decision (which behavior is correct), then proceed.

B2. **Diff script** (`scripts/ui-consolidation-diff.sh`) that lists every overlap and the diff status (identical / packages-leads / webui-leads / divergent). Run before starting; use to plan the migration order.

B3. **Migration order**: smallest/simplest first (`Notification`, `Dropdown`, `Sidebar` shell), highest-risk last (`FileTree`, `Terminal`).

B4. **One commit per component.** Each consolidation is atomic. CI must stay green per commit.

### Track C — Move composites out of `@sprout/ui`

C1. **Audit `packages/ui/src/components/` for app-specific composites** that don't belong in a library:
  - `BillingPage`, `TeamPage`, `AdminBillingPage` — app pages; belong in webui.
  - `TasksPage` — app page; belongs in webui.
  - Any component that imports from `@sprout/events` for app-specific events or uses `useSproutAdapter()` against a specific endpoint set.

C2. **Move identified composites to `webui/src/components/`** with a single rename commit per component.

C3. **Verify `@sprout/ui` has no remaining domain coupling.** A grep for `chatSession`, `BillingProrationDisplay`, persona names, API endpoint strings → expect zero matches in `packages/ui/`.

### Track D — Enforce the boundary

D1. **ESLint rule** (custom or via `import/no-restricted-paths`): forbid imports from `webui/src/components/` to `@sprout/ui` source files (must use the package entry), and forbid `@sprout/ui` from importing `webui/src/` at all.

D2. **Forbid duplicate filenames.** Add a CI check (a small script in `scripts/`) that fails the build if `comm -12 <(ls packages/ui/src/components) <(ls webui/src/components)` has any matches.

D3. **Storybook becomes the canonical visual reference for primitives.** Ensure every primitive in `@sprout/ui` has a story. Add a CI check that fails on missing stories for any new primitive.

### Track E — Decision-rubric documentation

E1. **`docs/COMPONENT_LIBRARY.md`** — covers: the Option A/B decision and its rationale, the primitive vs composite rubric (with examples), the import direction enforcement, how to add a new component (decide tier first), how to migrate a composite when it needs to become a primitive (rare).

E2. **Update `packages/ui/README.md`** (if it exists) to point at `docs/COMPONENT_LIBRARY.md` as the source of truth.

E3. **Update `CONTRIBUTING.md`** — add a "Where does my new component go?" subsection.

## Implementation Phases

### Phase 1: Decision + audit
[ ] SP-039-1a: Confirm Option A or Option B based on planned second consumer. Document in this spec or `roadmap/SP-039-DECISION.md`.
[ ] SP-039-1b: Write `scripts/ui-consolidation-diff.sh` — outputs the 30+ overlaps and per-component diff status.
[ ] SP-039-1c: Categorize every `packages/ui/src/components/*.tsx` as primitive or composite.

### Phase 2: Move misplaced composites
[ ] SP-039-2a: Move `BillingPage*`, `TeamPage*`, `AdminBillingPage*`, `TasksPage*` from `packages/ui` to `webui/src/components/`. One commit per move.
[ ] SP-039-2b: Audit `packages/ui` for any other domain-coupled components; move them.
[ ] SP-039-2c: Verify `grep -rn "chatSession\|persona\|adapter" packages/ui/src/components/` returns no domain-specific hits.

### Phase 3: Consolidate primitives — small first
[ ] SP-039-3a: `Notification` (and `NotificationItem`, `Notification.css`) → canonical in `packages/ui`; delete webui copy; update imports.
[ ] SP-039-3b: `Dropdown`, `Modal` (base), `ContextMenu` → same.
[ ] SP-039-3c: `Sidebar`, `StatusBar`, `MenuBar` → same.
[ ] SP-039-3d: `CommandPalette`, `CommandInput` → same.

### Phase 4: Consolidate primitives — large
[ ] SP-039-4a: `FileTree` (highest-impact primitive; verify behavior parity with at least manual smoke test in WebUI).
[ ] SP-039-4b: `Terminal` (uses xterm.js; verify keybinding parity, reattach behavior, search bar).
[ ] SP-039-4c: `GitSidebarPanel` — confirm whether primitive or composite first (recent edits in `b46bcada` suggest composite behavior).
[ ] SP-039-4d: `MessageBubble`, `MessageSegments`, `MessageContent`, `LiveLog`, `QueuedMessagesPanel`, `SelectionActionBar`, `ChatMessageContextMenu`.

### Phase 5: Enforce boundary
[ ] SP-039-5a: Add `eslint-plugin-import` `no-restricted-paths` rule + config in `webui/.eslintrc` and `packages/ui/.eslintrc`.
[ ] SP-039-5b: Add `scripts/check-no-duplicate-components.sh`; wire into `.github/workflows/build.yml`.
[ ] SP-039-5c: Add a Storybook coverage check — every primitive in `@sprout/ui` must have a `.stories.tsx`.

### Phase 6: Documentation
[ ] SP-039-6a: Write `docs/COMPONENT_LIBRARY.md` (decision + rubric + add-new-component recipe).
[ ] SP-039-6b: Update `CONTRIBUTING.md` with "Where does my new component go?" section.
[ ] SP-039-6c: Update `packages/ui/README.md` to point at the new doc.

## Success Criteria

| Metric | Target |
|--------|--------|
| `comm -12` between `packages/ui/src/components/` and `webui/src/components/` | 0 overlaps |
| Domain types referenced in `packages/ui/src/components/` | 0 |
| Storybook stories for primitives | 100% |
| ESLint boundary rule | Passing |
| CI duplicate-filename check | Passing |

## Files Reference

| File | Action |
|------|--------|
| `packages/ui/src/components/*` | Modify: ~30 components consolidated; ~4-6 composites removed |
| `webui/src/components/*` | Modify: ~30 duplicates deleted; imports rewritten to `@sprout/ui` |
| `webui/.eslintrc` (or equivalent) | Modify: add `no-restricted-paths` rule |
| `packages/ui/.eslintrc` | Modify: same |
| `scripts/ui-consolidation-diff.sh` | Create: planning helper |
| `scripts/check-no-duplicate-components.sh` | Create: CI guard |
| `.github/workflows/build.yml` | Modify: wire in the duplicate-filename check |
| `docs/COMPONENT_LIBRARY.md` | Create |
| `CONTRIBUTING.md` | Modify: add component-tier subsection |
| `packages/ui/README.md` | Modify: link to component library doc |
| `roadmap/SP-039-DECISION.md` | Create (optional): record the A vs B choice and rationale |

## Risks

- **Behavior drift surfaces during consolidation.** Two divergent copies of `FileTree` likely have subtle behavior differences. Mitigation: each consolidation gets a manual smoke test in the WebUI plus the existing component tests must pass.
- **Storybook stories may break.** Stories may rely on patterns that the consolidated component no longer supports. Mitigation: stories update is part of the per-component commit.
- **Imports cascade.** Renaming a primitive's location changes import paths app-wide. Mitigation: TypeScript catches these at compile; `make build-all` is the gate.
- **Hidden cross-coupling.** A composite in webui may secretly import from `packages/ui/src/internal/...`. Mitigation: the ESLint rule catches it.
- **The wrong option chosen.** If Option B is picked but no second consumer ever materializes, the layering cost was wasted. Mitigation: revisit annually; the consolidation step makes either option cheap to flip later.
