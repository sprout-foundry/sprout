# SP-062: CLI-Native Background Shell Execution

**Status:** ✅ Implemented (BackgroundProcessManager wired into shell dispatch)
**Date:** 2026-05-31
**Depends on:** None (extends existing `shell_command` tool)
**Priority:** High
**Effort Estimate:** ~1 week (4 phases)

## Problem

The `shell_command` tool has three background-related parameters exposed to the LLM:

- `background=true` — start a command asynchronously, return immediately with a session ID
- `check_background=<id>` — poll accumulated output
- `stop_background=<id>` — terminate the session

All three **only work in WebUI mode** because they depend on `TerminalManager` (hidden PTY sessions). When the agent runs without the web UI (pure CLI, `--no-web-ui`, or non-interactive `sprout agent "..."`):

1. **`background=true` → error**: `"background mode requires WebUI terminal manager"`
2. **2-minute timeout → command killed with no fallback**: The automatic promotion logic (`COMMAND_PROMOTED_TO_BACKGROUND`) in `ExecuteCommandAndWait` only fires when a `TerminalManager` is present. In CLI mode, the `os/exec` path just returns `context.DeadlineExceeded` and the command dies.
3. **`check_background` / `stop_background` → error**: Same `"requires WebUI terminal manager"` response.

This means the LLM is told about a capability (`background`, `check_background`, `stop_background`) in its tool definition that **silently fails** when the agent runs without the web UI. The LLM has no way to know ahead of time whether background mode will work, and after a timeout it gets a dead-end error with no recovery instructions.

### Concrete scenarios

| Scenario | What happens today | What should happen |
|----------|-------------------|--------------------|
| `sprout agent "start the dev server and run the tests"` | Agent runs `npm run dev`, hits 2-min timeout, command killed, agent gets a generic error with no path forward | Agent starts dev server in background, gets session ID, runs tests, checks server output, stops server when done |
| `sprout agent --no-web-ui "run the e2e suite"` | Long-running test suite killed at 2 minutes | Promoted to background, agent polls for completion |
| `sprout agent "deploy to staging"` | Deploy script killed at 2 minutes | Runs in background, agent monitors progress |

## Current Architecture

```
┌──────────────────────────────────────────────────────────┐
│ Tool definition (tool_registrations.go)                  │
│  - background, check_background, stop_background params  │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│ handleShellCommand (tool_handlers_shell.go)               │
│  Dispatches based on background/check/stop params         │
└──────┬───────────────────────────┬───────────────────────┘
       │                           │
       ▼                           ▼
┌──────────────────┐    ┌──────────────────────────────────┐
│ WebUI path       │    │ CLI path (os/exec)               │
│ TerminalManager  │    │ runShellCommand()                │
│  ✓ background    │    │  ✗ background → error            │
│  ✓ auto-promote  │    │  ✗ timeout → command killed      │
│  ✓ check/stop    │    │  ✗ check/stop → error            │
└──────────────────┘    └──────────────────────────────────┘
```

## Proposed Solution

Introduce a `BackgroundProcessManager` that provides PTY-less background execution using `os/exec.Cmd.Start()` + output capture to temp files. This becomes the fallback when `TerminalManager` is nil.

```
┌──────────────────────────────────────────────────────────┐
│ handleShellCommand (tool_handlers_shell.go)               │
│  background? → choose manager                            │
│    TerminalManager (WebUI)  OR  BackgroundProcessManager  │
└──────┬───────────────────────────┬───────────────────────┘
       │                           │
       ▼                           ▼
┌──────────────────┐    ┌──────────────────────────────────┐
│ WebUI path       │    │ CLI path                         │
│ TerminalManager  │    │ BackgroundProcessManager         │
│  (unchanged)     │    │  ✓ background via os/exec.Start  │
│                  │    │  ✓ auto-promote on timeout       │
│                  │    │  ✓ check → read output file      │
│                  │    │  ✓ stop → Process.Kill()         │
└──────────────────┘    └──────────────────────────────────┘
```

### Design Principles

1. **Same interface, different backend.** The LLM sees identical `background`/`check_background`/`stop_background` parameters. The response JSON shape (`{"session_id": "...", "status": "running", "output": "..."}`) is identical. The LLM cannot tell which backend is active.

2. **No PTY required.** The CLI path uses `os/exec.Cmd.Start()` with stdout/stderr piped to a temp file. No pseudo-terminal, no `TerminalManager`, no hidden sessions.

