# SP-063-4g: Panic Key — Emergency Stop for Computer-Use Action Loops

## TL;DR

Add a panic key that lets a user emergency-stop a runaway computer-use action
loop. Pressing **Ctrl+Shift+Escape** (configurable) triggers a three-layer
halt: (1) cancel the agent's `interruptCtx` via the existing `TriggerInterrupt()`
path so the LLM→tool→LLM loop unwinds; (2) kill any in-flight subprocess backend
via a new `PanicableBackend` decorator that wraps the subprocess in a process
group and sends SIGKILL on cancel; (3) record the event to the JSONL audit log
as `"panic_key_triggered"` with timing and reason metadata.

The panic key is **not a new transport** — it reuses `TriggerInterrupt()` from
`pkg/agent/pause.go:21` (already wired to the WebUI Stop button at
`pkg/webui/api_query.go:560`) and adds a subprocess-kill decorator between the
rate-limit and auditing layers in the backend chain.

**Chosen approach summary**: Reuse `TriggerInterrupt()` for loop cancellation
(no dedicated fast path needed — the existing `interruptCtx` propagates through
tool timeouts at `pkg/agent/tool_executor_sequential.go:128`). Add a
`PanicableBackend` decorator that wraps every subprocess call in a process group
(process-group helpers `SetProcessGroup`/`KillProcessGroup` are exported from
`pkg/agent_tools/computer_use/process_group_unix.go` so both the decorator and
`background_process_signal_unix.go` can call them). Record the event via
`RecordSafetyEvent()` at `pkg/agent_tools/computer_use/audit.go:88`.

---

## Glossary

| Term | Definition |
|------|-----------|
| **Panic key** | A key chord the user presses to emergency-stop a computer-use action loop |
| **Action loop** | The LLM→tool→LLM cycle driven by `seed/core.Agent.Run()` at `pkg/agent/seed_query.go:367` |
| **Subprocess backend** | `subprocessBackend` at `pkg/agent_tools/computer_use/backend_subprocess.go:24` that drives cliclick/xdotool via `exec.Command(...).CombinedOutput()` |
| **Backend decorator chain** | `subprocess → rateLimited → auditing` composition at `pkg/agent/computer_use_registration.go:53-66` |
| **Process group** | Unix process group (Setpgid) allowing SIGKILL to reach child processes; exported helpers `SetProcessGroup`/`KillProcessGroup` at `pkg/agent_tools/computer_use/process_group_unix.go` |
| **interruptCtx** | Per-agent cancellable context at `pkg/agent/pause.go:21` that `TriggerInterrupt()` cancels |
| **Audit log** | JSONL file with `AuditRecord{Time, Action, Args, Err}` at `pkg/agent_tools/computer_use/audit.go:15-20` |

---

## (a) What is the panic key?

### Definition

The panic key is **Ctrl+Shift+Escape** by default. It is a single key chord
captured at two levels:

1. **TUI (CLI)**: The `pkg/console/` reader watches for the chord on stdin.
   When the user presses Ctrl+Shift+Escape while the agent is running in
   computer_user mode, the reader calls `agent.TriggerInterrupt()` and
   additionally signals the panic-key decorator (see section b).

2. **WebUI**: The WebUI already has a "Stop" button wired to
   `handleAPIQueryStop` at `pkg/webui/api_query.go:560`, which calls
   `agent.TriggerInterrupt()` at line 595. The panic key in the WebUI is
   the **same Stop button** — no separate key chord is needed in the browser
   because the user can click Stop with their mouse. However, we add a
   keyboard shortcut in the WebUI (Ctrl+Shift+Escape) that triggers the same
   endpoint, for parity with the CLI and for users who prefer keyboard
   interaction.

### What happens immediately

When the panic key is pressed:
1. `PanicableBackend.Halt()` sets the halted flag and kills any in-flight subprocess.
2. `agent.TriggerInterrupt()` cancels `interruptCtx` (`pkg/agent/pause.go:21`),
   which propagates through tool timeouts at `pkg/agent/tool_executor_sequential.go:128`.
