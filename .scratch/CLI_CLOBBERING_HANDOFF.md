# HANDOFF: CLI Output Clobbering — "scattered characters" / "word-by-word deleting"

**Created:** 2026-06-29 13:47
**Status:** UNRESOLVED — user reports the issue persists after 5 fix commits + a rebuild.
**Branch:** `main` (clean except for working-tree changes, see below)

---

## 1. The Symptom

In the **interactive CLI (TTY mode)**, assistant prose output gets visually corrupted during/after streaming. Two manifestations:

1. **"Scattered characters"** — text characters land at random/wrong screen positions (dots, pipes, fragments scattered across rows). Read like the cursor is in the wrong place.
2. **"Word-by-word deleting"** — streamed prose appears to delete itself word by word as new words arrive, as if each new chunk overwrites/clears the previous line.

The user has rebuilt (`./sprout` is dated 13:44, newer than all source files) and confirms the issue **still happens**.

---

## 2. Root-Cause Hypotheses (5 distinct clobbering paths identified)

The console has **multiple independent writers** that all manipulate the TTY cursor. Any of them can clobber prose if the cursor math is off or if they race:

| # | Source | Mechanism | Status |
|---|--------|-----------|--------|
| A | `FinalizeAtTurnEnd` markdown re-render | Clears streamed segment via cursor-up + `\033[K`, re-emits ANSI-formatted version. Formatter changes visible line count (removes code fences, adds lang headers, collapses blanks) → row count mismatch → cursor stranded. | ✅ DISABLED in `7cb46d0d` |
| B | Chrome `writeTerminalMessage` | Was prefixed with `\r\033[K` which cleared the in-progress prose row ("word-by-word deleting"). | ✅ FIXED in `c9837e37` (prefix removed) |
| C | Status footer `Refresh()` | Uses DEC save/restore (`\0337`/`\0338`) + absolute positioning (`\033[%d;1H\033[K`). When `Refresh()` fires between prose chunks (e.g. on a `ToolEnd` event from the subscriber goroutine), the save/restore races with scroll-region content → cursor displacement. | ✅ FIXED in `0c007c3e` (`SetProseStreaming` gate → `Refresh()` is no-op during streaming) |
| D | Chrome routed through `streamingCallback` | Chrome output went through the same path as prose. | ✅ FIXED in `741b02bc` |
| E | **`ActivityIndicator` (spinner)** | Uses `\r\033[K` (render) and `\033[F\033[K` (ReplaceLastN walks up N lines clearing). Runs on a ticker goroutine. | ⚠️ **NOT ADDRESSED — prime remaining suspect** |

**The most likely remaining culprit is #E: the ActivityIndicator (spinner).** Its `render()` fires every `spinnerCadence` on a background goroutine and writes `\r\033[K<frame> <msg> (elapsed)` to the terminal. While it uses `TryLockOutput()`, the `\r\033[K` clears the current line — if prose is mid-stream on that line, the spinner clobbers it. `ReplaceLastN` is worse: it walks UP `n` lines with `\033[F\033[K`, which can walk up into prose rows.

---

## 3. Commits Applied Today (all 2026-06-29)

```
0c007c3e  13:25  fix(console): suppress footer refresh during prose streaming
e4c2a664  13:04  fix(console): disable status footer to eliminate cursor displacement
c9837e37  12:22  fix(console): remove \r\033[K from chrome output to stop TTY clobbering
7cb46d0d  12:06  fix(console): disable markdown re-render to eliminate CLI clobbering
741b02bc  11:51  fix(console): stop chrome from clobbering partial prose lines
```

### What each fix does

**`741b02bc`** — Stopped chrome (tool logs, agent messages) from routing through the `streamingCallback`. Chrome now goes directly to stdout.

**`7cb46d0d`** — Disabled the markdown re-render in `FinalizeAtTurnEnd()` entirely. The method now just calls `resetSegment()` without any clear-and-reprint dance. See `pkg/console/assistant_turn_renderer.go` lines ~291-310. **Trade-off: no ANSI syntax highlighting in CLI prose.** Comment in code says: if re-enabled, the formatter output MUST produce the exact same row count as the streamed segment.

**`c9837e37`** — Removed the `\r\033[K` prefix from `writeTerminalMessage` in `pkg/agent/output_router.go`. Chrome now goes directly to `os.Stdout` with no ANSI prefix.

