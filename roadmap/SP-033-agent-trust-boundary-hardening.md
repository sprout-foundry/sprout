# SP-033: Agent Trust Boundary Hardening

**Status:** 📋 Proposed
**Date:** 2026-05-18
**Priority:** High (supply-chain + persisted-data hygiene; users today are unknowingly exposed)
**Depends on:** SP-032 (shares subprocess shutdown chain)
**Related:** SP-004 (Security, Validation & MCP), SP-018 (Memory System), SP-027 (Persistent Context)

## Problem

Sprout has three trust boundaries that aren't well-defended today:

1. **The project boundary.** Cloning a hostile repo can silently activate skill instructions that subtly redirect the agent. There's no notice, no allowlist, no signing.
2. **The disk boundary.** Tool outputs flow into runlogs, the change tracker, embeddings, and memory files unredacted. The change tracker writes at world-readable mode. There's no `sprout history clear` or equivalent — user prompts accrete forever.
3. **The subprocess boundary.** MCP servers restart without a cap. Headless Chromium leaks on panic/SIGKILL. Python interpreter calls have no timeout. SP-032 covers shutdown for the *daemon* case; this spec extends the same hygiene to interactive mode.

These aren't theoretical: a user who pastes an API key into a prompt today will find it in `.sprout/revisions/*` at `0644` and in `~/.config/sprout/embeddings/conversation_turns.jsonl` indefinitely.

## What's already good (don't change)

- `pkg/credentials/backend.go:97` writes `api_keys.json` with mode `0600` and atomic-rename.
- `pkg/configuration/custom_provider_registry.go:119,464,471,509,558` all use `0600`.
- `pkg/embedding/check.go:175` uses `0600` for temp files.
- `pkg/agent_tools/git_args_validate.go` blocks the dangerous-flag set comprehensively.
- `pkg/agent_tools/security_classifier.go:307-312` already recursively classifies `$()` and backtick substitutions, taking max risk.

The fixes below extend this baseline; they don't replace it.

## Current State (verified)

| # | Area | Where | What's wrong |
|---|------|-------|-------------|
| **Skill discovery** | `pkg/configuration/config.go:1666-1755` | Scans `.sprout/skills/` from `os.Getwd()` on every config load; sets `Enabled: true`; no print/warning, no allowlist, no source attribution beyond a metadata tag |
| **Skill content trust** | `pkg/agent/skills/<id>/SKILL.md` consumption | The body of any discovered skill becomes agent instructions; can advise on dangerous-arg use, file-treat-as-safe, behavioural overrides |
| **Change-tracker file mode** | `pkg/history/changetracker.go:461,481` | `filesystem.WriteFileWithDir(…, 0644)` — revisions storing original/new file contents are world-readable |
| **No redaction layer** | `pkg/agent/`, `pkg/logging/`, `pkg/embedding/` | No `redact()` pass. Grep for `redact|scrub|sanitize` in `pkg/agent/` returns zero hits in production code |
| **No history-clear command** | `cmd/` | `sprout history`, `sprout embeddings`, `sprout memories` have no `clear` / `purge` / `forget` subcommand |
| **MCP unbounded restart** | `pkg/mcp/client.go:147` | `c.restartCount++` increments forever; no exponential backoff, no give-up threshold |
| **Browser leak on panic** | `pkg/webcontent/browser_rod.go:1311` | `Close()` exists but not registered with `runtime.SetFinalizer` or `signal.Notify`; SIGKILL leaves Chromium orphans |
| **Python interpreter no timeout** | `pkg/pythonruntime/runtime.go:65` | `exec.Command(…)` (not `CommandContext`) — a hung interpreter blocks the agent indefinitely |
| **Audit log captures expanded command** | `pkg/agent_tools/shell.go` | Logs the substituted `bash -c "$(…)"` rather than the raw tool call. Incident analysis sees the symptom, not the cause |
| **No SECURITY.md** | `docs/` | The classifier's documented limitations (env-var expansion not resolved, heredoc not inspected, symlinks not followed) aren't surfaced to operators |

## Goals / Non-Goals

**Goals**
- A first-time user opening a cloned repo sees a clear notice listing the project's skills and is asked to allowlist them.
- Tool outputs visibly carry secret patterns out only after passing through a single redaction function. Persisted files are `0600`.
- A user has a single command to wipe their conversation history, embeddings, and memories.
- Long-lived subprocesses (MCP, Chromium, Python) never accumulate indefinitely; the agent caps restart attempts and reaps on exit.
- The codebase's existing security design is documented so users can reason about it.