3. A `"panic_key_triggered"` event is recorded to the audit log (see section d).
4. The user sees feedback: TUI prints `[PANIC KEY: computer use halted]` or
   the WebUI shows a toast notification.

### Key chord configuration

The default chord is `Ctrl+Shift+Escape`. It is configurable via a new field
in `ComputerUseConfig` at `pkg/configuration/config_domain.go:47`:

```go
type ComputerUseConfig struct {
    // ... existing fields ...

    // PanicKeyChord is the key chord that triggers the panic key.
    // Default: "ctrl+shift+escape". Set to "" to disable.
    PanicKeyChord string `json:"panic_key_chord,omitempty"`
}
```

The chord is parsed at registration time (`RegisterComputerUseTools` at
`pkg/agent/computer_use_registration.go:42`) and stored in the panic-key
handler. If parsing fails, the default chord is used and a warning is logged.

### Chosen approach

- **Single chord** (Ctrl+Shift+Escape) captured at both TUI and WebUI levels.
- **No new signal handler** — reuse `TriggerInterrupt()` from `pkg/agent/pause.go:21`
  for the loop cancellation path.
- **Immediate subprocess kill** via a new `PanicableBackend` decorator that wraps
  the subprocess in a process group and kills it on panic.
- **Audit event** emitted via `RecordSafetyEvent()` at `audit.go:88`.

### Open questions

- Should the panic key be available when the agent is NOT in computer_user mode?
  (Current answer: no — it's a computer-use-specific safety feature. A regular
  Stop button suffices for non-computer-use queries.)
- Should the chord be configurable per-platform? (e.g., Cmd+Shift+Escape on
  macOS?) Current answer: no — Ctrl+Shift+Escape works on both platforms and
  keeps the config simple.

---

## (b) How does the panic key cancel an in-flight computer-use action?

### The problem

The subprocess backend at `pkg/agent_tools/computer_use/backend_subprocess.go:33`
uses `exec.Command(...).CombinedOutput()` with **no `SysProcAttr`** and no
`Setpgid`. This means:

1. There is no process group to signal — `SetProcessGroup()` from
   `pkg/agent_tools/computer_use/process_group_unix.go` cannot be used.
2. There is no context parameter — `CombinedOutput()` blocks until the process
   exits, with no way to cancel it mid-flight.
3. If the parent sprout process exits while a child subprocess (xdotool,
   cliclick) is still running, the child becomes orphaned and continues
   executing.

### The solution: `PanicableBackend` decorator

We introduce a new decorator `PanicableBackend` that wraps the subprocess
backend and adds two capabilities:

1. **Process group wrapping**: Every subprocess call is wrapped in a process
   group using `SetProcessGroup()` from
   `pkg/agent_tools/computer_use/process_group_unix.go`.
2. **Context-aware execution**: The decorator holds a `context.Context` that
   is cancelled when the panic key fires. If the context is cancelled while
   a subprocess is running, the decorator kills the process group.

#### Interface sketch

```go
// PanicableBackend wraps a ComputerBackend so that in-flight subprocess
// actions can be killed when the panic key is pressed. It sits between
// the subprocess backend and the rate-limit decorator.
type PanicableBackend struct {
    inner ComputerBackend
    mu    sync.Mutex
    // currentProcess tracks the in-flight subprocess for kill-on-cancel.
    currentProcess *os.Process
    currentPGID    int
    halted         bool
    haltReason     string
    haltedAt       time.Time
}

// NewPanicableBackend wraps inner with panic-key support.
func NewPanicableBackend(inner ComputerBackend) *PanicableBackend

// Halt signals the panic key was pressed. It kills any in-flight subprocess
// and records the halt state. Subsequent actions return ErrPanicKeyHalted.
func (p *PanicableBackend) Halt(reason string) error

// IsHalted reports whether the panic key has been triggered.
func (p *PanicableBackend) IsHalted() bool

// Reset clears the halted state after the user acknowledges the halt.
// This is called when the user restarts computer use after a panic.
func (p *PanicableBackend) Reset()

// All 7 ComputerBackend methods are implemented by delegating to inner,
// but with process-group wrapping and context cancellation handling.
func (p *PanicableBackend) Screenshot(region *Rect) ([]byte, Size, error)
func (p *PanicableBackend) MouseClick(x, y int, button MouseButton, double bool) error
// ... etc.
```