3. **Automatic promotion on timeout.** When `ExecuteShellCommandWithSafety` detects `context.DeadlineExceeded` in the `os/exec` path (today it just dies), the running process is adopted by the `BackgroundProcessManager` instead of being killed. The agent gets the same `formatBackgroundPromotionMessage()` response.

4. **Bounded lifecycle.** Background processes auto-expire after 2 hours of inactivity (matching the WebUI background session timeout). A cleanup goroutine reaps exited processes.

5. **Per-agent scope.** The `BackgroundProcessManager` lives on the `Agent` struct, next to `terminalManager`. Processes are scoped to the agent session.

## Implementation

### Phase 1: `BackgroundProcessManager` core

**New file:** `pkg/agent_tools/background_process.go`

```go
// BackgroundProcess represents a tracked background process.
type BackgroundProcess struct {
    ID        string    // "bg-<sanitized-prefix>-<random-hex>"
    Cmd       *exec.Cmd // the running process (nil after exit)
    OutputFile string   // path to accumulated output temp file
    Dir       string    // working directory
    StartedAt time.Time
    LastPolled time.Time
    done      chan struct{} // closed when process exits
    mu        sync.Mutex
}

// BackgroundProcessManager manages background processes for CLI mode.
// Provides the same lifecycle as the WebUI's TerminalManager background
// sessions but without PTY support.
type BackgroundProcessManager struct {
    processes map[string]*BackgroundProcess
    mu        sync.RWMutex
    expiry    time.Duration // default: 2 hours
}
```

**Key methods:**

| Method | Purpose |
|--------|---------|
| `Start(ctx, command, dir) (sessionID, error)` | `exec.Cmd.Start()` with stdout+stderr → temp file, returns session ID |
| `CheckOutput(sessionID) (output, status, error)` | Reads accumulated output from temp file, checks if process exited |
| `Stop(sessionID) error` | Sends `os.Interrupt` → `time.Sleep(100ms)` → `os.Kill`, waits for exit |
| `IsActive(sessionID) bool` | Checks if process is still running |
| `Adopt(cmd, outputFile, dir) (sessionID, error)` | Adopts an already-running `exec.Cmd` into background management (for timeout promotion) |
| `cleanupLoop()` | Goroutine that reaps exited/expired processes every 60s |

**Session ID format:** `bg-<prefix>-<8-hex>` (identical to WebUI format for LLM consistency).

**Output capture strategy:**
- On `Start()`, create a temp file: `~/.config/sprout/background/<sessionID>.output`
- `cmd.Stdout` and `cmd.Stderr` are both piped to this file via `io.MultiWriter` (file + optional ring buffer)
- `CheckOutput()` reads the entire file (or tail N bytes for large outputs)
- On cleanup, delete the temp file

### Phase 2: Timeout promotion in CLI path

**Modified file:** `pkg/agent_tools/shell_native.go`

Currently `runShellCommand` creates an `exec.Cmd`, runs it, and returns. There's no way to "rescue" a timed-out command because the context cancellation kills it.

The fix: when `ctx` is derived from the tool executor's 2-minute deadline, detect `context.DeadlineExceeded` and **adopt the process into background** instead of killing it.

```go
func runShellCommand(ctx context.Context, command string, streamOutput bool) (string, error) {
    // ... existing setup ...

    cmd := exec.CommandContext(ctx, shell, "-c", command)
    // ... dir setup ...

    // Pipe output so we can capture it even on timeout
    var outputBuf bytes.Buffer
    stdoutPipe, _ := cmd.StdoutPipe()
    stderrPipe, _ :=.StderrPipe()

    cmd.Start()

    // ... io.Copy into outputBuf ...

    err = cmd.Wait()

    if ctx.Err() == context.DeadlineExceeded && err != nil {
        // Tool deadline hit but process may still be running.
        // Adopt it into the BackgroundProcessManager.
        if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
            sessionID, adoptErr := bpm.Adopt(cmd, outputFile, cmd.Dir)
            if adoptErr == nil {
                return formatBackgroundPromotionMessage(sessionID, command, outputBuf.String()), nil
            }
        }
    }
    // ... existing error handling ...
}
```

**Important:** `exec.CommandContext` sends `os.Kill` on context cancellation by default. We need to override this to keep the process alive:

