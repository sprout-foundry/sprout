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

`pkg/ast/grammars_embed.go` references five tree-sitter grammar blobs via
`//go:embed`. They are gitignored — the build copies them from the
gotreesitter module cache. Before opening the project in an IDE (gopls will
otherwise flag the embed line red), run:

```bash
make prepare-grammars
```

The Makefile targets (`build`, `build-wasm`, `test-unit`) depend on this
automatically, so it only needs to be run manually for IDE/editor support
on a fresh clone.

## Roadmap

Detailed roadmap specifications live in the `roadmap/` directory. Always read
these specifications first to ensure alignment with the project direction.

- **SP-001** Agent Core Architecture
- **SP-002** Configuration, Credentials & Providers
- **SP-003** Webui & Frontend Architecture
- **SP-004** Security, Validation & MCP
- **SP-005** Supporting Systems & Infrastructure
- **SP-006** Delegate Tool (proposed)
- **SP-007** Extend Configuration (proposed)
- **SP-008** Reliability Engineering (proposed)
- **SP-009** Component Library Maturation (proposed)
- **SP-010** Editor Modernization (proposed)
- **SP-011** Terminal Parity (proposed)
- **SP-012** UX Polish (proposed)
- **SP-013** Agent Settings Management (proposed)
- **SP-014** Agent Terminal Sessions (active)
- **SP-015** Cloud Platform Integration (partially implemented)
- **SP-016** Embedding Index — Duplicate Detection & Semantic Search (proposed)
- **SP-017** Settings Panel Rework (proposed)
- **SP-018** Memory System (implemented)
- **SP-019** Multi-Chat Sessions (implemented)
- **SP-020** Trace/Dataset Mode (implemented)
- **SP-021** Self-Review Tool (implemented)
- **SP-024** Context Management — File Read Optimization (proposed)
- **SP-049** Shell Permission Overhaul — Tiered Allow-Lists & User Policy (proposed)
- **SP-054** LSP Language Coverage Expansion (proposed)
- **SP-055** CLI Pinned Input — Always-On Steering Panel (proposed)
- **SP-056** Remove Static Embedding Provider (proposed)

## Testing

```bash
go test ./...                   # Run unit tests
python3 test_runner.py          # Run E2E tests
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

  **NEVER persist `api.TestClientType` ("test") to `LastUsedProvider` or `SubagentProvider`.** That string is an in-process sentinel for mock clients; if it reaches disk, the next CLI run picks it up and `/commit` (plus every chat) silently routes to a no-op mock. Five layers of defense exist to prevent this (see `pkg/configuration/testing_isolation.go`):
  1. `Save()` / `SaveToDir()` strip `"test"` from `LastUsedProvider` and `SubagentProvider`
  2. `Load()` / `LoadConfigWithLayers()` self-heal a poisoned file on read
  3. `NewManagerWithDir` no longer pre-writes `"test"` to fresh test configs
  4. `NewTestManager(t)` is the idiomatic helper for hermetic tests
  5. The helper's cleanup verifies the real config file is unchanged at test end and fails loudly if not
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

## Integration with Sprout Foundry

This repo (`sprout`) integrates with the [`sprout-foundry`](../sprout-foundry) repository. Both repos must stay in sync.

### 1. Binary Distribution

**How it works**:
- The `sprout` binary is distributed via `scripts/install.sh`
- sprout-foundry installs it in Docker images using `SPROUT_VERSION` build argument
- Version is tracked in sprout-foundry's `VERSION` file

**When to update version**:
- After releasing a new version (create GitHub release)
- When sprout-foundry needs new features/fixes
- Update `SPROUT_VERSION` in sprout-foundry's `VERSION` file

**Release process**:
```bash
# Create new release
git tag -a v1.3.0 -m "Release 1.3.0"
git push origin v1.3.0
# Create GitHub release with binary assets
```

### 2. NPM Packages

**Packages maintained here** (consumed by sprout-foundry):
- `packages/events` - Shared events transport (`@sprout/events`)
- `packages/ui` - Shared React components (`@sprout/ui`)

**Build requirements**:
- Packages must be built before sprout-foundry can use them
- Build command: `npm run build` (in package directory or root)
- Output: `dist/` directory with compiled JS and TypeScript definitions

**When updating packages**:
1. Make changes in `packages/`
2. Build: `npm run build`
3. Test in sprout-foundry: `cd ../sprout-foundry/webui && npm install`
4. Update version in `packages/*/package.json` if breaking changes
5. Document changes in `COMPATIBILITY.md` in sprout-foundry

**Version management**:
- Package versions are in `packages/*/package.json`
- sprout-foundry references via `file:../packages/...` paths
- Breaking changes require version bump and compatibility update

### 3. API Contracts

**Daemon API** (sprout runs in workspace containers):
- Port: 56000 (configurable via `--port` or `--web-port`)
- Health: `GET /health` → `{"status":"ok","port":56000,"uptime":"..."}`
- WebSocket: Terminal/editor sessions
- Environment variables: `SPROUT_BIND_ADDR`, `SPROUT_ALLOWED_ORIGINS`, `SPROUT_TRUSTED_USER_HEADER`

**Task Runner Output** (`sprout --output-json`):
```json
{
  "status": "success|error",
  "query": "original prompt",
  "error": "error message (if error)",
  "files_modified": ["file1", "file2"],
  "git_diff": "unified diff",
  "metrics": {
    "elapsed_seconds": 45.2,
    "tokens_in": 12500,
    "tokens_out": 3200,
    "llm_calls": 4,
    "provider": "anthropic",
    "model": "claude-sonnet-4-20250514"
  }
}
```

**When changing contracts**:
1. Update `COMPATIBILITY.md` in sprout-foundry
2. Bump version in `VERSION` file (sprout-foundry)
3. Test integration thoroughly
4. Document breaking changes

### 4. Testing Integration

**Test locally**:
```bash
# Build sprout binary
go build -o ~/bin/sprout ./cmd/sprout

# Test with sprout-foundry
cd ../sprout-foundry
make test-integration  # Requires sprout on PATH
```

**Verify package integration**:
```bash
# Build packages
cd packages/events && npm run build
cd ../ui && npm run build

# Test in sprout-foundry
cd ../sprout-foundry/webui
npm install
npm run build  # Should compile with @sprout/* packages
```

### 5. Common Integration Issues

**"Package dist/ not found"**:
- Build the package: `cd packages/<name> && npm run build`

**"Cannot find module '@sprout/*'"**:
- Check if packages are built
- Reinstall in sprout-foundry: `cd ../sprout-foundry/webui && npm install`

**Version mismatch**:
- Check sprout-foundry's `VERSION` file
- Ensure sprout version matches requirements in `COMPATIBILITY.md`

### Resources

- [Integration Contract Analysis](../sprout-foundry/docs/INTEGRATION_CONTRACT_ANALYSIS.md)
- [Compatibility Matrix](../sprout-foundry/COMPATIBILITY.md)
- [sprout-foundry AGENTS.md](../sprout-foundry/AGENTS.md)

### Testing with sprout-foundry

**Recommended**: Use Docker Compose in sprout-foundry repo
```bash
cd ../sprout-foundry
docker compose -f docker-compose.local.yml up --build -d

# Then run integration tests
make test-integration
```

**Manual**: If Docker is not available, ensure PostgreSQL is running and sprout binary is on PATH.