#### How it wraps subprocess calls

The `PanicableBackend` does NOT implement the 7 methods itself — instead, it
wraps the `commandRunner` variable at `pkg/agent_tools/computer_use/backend_subprocess.go:33`.
We introduce a new `panicAwareCommandRunner` that replaces `commandRunner`:

```go
// panicAwareCommandRunner wraps exec.Command with process-group semantics
// and context cancellation. It replaces the package-level commandRunner
// when the PanicableBackend decorator is active.
func (p *PanicableBackend) runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, name, args...)

    // Set process group so we can kill the whole tree on cancel.
    // Reuses SetProcessGroup() from computer_use/process_group_unix.go.
    SetProcessGroup(cmd)

    // Capture stdout/stderr via buffers so we can return output regardless.
    var buf bytes.Buffer
    cmd.Stdout = &buf
    cmd.Stderr = &buf

    // Start the process and track it for kill-on-cancel.
    if err := cmd.Start(); err != nil {
        return nil, err
    }

    // Track the process for panic-key kill.
    p.trackProcess(cmd.Process.Pid, cmd.Process)

    // Wait for the process to exit.
    err := cmd.Wait()
    p.untrackProcess(cmd.Process.Pid)

    // If context was cancelled (panic key), kill the process group.
    if ctx.Err() != nil {
        KillProcessGroup(cmd.Process) // from computer_use/process_group_unix.go
        return buf.Bytes(), ErrPanicKeyHalted
    }

    // Normal path: return captured output and any error.
    return buf.Bytes(), err
}
```

**Key design decision**: We use `exec.CommandContext` instead of plain
`exec.Command`. `CommandContext` automatically sends SIGKILL when the context
is cancelled, but we add process-group wrapping (`SetProcessGroup`) so that
child processes spawned by xdotool/cliclick are also killed.

#### Backend decorator chain (updated)

Before (at `pkg/agent/computer_use_registration.go:53-66`):
```
auditingBackend(rateLimitedBackend(subprocessBackend))
```

After:
```
auditingBackend(rateLimitedBackend(panicableBackend(subprocessBackend)))
```

The `PanicableBackend` is the innermost decorator (wrapping `subprocessBackend`
directly) so it can intercept every subprocess call.

#### Subprocess lifecycle changes

The current `subprocessBackend.run()` method at
`pkg/agent_tools/computer_use/backend_subprocess.go:37-42` uses the
package-level `commandRunner` variable. We modify it to accept a context:

```go
func (b *subprocessBackend) runWithCtx(ctx context.Context, name string, args ...string) error {
    out, err := commandRunnerWithCtx(ctx, name, args...)
    if err != nil {
        return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
    }
    return nil
}
```

The 7 method implementations (`Screenshot`, `MouseClick`, etc.) are updated
to call `runWithCtx` instead of `run`. The context comes from the
`PanicableBackend` wrapper, which derives it from the agent's `interruptCtx`.

**Wait**: the `ComputerBackend` interface at `pkg/agent_tools/computer_use/types.go:39-47`
does not have context parameters. We have two options:

1. **Add context to the interface** — breaks the interface contract, requires
   updating all 7 method signatures and all callers.
2. **Keep the interface unchanged** — the `PanicableBackend` decorator uses a
   package-level context that is set at registration time and cancelled on
   panic. This is the approach we choose.