```go
cmd.Cancel = func() error {
    // Don't kill on context cancel — let Adopt() take over.
    return nil
}
```

Wait — `exec.CommandContext` with `cmd.Cancel` set to a no-op will prevent the automatic kill, but `cmd.Wait()` will still return `context.DeadlineExceeded`. After adoption, the process keeps running and `BackgroundProcessManager` takes ownership.

Actually, there's a subtlety: with `Cancel = no-op`, `cmd.Wait()` returns the context error immediately but the process keeps running. The `Adopt` method needs to:
1. Get the PID from `cmd.Process.Pid`
2. Create a new tracked `BackgroundProcess` with a new `exec.Cmd` that points at the same PID (or just track the `*os.Process` directly)
3. Redirect future output to the output file (this is the tricky part — the original pipes are already consumed by the `io.Copy` goroutines)

**Alternative approach (simpler, more reliable):**

Instead of trying to adopt a process after timeout, restructure the execution flow:

1. Always create the temp output file at the start
2. Always pipe to the temp file
3. If the command finishes within the deadline → return output, delete temp file
4. If the deadline expires → check if process is still running. If yes, register with `BackgroundProcessManager`, return promotion message. The process was already writing to the temp file, so no output is lost.

This avoids the pipe-redirection problem entirely.

### Phase 3: Wire into agent dispatch

**Modified file:** `pkg/agent/shell.go` and `pkg/agent/tool_handlers_shell.go`

The agent already has `terminalManager` for WebUI mode. Add a parallel `backgroundProcessManager` for CLI mode.

```go
// In Agent struct or initialization:
backgroundProcessManager *tools.BackgroundProcessManager

// When terminalManager is nil (CLI mode):
if a.terminalManager == nil {
    if a.backgroundProcessManager == nil {
        a.backgroundProcessManager = tools.NewBackgroundProcessManager()
    }
    ctx = tools.WithBackgroundProcessManager(ctx, a.backgroundProcessManager)
}
```

**Modified file:** `pkg/agent_tools/shell.go`

`ExecuteShellCommandBackground`, `CheckBackgroundOutput`, and the handler methods check for `BackgroundProcessManager` as fallback:

```go
func ExecuteShellCommandBackground(ctx context.Context, command string, sessionID string) (string, error) {
    // Try TerminalManager first (WebUI mode)
    if tm := TerminalManagerFromContext(ctx); tm != nil {
        // ... existing PTY path ...
    }

    // Fallback to BackgroundProcessManager (CLI mode)
    if bpm := BackgroundProcessManagerFromContext(ctx); bpm != nil {
        sessionID, err := bpm.Start(ctx, command, "")
        if err != nil {
            return "", err
        }
        result, _ := json.Marshal(map[string]string{
            "session_id": sessionID,
            "status":     "running",
        })
        return string(result), nil
    }

    return "", fmt.Errorf("background mode requires WebUI terminal manager or BackgroundProcessManager")
}
```

Same dual-path for `CheckBackgroundOutput` and `StopBackgroundSession`.

### Phase 4: Cleanup and lifecycle

**Agent shutdown:** When the `Agent` shuts down (`Shutdown()`), stop all managed background processes:

```go
func (a *Agent) Shutdown() {
    // ... existing shutdown ...
    if a.backgroundProcessManager != nil {
        a.backgroundProcessManager.StopAll()
    }
}
```

**Cleanup goroutine:** The `BackgroundProcessManager` runs a background goroutine that:
- Scans all tracked processes every 60 seconds
- Kills processes that have been inactive (not polled) for > 2 hours
- Removes exited processes from the map
- Deletes temp output files for exited processes after 5 minutes

**On context cancellation:** When the agent's root context is cancelled (user hits Ctrl+C), the cleanup goroutine stops. A `defer` in `RunAgent` calls `backgroundProcessManager.StopAll()` to clean up.

## Edge Cases

