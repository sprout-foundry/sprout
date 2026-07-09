# repo_map improvements

## Problem statement

`GenerateRepoMap` (`pkg/agent_tools/repo_map.go`) is severely underpowered for medium-to-large repos despite being positioned as the agent's primary codebase-overview tool.

Observed behavior (against a ~150-file React Native codebase):

- Returns 12 files (mostly root-level `.tsx`, `App.tsx`, `WordMark` SVGs, e2e tests)
- Misses `src/` entirely (~95% of application code: `contexts/`, `screens/`, `services/`, `components/`, `routes/`, `utils/`, `services/ble/`, `services/offlineDemoRoutes/`)
- The 1024-token / 200-file budget is too small to cover any repo with more than a few hundred source files
- Output is dominated by alphabetical sort + first-N-wins, so deep directories are silently dropped before they ever get a chance to appear

## Root cause (in code)

Two consts in `pkg/agent_tools/repo_map.go`:

```go
repoMapTokenBudget     = 1024            // target ~1024 tokens
repoMapMaxFiles        = 200             // max files to include
repoMapCharBudget      = repoMapTokenBudget * 4  // 4096 chars
```

Plus:

1. Files are sorted alphabetically then truncated to first 200 (`files = files[:repoMapMaxFiles]`), so deep `src/` files lose to root-level files with shorter paths.
2. Per-file section is appended greedily until char budget is exhausted; whatever didn't make the cut is invisible to the caller.
3. The two limits interact badly: even if we raise `repoMapMaxFiles`, the char budget still caps total output.
4. No prioritization by directory depth, file importance, or recency — a 5-line `index.ts` gets the same treatment as `services/api/client.ts`.
5. No summary stats (total file count, dirs covered, what was omitted) so the caller can't tell when they're seeing partial output.
6. Format is `### path\n- sym:line\n` — no type/kind column, no signatures, no way to disambiguate overloaded functions.

## Repro

Test on this repo's `webui/` directory (after pruning `node_modules`), or any RN app's `src/` directory with 50+ files.

Before-state output:
```
## repo_map: sprout
### App.tsx
- App:5
### configurations/WordMark.tsx
...
*... truncated (token budget reached)*
```
12 files, missing all of `src/`.

## Proposed improvements

### 1. Right-size the budget (low risk, high impact)

- Raise `repoMapCharBudget` to ~16k chars (~4k tokens) — still well within model context but covers an order of magnitude more code.
- Raise `repoMapMaxFiles` to ~2000 to remove the silent file-cap.
- Make both configurable via env (`SPROUT_REPO_MAP_CHAR_BUDGET`, `SPROUT_REPO_MAP_MAX_FILES`) so callers can tune per-call.

### 2. Prioritize by depth & importance

Replace the flat alphabetical sort with a depth-aware priority:

- Level 0 (root): always included — gives the caller the entry-point context.
- Level 1 (`pkg/`, `internal/`, `cmd/`, `src/`): always included, capped per-dir to avoid one directory hogging the budget.
- Level 2+: included proportionally, with per-directory caps so every top-level area gets representation.

Algorithm sketch:

```
priority := root > L1 dir > L2 file
round-robin across L1 dirs at each depth level
fall back to alphabetical within a dir
```

This guarantees that no single top-level directory (e.g., `node_modules`-style mega-dirs or one giant `pkg/`) can starve the rest.

### 3. Add summary header

Replace the bare `## repo_map: <name>` header with a summary block:

```
## repo_map: sprout
- total source files: 1847 (TS: 1203, Go: 644)
- dirs covered: 42 / 87 (45 omitted)
- budget: 16000 chars used / 16000 (100%)
- truncated: yes (28 files at depth >=3)
```

This makes partial output legible — callers can tell at a glance that they're seeing 30% of the repo.

### 4. Improve symbol format

Current: `- func run:10` (name + line)
Proposed: `- func run :pkg/util:10` (cleaner separation, easier to scan)

For TS/JS/Python, include class context: `- class App.componentDidMount :src/App.tsx:25` so callers can navigate nested members.

### 5. Optional: depth-targeted mode

Add a `depth` parameter so callers can request:

- `depth=1` — just the directory tree (cheap, always fits)
- `depth=2` — dirs + top-level files (default)
- `depth=3` — full sweep with the existing budget

This lets callers cheaply get a "what directories exist" answer without paying for symbol extraction.

### 6. Tests

- Unit tests for: budget enforcement, depth prioritization, dir caps, summary header format.
- Golden-file test on a fixture repo (small, ~50 files) that captures the expected output.
- Property test: any repo with N files where N < repoMapMaxFiles should produce N file sections.

## Files to touch

- `pkg/agent_tools/repo_map.go` — main implementation
- `pkg/agent_tools/repo_map_test.go` — new test file (currently doesn't exist)
- Possibly `pkg/agent_tools/all.go` — if the tool's schema needs new params (depth, budget override)
- `pkg/agent_tools/codegraph/` — if we want to read symbol data from the store instead of re-parsing (this is already wired up; check why it isn't being used)

## Out of scope (for this branch)

- Cross-file call graph (`get_callers` / `get_callees` are already tools but separate from `repo_map`)
- Adding new languages beyond the current set
- IDE integration / LSP surface

## Notes

- `GenerateRepoMap` is exposed as a tool via the agent tool registry; check `pkg/agent_tools/all.go` for the registration point.
- The codegraph store path is already plumbed in (`openGraphStore`, `formatRepoMapFromNodes`) — the bug may partly be that the store isn't being populated. Worth verifying before re-implementing parsing.