The package-level context approach is simpler and avoids interface churn. The
`PanicableBackend` holds a `context.Context` that is derived from the agent's
`interruptCtx` at registration time. When `TriggerInterrupt()` is called, the
context is cancelled, and the in-flight subprocess is killed.

#### Edge case: Parent exits while child subprocess runs

When the sprout process exits (e.g., Ctrl+C on the parent), any child
subprocess that was spawned with `Setpgid=true` will receive SIGHUP and
terminate. Without `Setpgid`, the child becomes orphaned (adopted by init).
The `PanicableBackend` decorator ensures `Setpgid=true` is always set, so
children are properly cleaned up on parent exit.

Additionally, we register an `os.Signal` handler for `SIGTERM` and `SIGHUP`
at registration time that calls `PanicableBackend.Halt("parent_exit")` to
ensure any in-flight subprocess is killed before the parent exits.

#### Edge case: Recursive / double-trigger

If the user presses the panic key twice (or mashes it), the second trigger is
a no-op. The `PanicableBackend.halted` flag is checked before any action:

```go
func (p *PanicableBackend) Halt(reason string) error {
    p.mu.Lock()
    if p.halted {
        // Already halted — log the duplicate trigger but don't re-kill.
        p.mu.Unlock()
        audit.RecordSafetyEvent("panic_key_duplicate", map[string]any{
            "reason": reason,
            "original_halt_at": p.haltedAt.Format(time.RFC3339),
        })
        return ErrAlreadyHalted
    }
    p.halted = true
    p.haltReason = reason
    p.haltedAt = time.Now()
    p.mu.Unlock()

    // Kill any in-flight subprocess.
    p.killCurrentProcess()

    // Record the panic key event.
    audit.RecordSafetyEvent("panic_key_triggered", map[string]any{
        "reason": reason,
    })

    return nil
}
```

The duplicate trigger is recorded to the audit log (action: `"panic_key_duplicate"`)
for debugging purposes but does not re-kill or re-cancel anything.

### Chosen approach

- **`PanicableBackend` decorator** inserted between subprocess and rate-limit
  layers in the decorator chain.
- **Process group wrapping** using `SetProcessGroup()` from
  `pkg/agent_tools/computer_use/process_group_unix.go` on every subprocess call.
  (The helpers are defined in the `computer_use` package as exported functions
  so both the panic-key decorator and `background_process_signal_unix.go` can
  call them. `background_process_signal_unix.go` imports them back.)
- **Context cancellation** using `exec.CommandContext` + process-group SIGKILL
  on cancel, reusing `KillProcessGroup()` from
  `pkg/agent_tools/computer_use/process_group_unix.go`.
- **Package-level context** derived from `interruptCtx` at registration time,
  avoiding interface changes to `ComputerBackend`.
- **Idempotent halt** — double-trigger is a no-op after the first, recorded
  to audit log as `"panic_key_duplicate"`.
- **Parent exit handler** — `os.Signal` handler for SIGTERM/SIGHUP calls
  `Halt("parent_exit")` to clean up orphaned subprocesses.
- **Platform support** — Panic key is Unix-only initially. It relies on
  `Setpgid` + `kill(-pgid, SIGKILL)` via `process_group_unix.go` (the `_unix`
  filename suffix follows Go build-tag convention). Windows requires
  `CreateProcess` with `CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent`
  and is out of scope for v1.

### Open questions

- Should the panic key also cancel actions that are NOT subprocess-based
  (e.g., a future native macOS AppleScript backend)? Current answer: yes,
  the decorator is designed to be backend-agnostic — it kills whatever
  process is tracked, regardless of how it was spawned.
- Should there be a grace period before SIGKILL (e.g., SIGINT first, then
  SIGKILL after 500ms)? Current answer: no — the panic key is an emergency
  stop, not a graceful shutdown. SIGKILL is immediate.

---