**Non-Goals**
- Building a full secrets-scanning engine. We pattern-match the obvious shapes (AWS keys, GH tokens, `BEGIN … PRIVATE KEY`, `Authorization:` headers, `.env`-style assignments) and call it done. Deep secrets detection is a future spec.
- Signing or sandboxing skill content. The allowlist + notice combination is the practical fix; cryptographic signing is overkill for v1.
- Removing the regex-classifier limitations the header comment already documents (env-var expansion, heredoc content). Those are acknowledged design choices; documenting them is enough.
- Touching the daemon-mode shutdown path — that's SP-032. SP-033 covers interactive mode and shared cleanup code.

## Proposed Solution

### Track A — Skill discovery UX

#### A1: Notice on discovery
When `discoverProjectSkills()` finds one or more skills, print to stderr (and the WebUI) before the agent starts:

```
sprout: discovered 2 project-local skills in ./.sprout/skills/
  - "Inject Custom Workflow" (./.sprout/skills/inject/SKILL.md)
  - "Override Commit Style"  (./.sprout/skills/override-commit/SKILL.md)
These skills will alter sprout's behaviour. Approve with: sprout skills allow inject override-commit
Or run with --no-project-skills to skip.
```

#### A2: Per-workspace allowlist
- New file: `.sprout/allowed_skills` (one skill ID per line). Discovered skills not in the allowlist are loaded with `Enabled: false` and listed in the notice.
- New CLI: `sprout skills allow <id>...` / `sprout skills revoke <id>...`.
- `--no-project-skills` flag disables discovery entirely for one run.
- CI/non-interactive default: skills disabled unless allowlist is committed.

#### A3: Source attribution
Every loaded skill gets `Metadata["source"]` set to either `"builtin"`, `"project:<repo-root>"`, or `"user"`. The agent's system prompt declares the source so the model has the context.

### Track B — Redaction layer

#### B1: Shared `redact.Apply(s string) string`
- New package `pkg/redact/` with one entry point: `Apply([]byte) []byte` and `ApplyString(string) string`.
- Patterns to cover initially:
  - AWS access keys: `AKIA[0-9A-Z]{16}` and the secret-key shape
  - GitHub tokens: `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_` + 36 chars
  - Slack tokens: `xox[abprs]-…`
  - OpenAI/Anthropic-style keys: `sk-[A-Za-z0-9]{20,}`
  - `-----BEGIN [A-Z ]+PRIVATE KEY-----` … END block
  - HTTP `Authorization:` / `X-API-Key:` header lines
  - `.env`-style `KEY=value` where KEY matches `*_TOKEN|*_KEY|*_SECRET|*_PASSWORD`
- Replacement: `[REDACTED:<kind>]` so the log/embedding remains diff-able and the kind is recoverable.
- Public `MarkAllowed(pattern)` for tests that need raw access.

