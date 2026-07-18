# SP-114 — Unify CLI and Steer Panel Command Execution

**Status:** ✅ Phase 1 Complete — Phase 2 complete (2026-07-17). Phase 1: `SteerCapable` interface + 31-command classification + CLI/WebUI steer allowlist (`ab6c975e`). Phase 2: `POST /api/command/execute` (`pkg/webui/api_command.go`) reuses `executeSafeSteerCommand` for stdout-capture; WebUI `onSendCommand` handler in `ChatView.tsx` routes safe commands via `notificationBus`. Long-output WebSocket streaming (§2c) deferred to a follow-up.
**Created:** 2026-07-04
**Phase 1 Completed:** 2026-07-17
**Effort:** Phase 1 (~2 days), Phase 2 (~2-3 days)

> **Note:** Renumbered from SP-112 (2026-07-05) to resolve a number collision with
> SP-112-platform-parity. Both specs were created 2026-07-04 with the same number.

## Problem

Today the CLI has a rich slash-command system (~31 commands: `/commit`, `/info`,
`/codegraph`, `/model`, `/review`, etc.) but there is **no way to run them from
the steer panel** or from the WebUI. When a user submits a slash command via the
CLI steer panel (Ctrl+Enter mid-turn) or via the WebUI steer endpoint
(`POST /api/query/steer`), it is **explicitly rejected**:

- **CLI steer**: `ClassifyPromptIntent` → `rejectCommandIntent("steer mode can't run a /command...")`
- **WebUI steer**: `api_query.go:508` → `"Slash commands cannot be steered while a query is running"`

This forces users to wait for the current turn to finish before running commands,
breaking flow for fast read-only operations like `/info`, `/codegraph stats`,
`/model`, or `/help`.

Some commands (like `/commit`, `/clear`, `/exit`) should NOT run mid-turn because
they mutate state that the active query depends on. But read-only commands and
most config commands are safe.

## Goal

Allow a subset of slash commands to execute from any surface — CLI steer panel,
WebUI steer endpoint, and the regular prompt — without blocking or conflicting
with an active agent turn.

## Non-Goals

- **Execute ALL commands mid-turn** — destructive commands (`/commit`, `/clear`,
  `/exit`, `/init`) remain prompt-only.
- **WebUI command-palette replacement** — existing Ctrl+K palette is for files
  and symbols, not slash commands. A WebUI command input is a new surface.
- **Full WebUI terminal emulation** — the steer endpoint is for commands, not
  general shell access.

## Background: Current Architecture

### CLI Command Registry (`pkg/agent_commands/`)

Every command implements:

```go
type Command interface {
    Name() string
    Description() string
    Execute(args []string, chatAgent *agent.Agent) error
}
```

Registered in `NewCommandRegistry()` — commands auto-register by calling
`registry.Register(&MyCommand{})`. Aliases like `/h` → `/help` delegate
transparently.

### Steer Submission Flow (CLI)

```
User types `/info` in steer panel (Ctrl+Enter)
  → SteerCoordinator.handleSteerSubmit(text)
    → ClassifyPromptIntent(text) → IntentSlash
    → rejectCommandIntent() → "steer mode can't run a /command"
```

### WebUI Steer Flow

```
User sends POST /api/query/steer { query: "/info" }
  → handleAPIQuerySteer
    → strings.HasPrefix("/", query) → 400 "can't steer slash commands"
```

### Intent Detection (`cmd/prompt_intent.go`)

```go
func ClassifyPromptIntent(chatAgent *agent.Agent, text string) PromptIntent
```
Returns `IntentSlash`, `IntentBangShell`, `IntentDetectedSh`, or `IntentNone`.
Used by both the REPL's main dispatch and the steer/queue interceptors.

### REPL Dispatch (`cmd/agent_query.go`)

The REPL loop calls `ProcessQuery` which:
1. Calls `agentCommands.IsSlashCommand(text)` to check if the text is a registered command
2. If yes, calls `agentCommands.Execute(text)` instead of sending to the LLM
3. If no, sends to the LLM provider

## Phase 1 — Command Classification & Steer Allowlist (~1-2 days)

### 1a. Classify every command as SafeForSteer or PromptOnly

Add a new interface:

```go
// SteerCapable is implemented by commands that can safely run mid-turn
// while an agent query is in progress. Commands that don't implement this
// are treated as PromptOnly.
type SteerCapable interface {
    // SafeDuringSteer returns true if this command is safe to run while
    // an agent query is active. Read-only commands and config commands
    // that don't interact with the active turn should return true.
    SafeDuringSteer() bool
}
```

Commands that implement `SafeDuringSteer() bool`:

| Command | SafeDuringSteer | Reason |
|---|---|---|
| `/info` | ✅ true | Read-only — shows agent state |
| `/codegraph` | ✅ true | Read-only (stats) or long-running (build/update, runs independently) |
| `/model` | ✅ true | Changes model for next turn, doesn't affect current turn |
| `/provider` | ✅ true | Same as /model |
| `/subagent-provider` | ✅ true | Config for next turn only |
| `/subagent-model` | ✅ true | Config for next turn only |
| `/persona` | ✅ true | Changes persona for next turn |
| `/help` | ✅ true | Read-only |
| `/usage` | ✅ true | Read-only token/cost peek |
| `/status` | ✅ true | Read-only git status |
| `/changes` | ✅ true | Read-only change list |
| `/log` | ✅ true | Read-only log view |
| `/mcp` | ✅ true | Server list is read-only; add/remove reject if query active |
| `/risk-profile` | ✅ true | Config change, no turn interaction |
| `/max-context` | ✅ true | Config change, no turn interaction |
| `/setup` | ❌ false | Interactive config wizard — uses stdin prompts, unsafe mid-turn |
| `/settings` | ❌ false | Interactive settings browser — deadlocks if run from goroutine |
| `/skill` | ✅ true | Skill management is independent of active turn |
| `/compact` | ❌ false | Mutates conversation state (SetMessages, ReplaceTurnCheckpoints) and calls LLM |
| `/recall` | ✅ true | Read-only memory recall |
| `/sessions` | ❌ false | Session lifecycle, too risky mid-turn |
| `/commit` | ❌ false | Git mutation + agent interaction |
| `/clear` | ❌ false | Destroys conversation state |
| `/exit` | ❌ false | Terminates session |
| `/init` | ❌ false | Project init — long-running, state-changing |
| `/shell` | ❌ false | Shell command — runs on agent's CWD |
| `/exec` | ❌ false | Direct execution — bypasses agent |
| `/edit` | ❌ false | File mutation — conflicts with agent |
| `/review` | ❌ false | Reads file state, conflicts with agent writes |
| `/review-deep` | ❌ false | Same |
| `/rollback` | ❌ false | Mutates file history |
| `/rewind` | ❌ false | Conversation state mutation |
| `/search` | ✅ true | Read-only search |
| `/index` | ❌ false | Index lifecycle — may interfere with agent's embedding queries |
| `/transcript` | ✅ true | Read-only transcript dump |

### 1b. Update steer interceptors to allow SafeForSteer commands

**CLI steer** (`cmd/steer_coordinator.go`):

In `handleSteerSubmit`, instead of unconditionally rejecting:

```go
func (c *SteerCoordinator) handleSteerSubmit(text string) {
    if c.agent == nil { return }
    
    // Check if it's a command that can execute mid-turn.
    if intent := ClassifyPromptIntent(c.agent, text); intent != IntentNone {
        if intent == IntentSlash {
            // Extract command name from "/command args..."
            parts := strings.Fields(strings.TrimPrefix(text, "/"))
            if len(parts) > 0 {
                cmdName := parts[0]
                if cmd, ok := c.agent.SlashCommands().GetCommand(cmdName); ok {
                    if steerable, ok := cmd.(SteerCapable); ok && steerable.SafeDuringSteer() {
                        // Execute in a goroutine, route output to stderr
                        go func() {
                            if err := cmd.Execute(parts[1:], c.agent); err != nil {
                                console.GlyphError.Fprintf(os.Stderr, "command /%s: %v", cmdName, err)
                            }
                        }()
                        return
                    }
                }
            }
        }
        // Reject non-steerable commands
        rejectCommandIntent(intent, text, "steer", "wait for the prompt to finish")
        return
    }
    
    // ... existing steer injection logic ...
}
```

**WebUI steer** (`pkg/webui/api_query.go`):

Similarly, instead of rejecting all `/` prefixed queries, check if the command
is `SafeDuringSteer` and if so, run it through the command registry instead of
injecting it into the agent's input stream. Return the command output to the
WebSocket stream as a system message or dedicated event.

**Queue mode** (`handleQueueSubmit`): Allow same set of SafeForSteer commands.
Commands that mutate config (`/model`, `/provider`, `/persona`) should still
work in queue mode since they affect the *next* turn, not the current one.

### 1c. Add `SlashCommands()` accessor to Agent

```go
// SlashCommands returns the command registry for slash commands.
func (a *Agent) SlashCommands() *commands.CommandRegistry
```

The agent needs to hold a reference to the command registry (set during
construction or lazily initialized). Currently the registry is created in
`cmd/agent_command.go` and passed to the REPL loop, not stored on the Agent.

**Option A**: Store the registry on the Agent struct. Set it via a setter
during CLI construction. WebUI agents won't have one (they don't run the CLI).

