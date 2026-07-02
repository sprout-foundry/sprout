# SP-014: Agent Terminal Sessions — Hidden PTY Routing + Background Mode

**Status:** ✅ Implemented (2026-06-14; hidden PTY routing, background mode, attach flow)

Agent `shell_command` calls used `os/exec.CommandContext()` — one-shot subprocesses with no PTY, no persistence, and no visibility. This spec routed agent commands through hidden PTY sessions managed by the existing `TerminalManager`, enabling long-running processes, scrollback replay, and promotion to visible terminal tabs. A `background` flag on `shell_command` enables fire-and-forget execution for dev servers and test watchers. CLI mode falls back to plain `os/exec` unchanged.

## Key decisions

- Sentinel-based output capture: command wrapped with `__SPROUT_DONE__:$?` sentinel, scanned from PTY output
- Session reuse: one hidden session per chat (not per command), preserving shell environment state across calls
- Hidden sessions share the 256 KB ring buffer; scrollback replays on attach
- CLI mode uses plain `os/exec` unchanged — routing is purely additive for WebUI mode
- Background sessions get a 2-hour cleanup timeout (vs 30 min for interactive)
- Hidden sessions scoped to chat session; not visible in terminal tab bar until promoted

## Artifacts

- code: `pkg/webui/terminal_agent_exec.go` — sentinel-based command execution via PTY
- code: `pkg/webui/api_agent_sessions.go` — REST endpoints (list, promote, output, kill)
- code: `pkg/webui/terminal_types.go` — hidden session fields + CreateHiddenSession method
- code: `webui/src/components/BackgroundTasks.tsx` — background tasks panel with status and attach
- code: `webui/src/services/api/terminalApi.ts` — agent session API calls
- code: `pkg/agent/tool_handlers_shell.go` — handle background mode + session_id queries

Full specification archived — see git history for original content.
