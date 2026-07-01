# SP-014: Agent Terminal Sessions — Hidden PTY Routing + Background Mode

**Status:** ✅ Implemented (Hidden PTY routing + background mode shipped)
**Depends on:** SP-001 (Agent Core Architecture), SP-003 (WebUI & Frontend Architecture)  
**Priority:** High  
**Effort Estimate:** ~2-3 weeks (3 phases)

## Problem

Agent `shell_command` tool calls use `os/exec.CommandContext()` — one-shot subprocesses with no PTY, no persistence, and no visibility. This creates three fundamental gaps:

1. **No auditability** — Commands leave no visible trace in the terminal. Users cannot see what ran or debug failures by inspecting the terminal.
2. **No long-running process support** — If the agent starts a dev server, test watcher, or any persistent process, the output is lost after `CombinedOutput()` returns or times out. There is no way to re-attach to inspect the running process.
3. **No interactive attachment** — The WebUI interactive terminal (`TerminalManager` PTY sessions via WebSocket) and agent command execution are completely independent subsystems that never interact. A process started by the agent cannot be promoted to a user-visible terminal tab.

### Current Architecture

```
Agent shell_command → os/exec.CommandContext(ctx, shell, "-c", command)
                    → cmd.CombinedOutput() → one-shot subprocess, no PTY

WebUI terminal → TerminalManager PTY sessions → WebSocket → persistent shell (separate)
```

The two paths share only the `workspaceRoot` from `webClientContext`. The agent never touches the `TerminalManager`.

## Proposed Solution

Route agent shell commands through **hidden PTY sessions** managed by the existing `TerminalManager`. Hidden sessions are tagged with metadata (`Hidden`, `Owner`, `ChatID`) but invisible in the terminal tab bar. A `background` flag enables fire-and-forget execution for long-running processes. Any hidden session can be promoted to visible (attached to a terminal tab) for interactive inspection with full scrollback replay.

### Target Architecture

```
Agent shell_command (foreground)
  → Hidden PTY session (TerminalManager)
  → Sentinel-based output capture
  → Return output + exit code to agent

Agent shell_command (background)
  → Hidden PTY session (TerminalManager)
  → Return {session_id, status: "running"} immediately
  → Agent queries output later via session ID

User clicks "Attach"
  → Promote hidden session → visible terminal tab
  → Reattach with scrollback replay (existing mechanism)
```

## Implementation Phases

### Phase A: Hidden Session Infrastructure (Backend Core)

**New files:**
- `pkg/webui/terminal_agent_exec.go` — `ExecuteCommandAndWait()`: sentinel-based synchronous command execution via PTY
- `pkg/webui/api_agent_sessions.go` — REST endpoints: list hidden sessions, promote to visible, retrieve output

**Modified files:**
- `pkg/webui/terminal_types.go` — Add `Hidden`, `Owner`, `ChatID`, `Name`, `AutoClose` fields to `TerminalSession`; add `CreateHiddenSession()`, `ListHiddenSessions()` methods
- `pkg/webui/terminal_lifecycle.go` — Exclude hidden sessions from default listing; longer cleanup timeout for background sessions
- `pkg/webui/server.go` — Register agent session API routes

**Key design decisions:**
- **Sentinel-based output capture**: Write command as `command && echo "__SPROUT_DONE__:$?" || echo "__SPROUT_DONE__:$?"` to PTY. Subscribe a temporary `termSub` to capture output. Scan for the sentinel to detect completion and extract exit code. Fallback timeout (30s default) if sentinel never appears.
- **Session reuse**: One hidden session per chat (not per command). Commands run sequentially in the same PTY, preserving shell environment state (`cd`, `export`, etc.) across tool calls.
- **Ring buffer**: Hidden sessions use the same 256 KB ring buffer as interactive sessions. On attach, scrollback replays from the buffer.
- **Cleanup**: Hidden sessions auto-expire via existing 30-minute inactive cleanup. Background sessions get a 2-hour timeout.

### Phase B: Agent Integration + Background Mode

**Modified files:**
- `pkg/agent_tools/shell.go` — Add `TerminalManager` check; route through hidden PTY when available (WebUI mode). CLI mode falls through to existing `os/exec` unchanged.
- `pkg/agent/shell.go` — Pass through `TerminalManager` for hidden session creation
- `pkg/agent/tool_definitions.go` — Add `background` (bool, default false) parameter to `shell_command` tool definition
- `pkg/agent/tool_handlers_shell.go` — Handle `background=true`: write command to hidden PTY, return immediately with session ID
- `pkg/webui/client_context.go` — Expose `TerminalManager` accessor for agent context wiring