#### B2: Apply at the choke points
- `pkg/logging/request_logger.go` (the runlog path) — pipe HTTP bodies through `redact.Apply` before write
- `pkg/history/changetracker.go:461,481` — redact both `OriginalCode` and `NewCode` before writing the revision file *if* and only if the file path is *outside* the workspace root (revisions of workspace files should NOT be redacted — they're the actual file content; revisions of e.g. `~/.aws/credentials` should be)
- `pkg/agent/turn_checkpoints.go` (SP-027 hook) — redact `UserPrompt` and `ActionableSummary` before embedding
- `pkg/agent/memory_handlers.go` — redact before persisting

#### B3: File modes
- Change `pkg/history/changetracker.go:461,481` to `0600`.
- Audit `pkg/logging/` for runlog write mode; force to `0600` if not already.
- Audit `~/.config/sprout/embeddings/*.jsonl` write mode (from SP-027); force `0600`.

### Track C — Persisted-data lifecycle

#### C1: Clear commands
- `sprout history clear [--older-than 30d] [--workspace path]` — removes runlogs and change-tracker entries
- `sprout embeddings clear [--type conversation_turn|memory|code]` — removes JSONL records by type
- `sprout memories list` / `sprout memories rm <name>` (if not already present)
- All operations are confirmation-gated (`-y` skips).

#### C2: Retention defaults
- Add `RetentionDays int` to `PersistentContextConfig` (default 90 days for conversation_turns; 0 = forever).
- Background sweep on agent startup deletes expired entries.

#### C3: Document the data layout
Section in `docs/SECURITY.md` listing every file sprout persists, what it contains, its mode, and how to clear it.

### Track D — Subprocess hardening

#### D1: MCP restart cap
- `pkg/mcp/client.go:147` — track restarts in a sliding window. After 3 failures in 60s: exponential backoff (start 1s, double, max 5min). After 10 failures total in 24h: stop trying and surface a "MCP server <name> repeatedly failing; disabled" notice.

#### D2: Chromium cleanup on signal
- In the interactive-mode signal handler (and in `cmd/agent_modes.go` already-extended chain from SP-032 A1), call `webcontent.RodRenderer.Close()` if a renderer is alive.
- Also register a `runtime.SetFinalizer` defensive backstop on the renderer struct in `pkg/webcontent/browser_rod.go:1311`.

#### D3: Python timeout
- `pkg/pythonruntime/runtime.go:65` — `exec.Command(…)` → `exec.CommandContext(ctx, …)` with a parent context that has a sensible deadline (default 30s for `inspectInterpreter`, configurable for longer operations).

### Track E — Audit log integrity

#### E1: Preserve the raw tool call
At each tool-call dispatch site in `pkg/agent/tool_executor*.go`, log:
- The raw tool name + arguments JSON (before any substitution or expansion)
- The post-resolution executed command (current behaviour)
- The classification decision (`SecuritySafe`/`SecurityCaution`/`SecurityDangerous`)
- The user's approval decision (auto-approved by rule X, manually approved, denied)

All four go into the runlog as structured fields. Today only the executed command is captured.

### Track F — Documentation

#### F1: `docs/SECURITY.md`
Cover:
- Trust boundaries (project / disk / subprocess / network)
- The classifier's documented limitations (lift from the `security_classifier.go:12-25` header comment)
- File layout: `~/.sprout/`, `~/.config/sprout/`, `.sprout/` — what's stored where, with what perms
- How to clear persisted data
- The skill allowlist model
- The auth-token requirement when binding to non-local (from SP-032 B1)
- How to report a security issue (`SECURITY.md` at repo root)

## Implementation Phases

### Phase 1: Skill discovery UX (Day 1-3) — HIGH user-visible impact

- [ ] **SP-033-1a**: Add the discovery notice to `pkg/configuration/config.go:1690-1755`. Print to stderr; for WebUI, surface via a startup banner.
- [ ] **SP-033-1b**: Implement `.sprout/allowed_skills` allowlist read/write helpers.
- [ ] **SP-033-1c**: Wire allowlist into `discoverProjectSkills` — skills not in allowlist load with `Enabled: false`.
- [ ] **SP-033-1d**: New CLI commands `sprout skills allow|revoke|list` in `cmd/skills.go`.
- [ ] **SP-033-1e**: `--no-project-skills` flag on the agent command; default off when stdin is non-TTY (CI/script mode).
- [ ] **SP-033-1f**: Set `Metadata["source"]` correctly for builtin / project / user skills; surface in agent system prompt.

### Phase 2: Redaction + file modes (Day 4-6)

- [ ] **SP-033-2a**: Create `pkg/redact/redact.go` with the pattern set listed above; comprehensive unit tests.
- [ ] **SP-033-2b**: Apply at `pkg/logging/request_logger.go` (runlog HTTP body path).
- [ ] **SP-033-2c**: Apply at `pkg/agent/turn_checkpoints.go` before SP-027's `EmbedAndStoreTurn()`.
- [ ] **SP-033-2d**: Apply at `pkg/agent/memory_handlers.go` before file write.
- [ ] **SP-033-2e**: Conditional redaction at `pkg/history/changetracker.go:461,481` — only when revision target is outside workspace root.
- [ ] **SP-033-2f**: Change file modes `0644` → `0600` at `pkg/history/changetracker.go:461,481`. Audit all other `os.WriteFile(…0644)` sites in `pkg/logging/` and `pkg/embedding/` and tighten where the data is user-private.

### Phase 3: Lifecycle commands (Day 7-9)

- [ ] **SP-033-3a**: `sprout history clear [--older-than DURATION] [--workspace PATH]` in `cmd/history.go`.
- [ ] **SP-033-3b**: `sprout embeddings clear [--type TYPE]` in `cmd/embeddings.go`.
- [ ] **SP-033-3c**: Add `RetentionDays` to `PersistentContextConfig`; background sweep on startup.
- [ ] **SP-033-3d**: Confirmation prompt with `-y` bypass for all clear operations.

### Phase 4: Subprocess hardening (Day 10-12)

- [ ] **SP-033-4a**: MCP restart sliding-window backoff and 24h cap at `pkg/mcp/client.go:147`.
- [ ] **SP-033-4b**: Register `webcontent.RodRenderer.Close()` in interactive-mode signal handler; add `runtime.SetFinalizer` backstop in `pkg/webcontent/browser_rod.go:1311`.
- [ ] **SP-033-4c**: `pkg/pythonruntime/runtime.go:65` — switch `exec.Command` to `exec.CommandContext` with a 30s default deadline.

### Phase 5: Audit log + documentation (Day 13-15)

- [ ] **SP-033-5a**: Structured runlog entries — raw tool-call JSON + executed command + classification + approval decision. Wire at each dispatch site in `pkg/agent/tool_executor*.go`.
- [ ] **SP-033-5b**: Write `docs/SECURITY.md` per the F1 outline.
- [ ] **SP-033-5c**: Add `SECURITY.md` at repo root with a vuln-reporting contact and the link to `docs/SECURITY.md`.

## Success Criteria

| Metric | Target |
|--------|--------|
| Cloning a repo with `.sprout/skills/` and starting sprout | Skills are listed in a notice, not silently activated |
| `cat .sprout/revisions/*` from another local user | Permission denied (`0600`) |
| Paste an `AKIA…` key into a prompt; check `~/.config/sprout/embeddings/*.jsonl` | Stored as `[REDACTED:aws-access-key]` |
| MCP server crashing 50× in a row | Disabled after 10, agent prints notice |
| `kill -9` of the agent process | No headless-Chrome orphans (verified by `pgrep`) |
| `sprout history clear --older-than 30d` | Removes matching entries, prints summary |
| `docs/SECURITY.md` exists | Covers trust boundaries, data layout, classifier limitations |

## Risks

- **Redaction false-positives** mangle legitimate content. Mitigation: every pattern includes an opt-in `MarkAllowed` for specific test fixtures; ship with a permissive set first and tighten via telemetry / user reports.
- **Skill allowlist breaks existing workflows.** Users who *want* project skills will see new friction. Mitigation: default to "enabled with notice" on first-run interactive mode (one-time confirmation); only hard-deny in CI. Document the migration path in release notes.
- **Retention sweep deletes data the user wanted to keep.** Mitigation: default `RetentionDays=0` (= forever); the user opts in by setting a value. Add `--dry-run` to the clear commands.
- **`runtime.SetFinalizer` reliability** — Go finalizers aren't guaranteed to run. Mitigation: use them as the *backstop only*; primary cleanup remains the signal handler.

## Files Reference

| File | Action |
|------|--------|
| `pkg/configuration/config.go` | Modify: `discoverProjectSkills` (1666-1755) — add notice, allowlist filter, source attribution |
| `cmd/skills.go` | Create: `sprout skills allow|revoke|list` |
| `pkg/redact/redact.go` | Create: shared redaction pass |
| `pkg/logging/request_logger.go` | Modify: pipe HTTP bodies through `redact.Apply` |
| `pkg/history/changetracker.go` | Modify: lines 461,481 — mode `0600`; conditional redaction outside workspace |
| `pkg/agent/turn_checkpoints.go` | Modify: redact before SP-027 embedding |
| `pkg/agent/memory_handlers.go` | Modify: redact before write |
| `cmd/history.go`, `cmd/embeddings.go` | Create: `clear` subcommands |
| `pkg/configuration/config.go` (PersistentContextConfig) | Modify: add `RetentionDays` |
| `pkg/mcp/client.go` | Modify: line 147 — sliding-window backoff + 24h cap |
| `pkg/webcontent/browser_rod.go` | Modify: signal-handler integration; `SetFinalizer` backstop |
| `pkg/pythonruntime/runtime.go` | Modify: line 65 — `CommandContext` with deadline |
| `pkg/agent/tool_executor*.go` | Modify: structured runlog entries (raw call + executed + classification + approval) |
| `docs/SECURITY.md` | Create: trust-boundary + data-layout doc |
| `SECURITY.md` (repo root) | Create: vuln-reporting contact |