| Case | Behavior |
|------|----------|
| Process exits before `check_background` | `CheckOutput` returns `status: "exited"` with full output + exit code |
| Process exits between promotion and first poll | Same — status will be "exited" on first poll |
| Agent crashes / is killed | Output files remain in `~/.config/sprout/background/` — stale file cleanup on next startup |
| Orphan detection | On startup, scan `~/.config/sprout/background/` for output files. Kill any PIDs still running (if recorded), delete files |
| Multiple concurrent background commands | Per-chat cap of 5 (matching WebUI's `maxBackgroundSessionsPerChat`) |
| Shell builtins (`cd`, `export`) | Cannot be backgrounded — they're shell-internal. Return an error suggesting the user wrap in a script |
| Commands that daemonize (e.g., `npm run dev &`) | The parent shell exits immediately; the child may or may not be trackable. Output file captures what the parent produced before exiting. `check_background` will report "exited" immediately |
| `stop_background` on an already-exited process | Return success with a note that the process already exited |

## Response Format

All responses use the same JSON shape as the WebUI path:

**`background=true`:**
```json
{"session_id": "bg-npm-dev-a1b2c3d4", "status": "running"}
```

**`check_background`:**
```json
{"session_id": "bg-npm-dev-a1b2c3d4", "status": "running", "output": "Server started on port 3000\n"}
```

**`stop_background`:**
```
Background session bg-npm-dev-a1b2c3d4 stopped.
```

**Timeout promotion (automatic):**
```
Command exceeded the 2-minute tool deadline. It is still running in background session bg-npm-dev-a1b2c3d4.

Command: npm run dev

Output so far (partial):
Server started on port 3000
Compiling...

IMPORTANT: the output above is partial — only what arrived before the 2-minute tool deadline. The command kept running.

To get the rest, do NOT assume the command finished — actively poll:
- Check progress (returns accumulated output since session start): shell_command check_background="bg-npm-dev-a1b2c3d4"
- Stop it (kills the process): shell_command stop_background="bg-npm-dev-a1b2c3d4"

Background sessions are kept for up to 2 hours of inactivity. Either wait and poll,
or stop the command if you want to try a different approach.
```

This is the exact same message produced by `formatBackgroundPromotionMessage()` — no changes needed.

## Files Changed

| File | Change |
|------|--------|
| `pkg/agent_tools/background_process.go` | **New** — `BackgroundProcess`, `BackgroundProcessManager` |
| `pkg/agent_tools/background_process_test.go` | **New** — unit tests |
| `pkg/agent_tools/shell_native.go` | **Modify** — detect timeout, adopt process into background |
| `pkg/agent_tools/shell.go` | **Modify** — dual-path in `ExecuteShellCommandBackground`, `CheckBackgroundOutput` |
| `pkg/agent_tools/terminal.go` | **Modify** — add `BackgroundProcessManagerFromContext` context helper |
| `pkg/agent/shell.go` | **Modify** — wire `BackgroundProcessManager` into context when no TerminalManager |
| `pkg/agent/agent.go` | **Modify** — add `backgroundProcessManager` field, initialize, shutdown |
| `pkg/agent/tool_handlers_shell.go` | **Modify** — fallback path in `stopBackgroundSession`, `checkBackgroundOutput` |

## Testing Plan

1. **Unit tests** — `BackgroundProcessManager.Start/Check/Stop` with short-lived commands (`echo`, `sleep`)
2. **Timeout promotion test** — Run `sleep 150` (2.5 min), verify promotion at 2 min, verify `check_background` returns output, verify `stop_background` kills it
3. **Concurrent cap test** — Start 6 background commands, verify the 6th returns a cap-reached error
4. **Exit detection test** — Start `echo done`, poll until status becomes "exited"
5. **Cleanup test** — Start a process, mark it inactive for > expiry, verify cleanup goroutine removes it
6. **Integration test** — Full `sprout agent "start a dev server and check if it's running"` flow without WebUI

## Out of Scope

- **PTY support in CLI mode.** The CLI path does not get PTY features (interactive programs, color passthrough, terminal resizing). This is intentional — the goal is background process management, not full terminal emulation.
- **Streaming output to CLI.** Background process output is only accessible via `check_background` polling. Real-time streaming to the terminal is not supported.
- **Persistent background across sessions.** Background processes die when the agent exits. Persistent services should use `sprout service install`.
- **Background for `browse_url` or other tools.** Only `shell_command` gets background support.

## Security Considerations

- The same risk cascade (static classifier + persona rules) that gates synchronous shell commands applies to background commands.
- Background processes run with the same permissions as the agent process.
- `stop_background` sends SIGINT then SIGKILL — same authority as starting the command.
- Output temp files are in `~/.config/sprout/background/` (user-private directory).
- Cap enforcement prevents a runaway agent from spawning unlimited background processes.
