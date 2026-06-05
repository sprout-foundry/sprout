# AGENTS.md

This file provides guidance to AI agents working on code in this repository.

## Subagent Execution Policy

**Always use serialized subagents, never parallel.** Use `run_subagent` for
delegated work. Do NOT use `run_parallel_subagents` — parallel execution has
caused file conflicts and build failures due to overlapping edits.

Run subagents sequentially: test after code, review after test, fix after review.

## Build Verification Requirement

**You MUST run `make build-all` after making any code changes.** This builds both the React UI (deployed into Go embed) and the Go binary. A successful build confirms:
- Frontend TypeScript compiles without errors
- React UI bundles successfully
- Go binary compiles and embeds the UI

Run it at the end of every implementation task, before reporting work as complete:

```bash
make build-all
```

### First-time setup after `git clone`

Run `make prepare-grammars` once to populate gitignored tree-sitter blobs (only needed for IDE/gopls; `make build/test-unit` does it automatically).

## Roadmap

Detailed specifications live in `roadmap/` as `SP-###.md` files. Before making changes that touch a roadmap area, `ls roadmap/` to discover the relevant spec and read it for direction. Treat the spec as authoritative when its scope overlaps your task.

## Testing

```bash
go test ./...                   # Run unit tests
python3 e2e_test_runner.py          # Run E2E tests
```

### Test Isolation

**Tests must never alter the working environment.** When writing or running tests, agents must ensure that test workflows do not leak side effects into the codebase, git state, or configuration.

**Concrete risks to avoid:**
- **Branch changes** — Tests that create or switch git branches can leave the repo on the wrong branch. Always clean up or run in isolated clones. A prior testing session accidentally created a `new-branch` that diverged from `main`, requiring manual cherry-picking to recover commits.
- **Config/env mutation** — Tests that set environment variables (e.g., `SPROUT_CONFIG`, `LEDIT_CONFIG`) can leak between test cases. Always scope env changes with `t.Setenv()` and set *both* `SPROUT_CONFIG` and `LEDIT_CONFIG` to the same temp dir.

  **Preferred pattern: `configuration.NewTestManager(t)`.** It does all of the above in one call and ships a cleanup hook that fails the test if the real config file gets touched (Layer 5 detector). Use it for any test that reads, writes, or mutates `configuration.Config`:

  ```go
  mgr, cleanup := configuration.NewTestManager(t)
  defer cleanup()
  // mutate via mgr.UpdateConfig(...) — never call configuration.Load() directly
  ```

  **NEVER persist `api.TestClientType` ("test") to `LastUsedProvider` or `SubagentProvider`.** That string is an in-process sentinel for mock clients; if it reaches disk, the next CLI run picks it up and `/commit` (plus every chat) silently routes to a no-op mock. Multiple layers of defense exist (see `pkg/configuration/testing_isolation.go`); `NewTestManager(t)` is the idiomatic helper and its cleanup verifies the real config file is unchanged at test end.
- **Uncommitted test artifacts** — Test files created during a session (e.g., `*_test.go` files exploring codebase structure) must not be left uncommitted in the working tree. Either commit them or remove them before finishing.

## Git Operations Policy

### Absolute Rules

**NEVER FORCE PUSH.** `git push --force`, `git push -f`, `git push --force-with-lease`, and any variant that rewrites remote history is **unconditionally forbidden**. A fast-forward push that drops remote commits has the same destructive effect as a force push — always verify the remote has no divergent commits before pushing.

**NEVER COMMIT OR PUSH CHANGES without an explicit user request.** Only the repository owner decides when to commit.

### Mandatory Pre-Push Safety Check

Before **every** `git push`, you MUST:

