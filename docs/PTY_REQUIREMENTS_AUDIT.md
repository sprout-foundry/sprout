# PTY Terminal System Requirements & Audit

## Requirements

### REQ-1: Pre-warmed PTY Pool
A group of hidden PTY sessions that have already booted (sourced .zshrc/.bashrc, etc.) so that all
shell command execution avoids paying the shell startup cost on every invocation. Sessions must be:
- Created proactively (one per chat)
- Fully initialized (shell prompt visible, rc files sourced)
- Reused across multiple commands within the same chat

### REQ-2: Background Shell Tasks
A `background` mode for `shell_command` that:
- Executes a command in a new PTY session without blocking the tool call
- Returns a session ID immediately
- Allows checking accumulated output by ID (`check_background=<id>`)
- Allows stopping the background task by ID (`stop_background=<id>`)
- Useful for running long-lived services (web servers, watchers, etc.)

### REQ-3: All Commands Through PTY
In WebUI mode, ALL synchronous shell commands from the agent must route through the pre-warmed
hidden PTY sessions, not through `os/exec`. This ensures:
- Consistent shell environment (aliases, env vars, PATH from .zshrc)
- No per-command shell startup latency
- Single code path for shell execution in WebUI mode

---

## Audit Findings

### PASS: REQ-3 — All Commands Through PTY
`ExecuteShellCommandWithSafety` checks for `TerminalManager` in context and routes through hidden
PTY when available (`streamOutput=false`). Falls back to `os/exec` only when PTY fails or in CLI mode.
Agent's `executeShellCommandWithTruncation` wires the TerminalManager into context. **Working correctly.**

### PARTIAL: REQ-1 — Pre-warmed PTY Pool
- ✅ `GetOrCreateHiddenSessionForChat` creates one hidden session per chat with deterministic ID
- ✅ Session reuse works — same chat gets the same session
- ✅ Shell is launched with `--login` flag to source rc files
- ❌ **NO readiness wait**: `GetOrCreateHiddenSessionForChat` returns immediately after creating the
  session, before the shell has finished sourcing .zshrc. The first command sent to a new session
  may arrive while the shell is still initializing, causing it to be buffered and potentially mixed
  with startup output.
- ✅ Tests have `waitForShellReady` but production code does not

### PARTIAL: REQ-2 — Background Shell Tasks
- ✅ `background=true` parameter works — creates new PTY, writes command, returns session ID
- ✅ `check_background=<id>` parameter works — returns accumulated ring buffer output
- ✅ Background sessions get 2-hour cleanup timeout (vs 30-min for regular hidden)
- ✅ Each background command gets its own session with descriptive name
- ❌ **NO `stop_background`**: No mechanism to stop/kill a background task by ID. The
  `TerminalAccess` interface has no `StopBackground` method. The tool definition has no
  `stop_background` parameter. Users cannot terminate background services.
- ❌ **Background output doesn't report process status**: `CheckBackgroundOutput` always reports
  `status: "running"` — it doesn't check if the process has actually exited.

---

## Gaps to Fix

1. **Shell readiness wait** (REQ-1): Add a readiness wait in `GetOrCreateHiddenSessionForChat`
   (or in `ExecuteCommandAndWait`) so the first command on a new session doesn't arrive during
   shell initialization.

2. **Stop background** (REQ-2): Add `stop_background` parameter to `shell_command` tool,
   `StopBackgroundSession` method to `TerminalAccess` interface and `TerminalManager`, and
   wire it through the handler.

3. **Background status** (REQ-2): Report whether the background process is still running or
  has exited in `CheckBackgroundOutput`.
