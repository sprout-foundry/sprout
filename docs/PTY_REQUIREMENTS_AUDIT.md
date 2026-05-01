# PTY Terminal System — Requirements & Implementation Status

## Requirements

### REQ-1: Pre-warmed PTY Pool
A group of hidden PTY sessions that have already booted (sourced .zshrc/.bashrc, etc.)
so that all shell command execution avoids paying the shell startup cost on every
invocation.

- Created proactively (one per chat session)
- Fully initialized (shell prompt visible, rc files sourced)
- Reused across multiple commands within the same chat

### REQ-2: Background Shell Tasks
A `background` mode for `shell_command` that:
- Executes a command in a new PTY session without blocking the tool call
- Returns a session ID immediately
- Allows checking accumulated output by ID (`check_background=<id>`)
- Allows stopping the background task by ID (`stop_background=<id>`)
- Reports whether the process is still running or has exited
- Useful for running long-lived services (web servers, watchers, etc.)

### REQ-3: All Commands Through PTY
In WebUI mode, ALL synchronous shell commands from the agent must route through
the pre-warmed hidden PTY sessions, not through `os/exec`. This ensures:
- Consistent shell environment (aliases, env vars, PATH from .zshrc)
- No per-command shell startup latency
- Single code path for shell execution in WebUI mode

### REQ-4: UI Attachment for Agent Sessions
Hidden/background PTY sessions should be "attachable" in the WebUI terminal
so users can see the output of running agent commands:
- Background sessions appear as attachable in the terminal tab bar
- User can click to promote a hidden session to a visible terminal tab
- User can view accumulated output of background sessions
- User can kill/stop background sessions from the UI
- The terminal reuses the existing WebSocket PTY stream (no new PTY needed)

## Implementation Status

### REQ-1: Pre-warmed PTY Pool ✅ COMPLETE
- `GetOrCreateHiddenSessionForChat` creates one hidden session per chat
- `waitForShellReady()` waits for 500ms quiet period after shell startup
- Session reuse works — same chat gets the same session
- Shell launched with `--login` flag to source rc files
- Falls back to os/exec if shell fails to initialize

### REQ-2: Background Shell Tasks ✅ COMPLETE
- `background=true` creates new PTY, writes command, returns session ID
- `check_background=<id>` returns accumulated output + running/exited status
- `stop_background=<id>` sends Ctrl+C then closes the session
- Each background command gets its own session with descriptive name
- Background sessions get 2-hour cleanup timeout (vs 30-min for regular hidden)

### REQ-3: All Commands Through PTY ✅ COMPLETE
- `ExecuteShellCommandWithSafety` routes through PTY when TerminalManager in context
- Falls back to os/exec only when PTY fails or in CLI mode
- Agent wires TerminalManager into context for WebUI mode

### REQ-4: UI Attachment ✅ COMPLETE
- Backend: `GET /api/terminal/agent-sessions` lists background sessions with preview
- Backend: `POST /api/terminal/agent-sessions/{id}/attach` promotes hidden → visible
- Backend: `POST /api/terminal/agent-sessions/{id}/kill` terminates background session
- Backend: `GET /api/terminal/agent-sessions/{id}/output` returns accumulated output
- Frontend: `TerminalTabBar` component shows `AttachableSession` badges
- Frontend: Click to attach promotes session to visible terminal tab
- Frontend: `TerminalPane` supports reattach to existing PTY session