**`e4c2a664`** — Temporarily disabled the footer by commenting out `footer.Start()`. **Superseded by `0c007c3e`.**

**`0c007c3e`** (latest) — Re-enabled the footer but added a prose-streaming gate:
- `StatusFooter.proseStreaming` flag (bool)
- `StatusFooter.SetProseStreaming(active bool)` — when true, `Refresh()` is a no-op (returns early before `draw()`)
- `AssistantTurnRenderer.SetFooter(f)` wires the footer
- On first `WriteChunk` of each segment → `SetProseStreaming(true)`
- On segment end (`resetSegment`, called by `OnExternalWrite` and `FinalizeAtTurnEnd`) → `SetProseStreaming(false)` + single `Refresh()` to catch up

---

## 4. Uncommitted Working-Tree Changes (in the binary, NOT committed)

The binary (13:44) is newer than these files, so they ARE compiled in, but they are not yet committed to git:

| File | Change | Purpose |
|------|--------|---------|
| `cmd/agent_terminal_subscriber.go` | Added `LockOutput()/UnlockOutput()` around `fmt.Fprintln(os.Stdout)` external writes + `r.OnExternalWriteRows(n)` calls to sync `physicalLines` | Prevent external writes from interleaving with prose; keep row accounting correct |
| `cmd/agent_tool_display.go` | Added `todoBlockRowCount()` helper (counts `\n` + 1) | Feeds `OnExternalWriteRows` with correct row count for multi-line todo blocks |
| `pkg/agent/agent_creation.go` + `pkg/agent/mock_provider_init.go` + `pkg/agent/mock_provider_init_js.go` | Extracted mock provider init into build-tagged files (`!js` / `js`) | Fixes WASM build (SP-087). **Unrelated to clobbering.** |
| `packages/ui/MessageContent.tsx` + `packages/ui/package.json` | Removed `remark-breaks` plugin | **Unrelated to clobbering** (webui markdown rendering change) |
| `TODO.md` | Marked SP-087-6 as done | Bookkeeping |

---

## 5. Architecture: The Multi-Writer Problem

The interactive CLI has **at least 5 concurrent writers** to the TTY, each with its own cursor model:

1. **`AssistantTurnRenderer.WriteChunk()`** — streams prose to stdout, indents lines, tracks `physicalLines` / `curLineRunes` for row accounting. Uses `LockOutput()`.
2. **`ActivityIndicator` (spinner)** — background goroutine, `render()` every ~100ms writes `\r\033[K<frame>`. Uses `TryLockOutput()`.
3. **`StatusFooter`** — event-driven `Refresh()`, uses DEC save/restore (`\0337`/`\0338`) + absolute positioning (`\033[%d;1H\033[K`). Now gated by `proseStreaming`.
4. **Terminal subscriber goroutine** (`cmd/agent_terminal_subscriber.go`) — writes tool logs, todo blocks, subagent progress to stdout. Now uses `LockOutput()`.
5. **`InputReader`** (`pkg/console/input_render.go`, `input_core.go`) — uses `\r\033[K` and `\033[<n>C` cursor positioning.

The `outputMu` mutex (`LockOutput`/`UnlockOutput`/`TryLockOutput` in `pkg/console/console_lock.go`) serializes writes, but it does **NOT** prevent cursor-positioning sequences from clobbering rows written by other writers — it only prevents byte interleaving. The `\033[K`, `\033[F`, `\0337`/`\0338`, and `\033[%d;1H` sequences manipulate the shared cursor position, and if the position is wrong (row math error) or stale (saved before a scroll), every subsequent write lands in the wrong place.

---

## 6. Key Files

| File | Role |
|------|------|
| `pkg/console/assistant_turn_renderer.go` | Prose streaming, indent, segment buffering, (disabled) markdown re-render |
| `pkg/console/status_footer.go` | Pinned cost/context/model footer. DEC save/restore + absolute positioning. `proseStreaming` gate. |
| `pkg/console/activity_indicator.go` | **Spinner.** `\r\033[K` render + `\033[F\033[K` ReplaceLastN. Prime remaining suspect. |
| `pkg/console/console_lock.go` | `outputMu` mutex — `LockOutput`/`UnlockOutput`/`TryLockOutput` |
| `pkg/console/input_render.go` / `input_core.go` | Input line rendering with cursor positioning |
| `cmd/agent_terminal_subscriber.go` | Terminal event subscriber — tool logs, todos, progress → stdout |
| `cmd/agent_mode_state.go` | Creates `AssistantTurnRenderer`, wires `SetFooter`, manages `currentTurnRenderer` atomic |
| `cmd/agent_modes.go` | Prose streaming callback (line ~681 references `currentTurnRenderer`) |
| `pkg/agent/output_router.go` | `writeTerminalMessage` — chrome output path (now no ANSI prefix) |

