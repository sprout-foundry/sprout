# AGENTS.md

This file provides guidance to AI agents working on code in this repository.

## Workflow

- **Subagents**: serialized only. Use `run_subagent`, never `run_parallel_subagents` — parallel edits cause file conflicts and broken builds. Sequence: code → test → review → fix.
- **Build verification**: run `make build-all` after every code change (React UI + Go binary + embed). One canonical command, used everywhere.
- **First-time setup**: `make prepare-grammars` once after `git clone` (only needed for IDE/gopls; `make build`/`make test-unit` does it automatically).
- **Roadmap**: `roadmap/SP-###.md` files are authoritative for their area. `ls roadmap/` before touching one.

## Testing

```bash
go test ./...                   # unit tests
make test-smoke                 # smoke tests
```

See `docs/TESTING.md` for the full testing strategy.

### Test Isolation

Tests must never leak side effects into the codebase, git state, or config:

- **Branch changes** — never create/switch branches in tests; a prior session accidentally created `new-branch` that diverged from `main`.
- **Config/env mutation** — scope env changes with `t.Setenv()` and set *both* `SPROUT_CONFIG` and `LEDIT_CONFIG` to the same temp dir.
- **Preferred helper**: `configuration.NewTestManager(t)` does all of the above in one call and ships a cleanup hook that fails the test if the real config file gets touched. Use it for any test that reads, writes, or mutates `configuration.Config`.
- **NEVER persist `api.TestClientType` ("test")** to `LastUsedProvider` or `SubagentProvider` — that string is an in-process sentinel for mock clients; if it reaches disk, the next CLI run silently routes to a no-op mock. See `pkg/configuration/testing_isolation.go`.
- **Uncommitted test artifacts** — `*_test.go` files created during a session must be committed or removed, not left in the tree.

## Git Operations

### Rules

- **NEVER FORCE PUSH** in any variant (`--force`, `-f`, `--force-with-lease`). A fast-forward push that drops remote-only commits is equally destructive.
- **NEVER COMMIT OR PUSH** without an explicit user request. Only the repo owner decides when to commit.
- **Staging**: `git add <specific-file>` is fine; `git add .`/`-A`/`--all` is blocked.
- **`orchestrator` git-write**: this persona can stage, commit (via the commit tool), and push without interactive approval, governed by `CapabilityGitWrite`.
- **Local history ops** (`git checkout`, `restore`, `reset`, `stash`, `rebase`) are restorable via ChangeTracker (`list_changes`, `recover_file`, `revert_my_changes`) and may run via `shell_command`. Prefer scoped ops over whole-tree resets; use `recover_file` for agent edits (touches only files the agent changed).
- **Active change set isolation**: focus only on your assigned task. Don't revert other agents' / user's in-progress work. If a build fails due to *unrelated* in-tree changes, pause briefly and retry; if it keeps failing, escalate.
- **Commit messages are shell-safe in both tools.** The `commit` tool and the `git` tool's `commit` operation both write the message to a Go temp file (no shell involvement) before calling `git commit -F <path>`. Backticks, `$()`, `"`, and `!` in commit messages are no longer expanded. Always `git log -1 --format='%H%n%s%n%b'` to verify the message after committing.

### Pre-Push Safety Check

Before every `git push`:

1. `git fetch origin <branch>`
2. `git log HEAD..FETCH_HEAD --oneline`
3. If non-empty (remote has commits you don't): `git merge FETCH_HEAD` → resolve → `make build-all` → commit → push.
4. If empty: push.

**Never skip step 2.** Even if you expect the remote to be behind, verify.

### Conflict Resolution

1. Read both sides (`git diff HEAD...MERGE_HEAD` or inspect markers). Understand what each side discarded.
2. Merge intentionally — additive changes (e.g., one side adds `ctx`, the other adds a parameter) keep both.
3. Never blindly pick "ours" or "theirs" — each `<<<<<<<`/`=======`/`>>>>>>>` block needs human reasoning.
4. After resolving: `make build-all` + relevant tests.
5. Search for stray `<<<<<<`/`======`/`>>>>>>` markers.

## Security Risk Classification

Shell commands are classified by a heuristic (`pkg/agent_tools/security_classifier.go` + `shell_patterns.go`) on a Safe / Caution / Dangerous / Critical scale, folded onto Low/Medium/High/Critical by `pkg/agent/risk_assessment.go`. This gate decides auto-approve vs. prompt vs. block.

**Do NOT attempt an embedding-based classifier.** Tried and removed — embeddings conflate `rm -rf node_modules` and `rm -rf /etc` (nearly identical vectors, opposite risk). A tokenizing command parser is the correct tool; if revisiting accuracy, fix the catch-all CAUTION default, not the classifier architecture.

## Design System

The webui uses a token-driven design system rooted in `webui/src/App.css` (canonical, both themes) and mirrored in `packages/ui/.storybook/tokens.css` (Storybook isolation). Every component that ships visual style — webui-local or `@sprout/ui` shared — must honor these rules so the UI theme-switches cleanly and stays brand-consistent.

### Token catalogue

**Surfaces** — `--bg-primary`, `--bg-secondary`, `--bg-tertiary`, `--bg-elevated`, `--bg-surface`, `--bg-hover` (alias for elevated).
**Status-tinted surfaces** — `--bg-error`, `--bg-success`, `--bg-warning`, `--bg-info` (12% accent-tinted via `color-mix`; use for inline alert panels).
**Text** — `--text-primary`, `--text-secondary`, `--text-tertiary`, `--text-muted`.
**Accents** — `--accent-primary`, `--accent-secondary`, `--accent-success`, `--accent-warning`, `--accent-error`, `--accent-info` (alias for primary).
**Accent foregrounds** — `--accent-fg` (white on accent surfaces), `--accent-warning-fg` (#1a1a2e dark text on amber — white-on-yellow fails contrast), `--accent-on-primary` (alias for `--accent-fg`).
**Borders** — `--border-subtle`, `--border-default`, `--border-strong`, `--border-focus`.
**Brand** — `--brand-teal`, `--brand-frost`, `--brand-active-cyan`, `--brand-navy` (sprout brand, not generic accents).
**Other** — `--radius-{sm,md,lg,xl}`, `--space-{1..12}`, `--text-{xs..3xl}`, `--shadow-{subtle,elevated,float}`, `--font-{sans,mono}`, `--ease-{out,in-out}`.

### Hard rules

1. **No raw hex/rgba in CSS or inline `style={{}}`.** Use a token. Exceptions: pure black/white scrims (`rgba(0,0,0,0.5)` modal overlays — theme-neutral by intent), HTML preview iframes needing true white, and file-type/language icons (JS yellow, TS blue, Go cyan) which are intentional brand identifiers.
2. **No hex fallbacks on defined tokens.** Write `var(--accent-primary)`, not `var(--accent-primary, #6366f1)`. A wrong-color fallback (especially the indigo Tailwind default) silently renders off-brand.
3. **Status-tinted backgrounds use `color-mix`, not literal rgba.** `color-mix(in srgb, var(--accent-error) 12%, transparent)` over `rgba(224, 108, 117, 0.12)` — the latter doesn't theme-switch.
4. **Text on a colored background uses the matching foreground token.** On `--accent-primary|success|error|secondary` → `var(--accent-fg)`. On `--accent-warning` → `var(--accent-warning-fg)` (white-on-amber fails WCAG). Never `var(--text-primary)` on accent backgrounds (tuned for `--bg-*`).
5. **Every interactive element gets `:focus-visible`.** Pattern: `outline: none; box-shadow: 0 0 0 2px var(--accent-primary)`. For tab-strip / chrome items, use `outline: 2px solid var(--accent-primary); outline-offset: -2px` so the ring sits inside the tab bounds.
6. **Don't `@media (prefers-color-scheme: dark)` for theming.** This app toggles via `:root[data-theme='light']`; a `prefers-color-scheme` rule fights the user's explicit choice.
7. **`transition: all` is an anti-pattern.** List the properties you actually animate (`background, color, border-color, box-shadow`). Animating `all` includes layout properties and can jank.
8. **No hardcoded white-alpha `rgba(255, 255, 255, X)` inset highlights** on themable surfaces (invisible in light theme). Use `color-mix(var(--accent-fg) X%, transparent)` if it must theme-switch, or guard it for dark theme only.

### Adding a new token

Add to **both** `webui/src/App.css` (both `:root` and `:root[data-theme='light']`, or once at `:root` if theme-neutral) and `packages/ui/.storybook/tokens.css`. Skip the storybook file and shared-package components render unstyled in Storybook even though they look fine in the live webui.

### Shared-package CSS (`packages/ui/src/components/*.css`)

Tokens are resolved by the **consumer's** stylesheet, not the package's own:

- Always use tokens; fallbacks are allowed and encouraged here (`var(--accent-primary, #61afef)` — Atom-One blue, not `#6366f1` indigo).
- Don't duplicate webui CSS into the shared package. The webui-local copy holds only webui-specific overrides; the shared package is the source of truth for base styles.

### Verification before "done"

```bash
# Raw hex/rgba leaks introduced in your branch
git diff origin/main -- 'webui/src/**/*.css' 'packages/ui/src/**/*.css' \
  | grep -E '^\+.*(#[0-9a-fA-F]{3,6}|rgba\([0-9])' \
  | grep -vE 'rgba\(0, 0, 0|var\(--'

# Undefined token references in webui CSS
grep -rohE 'var\(--[a-z-]+' webui/src/components/ | sort -u \
  | while read tok; do
      name="${tok#var(--}"
      grep -q "^\s*--${name}:" webui/src/App.css || echo "UNDEFINED: $tok"
    done
```

`make build-all` after CSS changes — Lightning CSS warns on invalid syntax like unescaped `?` in selectors or malformed `color-mix()` percentage strings.

## Code Conventions

**Self-documenting code is the default.** The default is *no comment*. Code reads itself when names and structure explain intent. Add a comment only when it explains a "why" the compiler can't enforce (non-obvious invariants, upstream-bug workarounds, security-sensitive rationale, why-not-the-obvious-approach). Never restate what the next line obviously does.

For everything else, read the canonical sources before making changes:

- `CONTRIBUTING.md` — development setup, branch/commit conventions, PR process, reviewer checklist
- `docs/TESTING.md` — testing layers, coverage thresholds
- `docs/ARCHITECTURE.md` — system architecture
- `docs/PERSONAS.md` — agent persona system, risk cascade, allowed tools
- `docs/SECURITY.md` — security model, tool call classification
- `.editorconfig` — indentation (2 spaces default, 4 for Go, tabs for Makefile)

The rules below are the ones agents trip over most often:

- **File size**: Under 500 lines per file. Split before you exceed it.
- **Single responsibility**: Each type/file has one primary concern.
- **No duplication**: Use existing utilities before writing new ones. If a helper is missing, check `pkg/` first.
- **Go errors**: `fmt.Errorf("doing X: %w", err)` — wrap at boundaries, return raw errors at the source.
- **Commits**: Conventional Commits (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`).
- **Branches**: `feature/`, `fix/`, `docs/`, `refactor/` prefixes.

## Context Architecture (SP-066)

Conversation context is managed by **three distinct operations**, plus an orthogonal embedding store. Conflating them is the recurring failure mode — the design depends on each operation staying in its lane. Full design: `roadmap/_completed/SP-066-never-ending-context.md`.

1. **Substitute** (free, every prompt build) — `BuildCheckpointCompactedMessages` replaces ranges covered by a `TurnCheckpoint` with its summary. No LLM call.
2. **Rollup** (one LLM call, amortized) — background worker folds oldest `Level`-N checkpoints into one `Level+1` once count exceeds `rollupTriggerCount` (protecting `recentTurnsToPreserve` at level 0).
3. **Compact** (LLM call on raw history, explicit) — `/compact` slash command wipes the active checkpoint list and replaces the head of `messages[]` with one recap. User-initiated hammer, paired with `/clear`.

The **conversation store** (vector embeddings via `pkg/agent/turn_embedding.go` → `pkg/embedding/`) is the persistent memory layer — orthogonal to the three operations. Every summary persists in the store regardless of subsequent `TurnCheckpoint` list manipulation. `/compact` wiping the active list does NOT delete embedded summaries; recall (SP-066 Phase 3) can still surface them.

### Don't re-collapse these into one mechanism

- `/compact` must not skip when checkpoints exist — the user invoked it explicitly; honor the wipe.
- Don't gate substitution on headroom — substitute always; the reservation math is for the LLM-fall-through path.
- Don't treat the embedding store as ephemeral — it survives `/compact` and rollups by design.
- Don't let rollups consume the recency window — `recentTurnsToPreserve` per-turn checkpoints stay at full fidelity.

## Change Tracking

The `ChangeTracker` (in `pkg/agent/change_tracking*.go`) records every file mutation an agent (primary or subagent) performs during a session. It powers `list_changes`, `recover_file`, and `SubagentReturn.FilesModified` — the authoritative manifest of what a subagent edited.

**Trust `files_modified`.** Do NOT revert files outside that list. If the working tree contains diff you don't recognize, check the subagent's manifest before treating it as out-of-scope.

**Git is authoritative for committed content (SP-077).** The ChangeTracker defers to git for content that is committed at HEAD. Two mechanisms prevent committed work from being silently reverted:
- **Phase 1 (filter):** `filterGitSourcedDeltas` in `TrackShellTurn` suppresses working-tree deltas whose post-operation content matches HEAD. These deltas are caused by git operations (merge, checkout, reset, pull) — not agent edits — and recording their stale pre-operation bytes as recoverable `OriginalCode` caused recurring data-loss incidents.
- **Phase 2 (sweep):** `sweepCommittedSnapshots` in `Commit()` marks persisted snapshots as `"superseded"` when their `NewCode` matches HEAD, preventing old snapshots from prior sessions from being reverted.

## Integration with Sprout Foundry

This repo's binary and packages (`@sprout/events`, `@sprout/ui`) are consumed by [`../sprout-foundry`](../sprout-foundry). Both repos must stay in sync.

- **`sprout` binary** — distributed via `scripts/install.sh`; sprout-foundry pins to a `SPROUT_VERSION` in its `VERSION` file and installs it in Docker images.
- **NPM packages** (`packages/events`, `packages/ui`) — sprout-foundry references via `file:../packages/...` paths and consumes their `dist/` outputs. Run `npm run build -w @sprout/events` / `-w @sprout/ui` after changes.
- **Daemon API** — port 56000 (configurable via `--port` / `--web-port`); env: `SPROUT_BIND_ADDR`, `SPROUT_ALLOWED_ORIGINS`, `SPROUT_TRUSTED_USER_HEADER`; `GET /health` returns `{"status":"ok","port":56000,"uptime":"…"}`; WebSocket for terminal/editor sessions.
- **Task Runner JSON contract** (`sprout --output-json`) — `{status, query, error?, files_modified, git_diff, metrics: {elapsed_seconds, tokens_in/out, llm_calls, provider, model}}`. Treat field names as a stable contract.

When you change any of the above: (1) bump versions (`package.json` for npm, `VERSION` for binary), (2) update `../sprout-foundry/COMPATIBILITY.md`, (3) run `cd ../sprout-foundry && make test-integration` (requires sprout binary on PATH; Docker Compose at `docker-compose.local.yml` is the recommended setup).

See `../sprout-foundry/AGENTS.md` for the sister-repo's conventions, and `../sprout-foundry/docs/INTEGRATION_CONTRACT_ANALYSIS.md` for the full contract analysis.