1. **Fetch remote state**: `git fetch origin <branch>`
2. **Check for remote-only commits**: `git log HEAD..FETCH_HEAD --oneline`
3. **If output is non-empty** (remote has commits you don't have):
   - You MUST merge those commits in first: `git merge FETCH_HEAD`
   - Resolve any conflicts (see Conflict Resolution below)
   - Build and test after merge: `make build-all`
   - Commit the merge, then push
4. **If output is empty** (fast-forward safe): proceed with push

**Never skip step 2.** Even if you expect the remote to be behind, verify it. A fast-forward push that discards remote commits is as destructive as `--force`.

### Staging Files

**Staging specific files is always allowed.** `git add <filepath>` may be used via `shell_command` by any persona. However, broad patterns (`git add .`, `git add -A`, `git add --all`) are always blocked — use the git tool with specific file paths instead.

### Committing and Pushing

**`orchestrator` git-write privileges**: When `AllowOrchestratorGitWrite=true` (the default for fresh installs), the `orchestrator` persona can stage files, commit (via the commit tool), and push without interactive approval. When the flag is `false`, all git-write operations require the git tool with explicit user approval. Operations that discard or alter history (checkout, restore, reset) always require the git tool pathway with explicit user approval, regardless of persona or flag.

### Active Change Set Isolation

When working on a specific task (e.g., a TODO item), you MUST respect other active changes in the working tree:

1. **Focus ONLY on your assigned task.** Do NOT modify, revert, or delete any other active changes that exist in the working tree or change sets.
2. **Do NOT run destructive git commands** (`git checkout`, `git restore`, `git reset`, `git stash drop`, etc.) that would alter existing staged or unstaged changes that are not yours.
3. **If a build or test fails** due to conflicts with OTHER unrelated changes (not caused by your current work): pause for 2 minutes, then retry. Repeat up to 3 times (total delay of up to 6 minutes).
4. **After 3 failed retries** due to external conflicts, stop and escalate to the user. Report the conflicting changes. Do NOT attempt to resolve other people's changes yourself.
5. **Pass these isolation rules verbatim** when delegating to subagents.

### Conflict Resolution

When a merge produces conflicts:

1. **Read both sides** — understand what HEAD (yours) and the remote (theirs) each changed. Use `git diff HEAD...MERGE_HEAD` or inspect conflict markers directly.
2. **Merge intentionally** — combine both sides' changes when they are additive (e.g., one side adds `ctx context.Context`, the other adds a new parameter; the correct merge keeps both).
3. **Never blindly pick one side** — do not resolve a conflict by simply choosing "ours" or "theirs" without understanding what is being discarded. Each `<<<<<<<`/`=======`/`>>>>>>>` block requires human-like reasoning about intent.
4. **Verify after resolving** — run `make build-all` and relevant tests to confirm the merge compiles and passes.
5. **Check for stray conflict markers** — after editing, search for `<<<<<<`, `======`, `>>>>>>` to confirm all markers are removed.

### Git Tool Pathways

| Operation | Tool | Approval |
|-----------|------|----------|
| `git status`, `git diff`, `git log`, `git show`, `git fetch` | `shell_command` | Always allowed |
| `git add <specific-file>` | `shell_command` | Always allowed |
| `git commit -m "..."` | `shell_command` (orchestrator + git-write flag) or commit tool | Per orchestrator git-write rules |
| `git push` | `shell_command` (after pre-push safety check) | Per orchestrator git-write rules |
| `git checkout`, `git switch`, `git restore`, `git reset` | Git tool only | Requires explicit user approval |
| `git push --force` (any variant) | **FORBIDDEN** | Never allowed |
| `git rebase` (onto remote) | **FORBIDDEN** | Use merge instead |

## Design System

The webui is built on a token-driven design system rooted in `webui/src/App.css`
(canonical definitions for both themes) and mirrored in
`packages/ui/.storybook/tokens.css` (for Storybook isolation). Every component
that ships visual style — webui-local or `@sprout/ui` shared — must honor these
rules so the UI theme-switches cleanly and stays brand-consistent.

### Token catalogue (don't reinvent these)

**Surfaces** — `--bg-primary`, `--bg-secondary`, `--bg-tertiary`,
`--bg-elevated`, `--bg-surface`, `--bg-hover` (alias for elevated).
**Status-tinted surfaces** — `--bg-error`, `--bg-success`, `--bg-warning`,
`--bg-info` (12% accent-tinted via color-mix; use for inline alert panels).
**Text** — `--text-primary`, `--text-secondary`, `--text-tertiary`, `--text-muted`.
**Accents** — `--accent-primary`, `--accent-secondary`, `--accent-success`,
`--accent-warning`, `--accent-error`, `--accent-info` (alias for primary).
**Accent foregrounds** — `--accent-fg` (white text on accent surfaces),
`--accent-warning-fg` (#1a1a2e dark text on amber/warning — white-on-yellow
fails contrast), `--accent-on-primary` (alias for `--accent-fg`).
**Borders** — `--border-subtle`, `--border-default`, `--border-strong`,
`--border-focus`.
**Brand** — `--brand-teal`, `--brand-frost`, `--brand-active-cyan`,
`--brand-navy` (sprout brand, not generic accents).
**Other** — `--radius-{sm,md,lg,xl}`, `--space-{1..12}`, `--text-{xs..3xl}`,
`--shadow-{subtle,elevated,float}`, `--font-{sans,mono}`, `--ease-{out,in-out}`.

### Hard rules

1. **No raw hex/rgba in CSS or inline `style={{}}`.** Use a token.
   - Pure black/white scrims (modal overlays at `rgba(0,0,0,0.5)`) are the
     only exception — they're theme-neutral by intent.
   - HTML preview iframes that need a true white background (so user HTML
     renders correctly) are also exempt.
   - File-type / language icons (JS yellow, TS blue, Go cyan) are intentional
     brand identifiers — leave them as literal hex.
2. **No hex fallbacks on defined tokens.** Write `var(--accent-primary)`,
   not `var(--accent-primary, #6366f1)`. The token is canonical; a
   wrong-color fallback (especially the indigo `#6366f1` Tailwind default)
   silently renders off-brand if anything goes sideways.
3. **Status-tinted backgrounds use `color-mix`, not literal rgba.**
   `color-mix(in srgb, var(--accent-error) 12%, transparent)` over
   `rgba(224, 108, 117, 0.12)` — the latter doesn't theme-switch.
4. **Text on a colored background uses the matching foreground token.**
   - On `--accent-primary|success|error|secondary` surfaces → `var(--accent-fg)`.
   - On `--accent-warning` surfaces → `var(--accent-warning-fg)` (dark navy,
     because white-on-amber fails WCAG contrast).
   - Don't use `var(--text-primary)` on accent backgrounds — it's tuned for
     `--bg-*` surfaces and will be low-contrast.
5. **Every interactive element gets `:focus-visible`.** Keyboard users must
   see where focus is. Pattern:
   ```css
   .button:focus-visible {
     outline: none;
     box-shadow: 0 0 0 2px var(--accent-primary);
   }
   ```
   For tab-strip / chrome items, use `outline: 2px solid var(--accent-primary);
   outline-offset: -2px` instead so the ring sits inside the tab bounds.
6. **Don't `@media (prefers-color-scheme: dark)` for theming.** This app
   toggles via `:root[data-theme='light']`. A `prefers-color-scheme` rule
   fights the user's explicit choice. The token system already theme-switches —
   write rules once using tokens.
7. **`transition: all` is an anti-pattern.** List the properties you actually
   animate (`background, color, border-color, box-shadow`). Animating `all`
   includes layout properties and can jank.
8. **No hardcoded white-alpha `rgba(255, 255, 255, X)` inset highlights**
   on themable surfaces. They become invisible in light theme. Use
   `color-mix(var(--accent-fg) X%, transparent)` if you need the highlight
   to theme-switch, or accept that it's a dark-theme-only flourish and put
   it inside a dark-theme guard.

### When you add a new token

Add it to **both**:
- `webui/src/App.css` — in both `:root` (dark) and `:root[data-theme='light']`
  (or define it once at `:root` if the value is theme-neutral, e.g.
  `--accent-fg: #ffffff`).
- `packages/ui/.storybook/tokens.css` — so shared-package Storybook still
  renders correctly.

If you don't add it to the storybook file, components in `@sprout/ui` that
use the new token will render unstyled in Storybook even though they look
fine in the live webui.

### When you touch shared-package CSS (`packages/ui/src/components/*.css`)

The shared package ships to both webui and sprout-foundry. Its tokens are
resolved by the **consumer's** stylesheet, not the package's own. Pattern:

- **Always use tokens.** Same rules as webui-local CSS.
- **Fallbacks are allowed and encouraged** here, since consumers may define
  tokens differently: `var(--accent-primary, #61afef)`. Use brand-correct
  fallbacks — `#61afef` (Atom-One blue) for primary, not `#6366f1` (indigo).
- **Don't duplicate webui CSS into the shared package.** If both `webui/src/
  components/Chat.css` and `packages/ui/src/components/ChatPanel.css` define
  `.chat-container`, the cascade order matters and drift creates ghosts.
  The webui-local copy should hold only webui-specific overrides; the shared
  package is the source of truth for base styles.

### Quick verification before declaring "done"

```bash
# Find raw hex/rgba leaks introduced in your branch
git diff origin/main -- 'webui/src/**/*.css' 'packages/ui/src/**/*.css' \
  | grep -E '^\+.*(#[0-9a-fA-F]{3,6}|rgba\([0-9])' \
  | grep -vE 'rgba\(0, 0, 0|var\(--'

# Find undefined token references in webui CSS
grep -rohE 'var\(--[a-z-]+' webui/src/components/ | sort -u \
  | while read tok; do
      name="${tok#var(--}"
      grep -q "^\s*--${name}:" webui/src/App.css || echo "UNDEFINED: $tok"
    done
```

Run `make build-all` after CSS changes — Lightning CSS will warn on
invalid syntax like unescaped `?` in selectors (real issue we hit) or
`color-mix()` with malformed percentage strings (also a real issue —
sed regex bug produced `252%` mid-refactor).

## Code Quality

- **File size target**: Under 500 lines per file
- **SRP**: Each type/file should have one primary responsibility
- **No code duplication**: Use existing utilities
- **Self-documenting code**: Descriptive names; comments only for "why"
- **Incremental refactoring**: Build after each extraction step

## Context Architecture (SP-066)

Conversation context is managed by **three distinct operations**, plus an
orthogonal embedding store. Conflating them is the recurring failure mode —
the design depends on each operation staying in its lane.

1. **Substitute** (free, automatic, every prompt build). seed's
   `BuildCheckpointCompactedMessages` walks the message list before each
   API call and replaces ranges covered by a `TurnCheckpoint` with that
   checkpoint's `Summary` / `ActionableSummary`. No LLM call; the
   summarization cost was paid once at checkpoint-record time. This is the
   default lever and should fire on every prompt build.
2. **Rollup** (one LLM call, amortized). Background worker at
   `pkg/agent/rollup.go`. When the count of `TurnCheckpoint`s at any
   `Level` exceeds `rollupTriggerCount` (with the `recentTurnsToPreserve`
   window protected at level 0), the worker folds `rollupSourceCount`
   oldest entries at that level into one `Level+1` checkpoint via the
   dedicated rollup prompt (`prompts/rollup_prompt.md`). The rolled-up
   checkpoint replaces its sources in `AgentState.TurnCheckpoints`. The
   LLM call happens once; the result is reused by every future
   substitution pass.
3. **Compact** (LLM call on raw history, explicit). The `/compact` slash
   command in `pkg/agent_commands/compact.go`. Whole-history LLM
   summarization that **wipes** the active checkpoint list and replaces
   the head of `messages[]` with one recap. Today's behavior preserved
   intentionally — this is the user's deliberate "collapse this
   conversation to one summary" hammer, paired with `/clear`. Under the
   substitution-first model it should be a rare power-user action, not
   a daily survival tool.

The **conversation store** (vector embeddings via
`pkg/agent/turn_embedding.go::EmbedAndStoreTurn` → `pkg/embedding/`) is
the **persistent memory layer** and is orthogonal to the three operations
above. Every summary (per-turn, rollup, `/compact` recap) is embedded at
creation time and **persists in the store regardless of subsequent
`TurnCheckpoint` list manipulation**. `/compact` wiping the active list
does NOT delete the embedded summaries — recall (SP-066 Phase 3) can
still surface them when the current turn asks about that material.

### Trigger

seed's chat loop triggers substitution + LLM-fall-through compaction
when the prompt exceeds `CompactionTriggerFraction × max_context_tokens`.
sprout computes the fraction in `pkg/agent/context_budget.go::
computeCompactionTriggerFraction` by subtracting conservative reservations
(15% response + 10% thinking + 5% tool I/O = 30%) so substitution fires
at 70% of the window instead of seed's hardcoded 0.85 default. The
reservation gates only the rare LLM-fall-through; substitution itself is
free and happens every prompt build regardless of headroom.

### Don't re-collapse these into one mechanism

A few specific anti-patterns to avoid:

- **Don't make `/compact` skip when checkpoints already exist.** The user
  invoked it explicitly; honor the wipe.
- **Don't gate substitution on headroom.** Substitute always. The
  reservation math is for the LLM-fall-through path, not substitution.
- **Don't treat the embedding store as ephemeral.** It survives `/compact`
  and rollups by design. Don't add code paths that clear it on
  checkpoint-list mutation.
- **Don't let rollups consume the recency window.** The most-recent
  `recentTurnsToPreserve` per-turn checkpoints stay at full fidelity so
  the active prompt has high-resolution recent context.

Files that touch this area: `pkg/agent/turn_checkpoints.go`,
`pkg/agent/rollup.go`, `pkg/agent/context_budget.go`,
`pkg/agent/seed_integration.go`, `pkg/agent_commands/compact.go`,
`pkg/agent/turn_embedding.go`. Treat SP-066 in `roadmap/` as the
authoritative design when modifying any of them.

## Change Tracking

The `ChangeTracker` (in `pkg/agent/change_tracking*.go`) records every
file mutation an agent (primary or subagent) performs during a session.
It powers three user-facing surfaces:

- `list_changes` — LLM tool returning the current manifest of created /
  modified / deleted files with a `recoverable` flag per entry.
- `recover_file` — LLM tool that restores a file to its captured
  pre-change state. Reverses edits, un-deletes deletes, and removes
  agent-created files.
- `SubagentReturn.FilesModified` — the subagent → primary handoff
  payload that tells the primary exactly which files the subagent
  edited. Authoritative; the primary should NOT revert files outside
  this list. A `[subagent files modified] … [/subagent files modified]`
  manifest header is also prepended to the subagent's stdout so the
  LLM can't miss it.

### What gets tracked

Three sources feed the tracker:

1. **Direct file-tool hooks**: `write_file`, `edit_file`,
   `patch_structured_file`, `write_structured_file` all call
   `TrackFileWrite` / `TrackFileEdit`. Original + new content captured
   verbatim.
2. **Shell-mutation snapshot diff**: a workspace walk runs around every
   `shell_command` invocation. Detects mutations from `sed -i`, `mv`,
   `rm`, `cp`, `tee`, `awk -i inplace`, build scripts, formatters,
   anything else that bypasses the structured tools. Original bytes
   captured before the shell runs so deleted files are recoverable
   even when they're untracked-by-git.
3. **Subagent rollup**: each subagent runs its own tracker; on
   completion the runner copies the tracker's changes into
   `SubagentResult.FileChanges`, which become the primary-visible
   `files_modified` manifest.

### Performance, safety filters, configuration

Read-only shell short-circuit, stat-based cache, walk budgets, symlink/binary/>1 MiB skips, skip-list (`.git`, `node_modules`, `dist`, …), and the `change_tracking` config block live in `pkg/agent/change_tracking*.go`. The defaults are tuned for typical workspaces; adjust via `config.json → change_tracking` if you hit a pathological repo.

### Subagent return contract

When a subagent finishes, its result JSON includes:

```json
{
  "status": "completed",
  "files_modified": [
    {"path": "/abs/path/foo.go", "op": "modified"},
    {"path": "/abs/path/new.go", "op": "created"},
    {"path": "/abs/path/old.go", "op": "deleted"}
  ],
  "stdout": "[subagent files modified]\nM /abs/path/foo.go\nA /abs/path/new.go\nD /abs/path/old.go\n[/subagent files modified]\n\n<subagent's final assistant message>"
}
```

The primary's `run_subagent` / `run_parallel_subagents` tool
descriptions explicitly state: **trust `files_modified`**. Do NOT
revert files outside that list. If the working tree contains diff you
don't recognize, check the subagent's manifest before treating it as
out-of-scope.

## Integration with Sprout Foundry

This repo's binary and packages (`@sprout/events`, `@sprout/ui`) are consumed by [`../sprout-foundry`](../sprout-foundry). Both repos must stay in sync.

### What's shared

- **`sprout` binary** — distributed via `scripts/install.sh`; sprout-foundry pins to a `SPROUT_VERSION` in its `VERSION` file and installs it in Docker images.
- **NPM packages** (`packages/events`, `packages/ui`) — sprout-foundry references via `file:../packages/...` paths and consumes their `dist/` outputs. Run `npm run build -w @sprout/events` / `-w @sprout/ui` after changes.
- **Daemon API** — port 56000 (configurable via `--port` / `--web-port`); env: `SPROUT_BIND_ADDR`, `SPROUT_ALLOWED_ORIGINS`, `SPROUT_TRUSTED_USER_HEADER`; `GET /health` returns `{"status":"ok","port":56000,"uptime":"…"}`; WebSocket for terminal/editor sessions.
- **Task Runner JSON contract** (`sprout --output-json`) — `{status, query, error?, files_modified, git_diff, metrics: {elapsed_seconds, tokens_in/out, llm_calls, provider, model}}`. Treat field names as a stable contract.

### When you change any of the above

1. Bump versions where appropriate (package.json for npm changes; `VERSION` for binary changes).
2. Update `../sprout-foundry/COMPATIBILITY.md`.
3. Run integration tests: `cd ../sprout-foundry && make test-integration` (requires sprout binary on PATH; Docker Compose at `docker-compose.local.yml` is the recommended setup).

### Resources

- [`../sprout-foundry/COMPATIBILITY.md`](../sprout-foundry/COMPATIBILITY.md) — version compatibility matrix
- [`../sprout-foundry/AGENTS.md`](../sprout-foundry/AGENTS.md) — sister-repo agent instructions
- [`../sprout-foundry/docs/INTEGRATION_CONTRACT_ANALYSIS.md`](../sprout-foundry/docs/INTEGRATION_CONTRACT_ANALYSIS.md) — full contract analysis