---

## 7. Current Verification Status

```
go build ./...                  → exit 0 ✅
GOOS=js GOARCH=wasm go build ./pkg/agent/...  → exit 0 ✅
go test ./pkg/console/... -count=1  → ok (0.545s) ✅
Binary (./sprout) timestamp: 13:44 — newer than all source files ✅
```

The user rebuilt and the binary is current. The issue persists.

---

## 8. Recommended Next Steps (for the next agent)

### Priority 1: Investigate the ActivityIndicator (spinner)
The spinner's `render()` loop writes `\r\033[K` every ~100ms. If prose is streaming on the same row (which it is — both write to stdout/the scroll region), the spinner's `\r\033[K` clears the in-progress prose line, then the next prose chunk rewrites it — producing the "word-by-word deleting" effect.

- **Check:** Is the spinner stopped during prose streaming? Look at `cmd/agent_modes.go` around the streaming callback — does `indicator.Stop()` get called before the first `WriteChunk`?
- **Check:** `ReplaceLastN` walks up `n` lines with `\033[F\033[K` — if `n` is wrong it can walk up into prose.
- **Possible fix:** Suppress the spinner during prose streaming (same pattern as `SetProseStreaming` on the footer), OR ensure `indicator.Stop()` is called before prose starts and only re-`Start()`ed after `FinalizeAtTurnEnd`.

### Priority 2: Verify the `proseStreaming` gate has no race window
- `SetProseStreaming(true)` is called on the first `WriteChunk` — but what if a footer `Refresh()` or spinner `render()` fires in the window between the renderer being created (`currentTurnRenderer.Store(r)`) and the first `WriteChunk`?
- The gate is per-segment. Between segments (after `OnExternalWrite`, before next `WriteChunk`), the footer can `Refresh()`. If prose resumes immediately, there could be a 1-frame window.

### Priority 3: Audit ALL external stdout writes for cursor manipulation
Search for any remaining `\r`, `\033[K`, `\033[F`, `\0337`, `\0338`, `\033[<n>A` in code paths that run during an active turn. The `input_render.go` and `select_list.go` paths are candidates.

### Priority 4: Consider whether the issue is terminal-specific
- What terminal is the user using? (iTerm2, Terminal.app, Alacritty, tmux, screen?) Terminal emulators differ in scroll-region + DEC save/restore behavior. tmux/screen wrappers are especially prone.
- Can the user reproduce in a bare terminal (no tmux)?

### Diagnostic approach
Add a `SPROUT_DEBUG_CONSOLE=1` env var that logs every cursor-manipulating write to stderr with a timestamp + caller. Capture a full session and correlate the clobbering frame with which writer fired. This would definitively isolate which writer is responsible.

---

## 9. Important Constraints (from AGENTS.md / memories)

- **Do NOT re-enable the markdown re-render** in `FinalizeAtTurnEnd` without ensuring the formatter output has the exact same row count as the streamed segment. The comment in `assistant_turn_renderer.go` (lines ~300-310) documents this.
- **Do NOT remove the staleness guards** in change tracking (`isFileStale`, `filterGitSourcedDeltas`) — they prevent mystery reversion incidents (see memory: `vision-rollback-root-cause`, `nested-git-root-cause`).
- **Build command:** `make build-all` (React UI + Go binary + embed). For just Go: `go build ./...`.
- **Test isolation:** Tests must use `configuration.NewTestManager(t)` and never persist `api.TestClientType` to config. See AGENTS.md "Test Isolation" section.
- **NEVER force push.** Never commit without explicit user request.
- The working-tree changes to `agent_creation.go` / `mock_provider_init*.go` fix the WASM build (SP-087) and are unrelated to the clobbering issue. Don't revert them.