**Option B**: Create a new registry for every Agent. Lightweight enough
(31 commands), no shared state.

## Phase 2 — WebUI Command Input (~2-3 days)

Expose safe commands in the WebUI so users can run them without switching to
the terminal.

### 2a. Dedicated command endpoint

```
POST /api/command { command: "/info", args: [] }
```

Separate from the steer endpoint. Returns `{ output: "...", error: "" }`.
The handler:
1. Parses the command name and args
2. Looks up in the command registry
3. Checks `SafeDuringSteer()`
4. Executes via the agent's command registry
5. Returns output as JSON

The provider-query steer endpoint (`/api/query/steer`) keeps the existing
behavior: it's for steering the active LLM query, not for running commands.
The new `/api/command` endpoint is the command surface.

### 2b. WebUI command input

A dedicated command input in the sidebar or a command bar (think VS Code
Ctrl+Shift+P but for sprout commands):

- `/` triggers command autocomplete showing the available commands
- Each command can show its description and usage inline
- Results (from `/info`, `/codegraph stats`, `/usage`) appear in a toast or
  inline output panel
- Visual distinction: a command input is different from the chat input

### 2c. Command output routing

Commands produce text output via `fmt.Fprint(os.Stdout)`. For the WebUI, this
output needs to be captured and routed:

- **Short output** (one line like `/info`, `/help`): display inline in a
  notification or result bubble
- **Long output** (`/changes` with many files, `/codegraph dead`): stream via
  the existing WebSocket event channel as a system message
- **Interactive commands** (`/commit`, `/settings`): remain CLI-only; WebUI
  shows a "This command is not available in the WebUI" message

### 2d. Security

The WebUI command endpoint must respect the same permission model as the CLI:
- Commands that are `SafeDuringSteer()` are always available
- Commands that are not safe are rejected with a clear message
- The endpoint checks agent state (is a query running?) before dispatching

## Dependencies

- `pkg/agent_commands/commands.go` — Command interface (existing)
- `cmd/steer_coordinator.go` — CLI steer interception (existing)
- `pkg/webui/api_query.go` — WebUI steer handling (existing)
- `pkg/webui/routes.go` — WebUI route registration (existing)

## Acceptance Criteria

**Phase 1:**
- `/info` and `/codegraph stats` run from the CLI steer panel without error
- `/commit` and `/clear` are still rejected from the steer panel
- No regression in regular steer injection (non-command text)
- Queue mode allows safe commands, rejects destructive ones

**Phase 2:**
- `/api/command` endpoint exists and returns command output
- WebUI has a visible command input (sidebar or command bar)
- Running `/info` from WebUI shows the agent overview
- Running `/commit` from WebUI shows "not available" message
- Command autocomplete shows available commands with descriptions

---

## Review Findings

Review by: reviewer persona (2026-07-04)

### 🔴 MUST_FIX — Critical gaps

1. **Output capture**: Every command writes to `os.Stdout` via `fmt.Print`/`fmt.Fprintln`.
   The `Command` interface has no `io.Writer` parameter. The WebUI `/api/command` handler
   has no way to capture output. Fix: add `SetOutput(w io.Writer)` to `Command` interface
   (or use `ExecuteContext` pattern).

2. **Goroutine safety**: The proposed CLI steer code fires a goroutine that calls
   `cmd.Execute()` while the agent is mid-turn. Every method called by safe commands
   must use `RLock`/`RUnlock`, not `Lock`/`Unlock`. Mandatory review step required before
   granting `SafeDuringSteer()`.

3. **Registry allocation storm**: `ClassifyPromptIntent` creates `NewCommandRegistry()`
   on every call (31+ allocations per steer/queue submit). Fix: add `DefaultRegistry()`
   with `sync.Once` in `pkg/agent_commands`.

4. **Dual dispatch surface in WebUI**: Both `/api/query` AND the proposed `/api/command`
   handle slash commands. Recommend removing slash command handling from `/api/query`
   in favor of the single `/api/command` endpoint.

### 🟡 SHOULD_FIX

5. **Queue mode prefix**: `DrainPendingInput` wraps queued messages in a blockquote,
   stripping the `/` prefix. Queued safe commands won't be recognized by the REPL.
6. **`!bang` commands**: Not discussed. Currently rejected via fallthrough — fine,
   but should be documented.
7. **`/search` vs `/index` inconsistency**: Both use the embedding manager. Needs
   verification that `/search` is truly read-only with RLock semantics.

### 🟢 SUGGEST

8. **`ExecuteContext` pattern**: Replace `Execute(args, agent)` with a context struct
   including `Output io.Writer` for forward-compatibility.
9. **Document `SteerCapable`** in the `Command` interface godoc.
10. **WebUI error format**: Define structured JSON error output for `/api/command`.