## (c) How does the panic key break the LLM→tool→LLM loop?

### The problem

The action loop lives in `seed/core.Agent.Run()` at `pkg/agent/seed_query.go:367`.
The existing cancellation is `TriggerInterrupt()` at `pkg/agent/pause.go:21`,
which cancels `interruptCtx`. This context is the parent for tool timeouts at
`pkg/agent/tool_executor_sequential.go:128`:

```go
parentCtx := te.agent.InterruptCtx()
ctx, cancel := context.WithTimeout(parentCtx, toolTimeout)
```

Cancelling `interruptCtx` cancels all derived tool contexts and the seed
library's `Run()` loop exits.

### The solution: reuse `TriggerInterrupt()`

We **do not introduce a dedicated fast path**. The existing mechanism is proven
(WebUI Stop button at `pkg/webui/api_query.go:560` uses it) and propagates
correctly through all tool execution contexts.

#### Sequence of events when panic key is pressed

1. User presses Ctrl+Shift+Escape (TUI) or clicks Stop (WebUI).
2. `PanicableBackend.Halt("panic_key")` sets the halted flag, kills any
   in-flight subprocess, and records `"panic_key_triggered"` to the audit log.
3. `agent.TriggerInterrupt()` cancels `interruptCtx` at `pkg/agent/pause.go:21`.
4. Tool execution context at `pkg/agent/tool_executor_sequential.go:128` is
   cancelled. The seed library's `Run()` at `pkg/agent/seed_query.go:367`
   detects the cancellation and exits.
5. `HandleInterrupt()` at `pkg/agent/pause.go:44` processes the interrupt and
   returns `"STOP"`. The user sees the halt message and the agent returns to
   the idle prompt.

#### Why not a dedicated fast path?

A dedicated fast path would need to bypass `context.WithTimeout` and coordinate
with the seed library's `Run()` loop, which only accepts a single context. The
existing path is already sub-second (the tool goroutine detects `ctx.Done()`
immediately). The real bottleneck is the subprocess kill, handled by the
`PanicableBackend` decorator (section b). Both paths operate in parallel and
complete within ~500ms.

#### Edge case: Replay after cancel

After the panic key halts, the user can continue the same query (message history
is preserved) or start a new query. Computer use requires **fresh opt-in
consent** — `computerUseSessionApproved` at
`pkg/agent/computer_use_registration.go:180` is reset so the user must
explicitly re-consent. The reset is implemented in `Halt()`.

### Chosen approach

- **Reuse `TriggerInterrupt()`** from `pkg/agent/pause.go:21` — no dedicated
  fast path. The existing mechanism is proven and propagates correctly through
  tool timeouts at `pkg/agent/tool_executor_sequential.go:128`.
- **Parallel subprocess kill** via `PanicableBackend.Halt()` ensures the
  subprocess is killed independently of the loop cancellation.
- **Fresh opt-in consent** required after panic — `computerUseSessionApproved`
  is reset so the user must explicitly re-consent before computer use resumes.
- **User feedback**: TUI prints halt message, WebUI shows toast notification.

### Open questions

- Should the panic key also clear the seed library's internal message buffer?
  (Current answer: no — the messages are preserved so the user can continue
  the conversation. The seed library's `Run()` returns cleanly on context
  cancellation.)
- Should there be a "panic key cooldown" (e.g., 5 seconds between triggers)?
  (Current answer: no — the idempotent halt flag handles double-triggers,
  and a cooldown would add unnecessary complexity.)

---

## (d) How is the panic-key event audited?

The audit log at `pkg/agent_tools/computer_use/audit.go:15-20` uses JSONL
format with `AuditRecord{Time, Action, Args, Err}`. The `RecordSafetyEvent()`
helper at `audit.go:88` is the pattern we reuse.

### New audit event types

We introduce three new action types, all emitted via `RecordSafetyEvent()`:

#### 1. `"panic_key_triggered"`