**Key design decisions:**
- **CLI fallback**: CLI mode uses plain `os/exec` unchanged — no `TerminalManager` dependency. The routing is purely additive for WebUI mode.
- **Background sessions**: Named with command prefix. Agent can later query output via `GET /api/terminal/agent-sessions/{id}/output`. Agent receives structured `{session_id, status}` response.
- **Security**: Hidden sessions use same shell validation as interactive terminals (whitelist of known shells from `AvailableShells()`).

### Phase C: Frontend — Background Tasks Panel + Attach Flow

**New files:**
- `webui/src/components/BackgroundTasks.tsx` — Collapsible panel showing running background agent sessions with status, output preview, "Attach" and "Kill" buttons

**Modified files:**
- `webui/src/components/Terminal.tsx` — Wire background tasks panel into terminal area
- `webui/src/components/TerminalTabBar.tsx` — "Agent Sessions" dropdown showing attachable hidden sessions
- `webui/src/services/api/terminalApi.ts` — Add API calls for agent session management

**Key design decisions:**
- **Auto-refresh**: Polling-based (every 5s) for background session status. Could be upgraded to WebSocket events later.
- **Attach flow**: Promote clears `Hidden` flag → session appears in terminal tab bar → existing `reattach` mechanism handles scrollback replay and live output subscription.

## Tool Specification Changes

### `shell_command` Tool (Modified)

```json
{
  "name": "shell_command",
  "parameters": {
    "command":     {"type": "string",  "required": true},
    "background":  {"type": "boolean", "required": false, "default": false,
                    "description": "Run command in background. Returns immediately with session_id."},
    "session_id":  {"type": "string",  "required": false,
                    "description": "Check output of a background session by ID."}
  }
}
```

When `background=true`, the response becomes:
```json
{"session_id": "agent-abc123", "status": "running", "message": "Command running in background"}
```

When `session_id` is provided (without a new command), the response returns accumulated output:
```json
{"session_id": "agent-abc123", "status": "running", "output": "...", "exit_code": null}
```

## API Endpoints (New)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/terminal/agent-sessions` | List hidden sessions with status + last N bytes of output |
| `POST` | `/api/terminal/agent-sessions/{id}/attach` | Promote to visible (clears `Hidden` flag) |
| `GET` | `/api/terminal/agent-sessions/{id}/output` | Return accumulated ring buffer output as text |
| `DELETE` | `/api/terminal/agent-sessions/{id}` | Kill and remove a background session |

## Open Questions

1. Should hidden sessions be visible to all users or scoped to the chat session that created them? → Scoped to chat session initially; may want cross-session visibility later.
2. Maximum concurrent background sessions per chat? → Suggest limit of 5 to prevent resource abuse.
3. Should output capture scrollback be larger for background sessions (e.g., 1 MB vs 256 KB)? → Start with same 256 KB; may increase if real-world usage demands it.
4. How does the agent know when a background process has exited? → Polling via `session_id` check, or server pushes an event via the chat event stream.

## Files Reference

| File | Action | Phase |
|------|--------|-------|
| `pkg/webui/terminal_types.go` | Modify: add hidden session fields + methods | A |
| `pkg/webui/terminal_agent_exec.go` | **New**: sentinel-based command execution | A |
| `pkg/webui/api_agent_sessions.go` | **New**: agent session REST endpoints | A |
| `pkg/webui/terminal_lifecycle.go` | Modify: hidden session cleanup policy | A |
| `pkg/webui/server.go` | Modify: register agent session routes | A |
| `pkg/agent_tools/shell.go` | Modify: route through hidden PTY | B |
| `pkg/agent/shell.go` | Modify: pass TerminalManager through | B |
| `pkg/agent/tool_definitions.go` | Modify: add `background` parameter | B |
| `pkg/agent/tool_handlers_shell.go` | Modify: handle background mode | B |
| `pkg/webui/client_context.go` | Modify: expose TerminalManager accessor | B |
| `webui/src/components/BackgroundTasks.tsx` | **New**: background tasks panel | C |
| `webui/src/components/Terminal.tsx` | Modify: wire panel + attach flow | C |
| `webui/src/components/TerminalTabBar.tsx` | Modify: agent sessions dropdown | C |
| `webui/src/services/api/terminalApi.ts` | Modify: agent session API calls | C |