Recorded when the panic key is first pressed.

**Args**: `reason` ("panic_key" or "parent_exit"), `source` ("tui" or "webui"),
`action_index` (0-based, -1 if none), `elapsed_ms` (since last action started).

**Where emitted**: `PanicableBackend.Halt()` when the `halted` flag is set.

#### 2. `"panic_key_duplicate"`

Recorded on double-trigger (panic key pressed while already halted).

**Args**: `reason`, `original_halt_at`, `duplicate_trigger_at` (both RFC3339).

**Where emitted**: `PanicableBackend.Halt()` early return when `p.halted` is true.

#### 3. `"panic_key_reset"`

Recorded when the user acknowledges the halt and resets the panic state.

**Args**: `halt_duration_ms`, `halt_reason`.

**Where emitted**: `PanicableBackend.Reset()`.

### Emission points

| Event | Emitted in | Code location |
|-------|-----------|---------------|
| `panic_key_triggered` | `PanicableBackend.Halt()` | `pkg/agent_tools/computer_use/panic_key.go` |
| `panic_key_duplicate` | `PanicableBackend.Halt()` (early return) | Same file |
| `panic_key_reset` | `PanicableBackend.Reset()` | Same file |

All three use `RecordSafetyEvent()` at `audit.go:88`. When the backend is a
`MockBackend` (tests), the call is a no-op. Events are appended to the same
JSONL file as regular actions — no separate log file. The `auditingBackend`
at `audit.go:25` handles file creation and rotation.

### Chosen approach

- **Three new action types**: `"panic_key_triggered"`, `"panic_key_duplicate"`,
  `"panic_key_reset"`.
- **Emission via `RecordSafetyEvent()`** at `audit.go:88` — same pattern as
  opt-in consent events.
- **Args shape** includes reason, source (TUI/WebUI), action index, and elapsed
  time for the primary event.
- **No changes to `AuditRecord` struct** — the existing `Args` map is sufficient.
- **No separate log file** — events are appended to the same JSONL file.

### Open questions

- Should the panic-key event include a screenshot of the screen at the time
  of the halt? (Current answer: no — screenshots are already recorded by the
  `"screenshot"` action type, and adding one on halt would increase log size
  and potentially capture sensitive content.)
- Should the audit log include the LLM's last thought/reasoning before the
  halt? (Current answer: no — that's in the session log, not the audit log.
  The audit log is for actions, not reasoning.)

---

## Implementation Notes

### Files created/modified

| File | Change |
|------|--------|
| `pkg/agent_tools/computer_use/panic_key.go` | **New** — `PanicableBackend` decorator, `Halt()`, `Reset()`, `ErrPanicKeyHalted` |
| `pkg/agent_tools/computer_use/backend_subprocess.go` | **Modified** — `run()` → `runWithCtx()`, context-aware `exec.CommandContext` + `SetProcessGroup()` |
| `pkg/agent_tools/computer_use/process_group_unix.go` | **New** — Exported `SetProcessGroup()`/`KillProcessGroup()` moved from `background_process_signal_unix.go` |
| `pkg/agent/computer_use_registration.go` | **Modified** — Insert `PanicableBackend` in decorator chain, register signal handler |
| `pkg/configuration/config_domain.go` | **Modified** — Add `PanicKeyChord` field to `ComputerUseConfig` |
| `pkg/console/` | **Modified** — Watch for Ctrl+Shift+Escape chord |
| `pkg/webui/` | **Modified** — Add keyboard shortcut for `/api/query/stop` |

### Testing

Key scenarios: mock-backend unit test (Halt kills in-flight process), full decorator chain integration test, idempotency (double-trigger), reset after halt, parent-exit SIGTERM cleanup, and regression (normal actions unchanged).

### Rollout

Phase 1: `PanicableBackend` decorator + process-group wrapping (core safety). Phase 2: TUI chord + WebUI shortcut. Phase 3: Audit events + signal handler.
