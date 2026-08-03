# SP-131 — Agent Creation & Event Publishing Dedup

## Problem

Agent creation and event publishing follow copy-pasted patterns across CLI,
WebUI, and workflow call sites. Each duplication instance has already caused
at least one bug (missing slash-command registration on webui agents), and
the patterns continue to diverge as new call sites are added.

### Symptom 1: Agent setup boilerplate is copy-pasted

Every code path that creates an agent runs the same sequence of setter calls:

```go
created.SetEventBus(eventBus)
created.SetSlashCommands(agent_commands.NewCommandRegistry())
created.SetWorkspaceRoot(workspaceRoot)
```

This appears in at least 4 locations:
- `pkg/webui/chat_sessions.go:229-232`
- `pkg/webui/client_context.go:551-553`
- `pkg/webui/git_api_review.go:363` (partial — missing `SetSlashCommands`)
- `pkg/webui/sessions_api.go:158` (partial — only `SetWorkspaceRoot`)

Each new call site risks omitting a setter (as happened with `SetSlashCommands`
on webui agents — fixed in `4ae74bcc1` but only after it caused a bug).

### Symptom 2: Event metadata decoration is manually reimplemented

The `client_id` / `chat_id` decoration pattern was copy-pasted 4× in
`cmd/agent_query.go`:

```go
if clientID := chatAgent.GetEventClientID(); clientID != "" {
    event["client_id"] = clientID
}
if chatID := chatAgent.GetEventChatID(); chatID != "" {
    event["chat_id"] = chatID
}
eventBus.Publish(eventType, event)
```

The agent already has `publishEvent()` (private) and `PublishEvent()` (public)
that do this decoration internally via `decorateEventPayload`. The manual
copies were redundant and missed additional metadata keys (`user_id`,
`subagent_depth`, `active_persona`).

**Status:** Resolved in `38984fd99` — all 4 blocks replaced with
`chatAgent.PublishEvent()` calls. This spec tracks preventing regression and
addressing the remaining duplication.

### Symptom 3: Command registry instantiation is scattered

`NewCommandRegistry()` was called from 7 sites. Some are read-only (safe to
share via `DefaultRegistry()` singleton); others mutate state via `SetOutput()`
and must keep per-instance creation. The distinction was not documented and
led to confusion.

**Status:** Partially resolved in `e0eb0c7f4` — read-only CLI paths switched
to `DefaultRegistry()`. The singleton-vs-instance boundary is not codified.

## Goals

1. **Single agent-setup function.** One `ConfigureAgent(agent, opts)` helper
   that applies all standard setters (EventBus, SlashCommands, WorkspaceRoot,
   OutputRouter, etc.). New call sites call the helper instead of copy-pasting
   individual setters.

2. **Event publishing goes through `PublishEvent`.** No call site outside
   `pkg/agent` calls `eventBus.Publish` directly. All events route through
   `agent.PublishEvent()` so metadata decoration is automatic and consistent.

3. **Registry instantiation boundary documented.** A clear rule for when to
   use `DefaultRegistry()` (read-only: IsSlashCommand checks, completion) vs
   `NewCommandRegistry()` (mutating: Execute, SetOutput). Codified as a
   comment on the constructor or a lint-level convention.

4. **Regression prevention.** A test that asserts every agent creation path
   (CLI interactive, direct, query, webui chat session, webui client context,
   webui git review) has the full set of setters applied.

## Non-Goals

- Refactoring provider config resolution (229 references — too deeply embedded;
  tracked separately if needed).
- Refactoring tool registration (45 instances — already somewhat centralized
  through the registry pattern).
- Changing the circular-dependency constraint (`pkg/agent` cannot import
  `pkg/agent_commands`).

## Completed Work

| Commit | Change |
|--------|--------|
| `4ae74bcc1` | Wire `SetSlashCommands` into webui agent creation paths |
| `e0eb0c7f4` | Use `DefaultRegistry()` singleton for read-only CLI paths |
| `38984fd99` | Dedup event decoration via `PublishEvent`, rename from `PublishBudgetUpdate` |

## Remaining Work

### Phase 1: `ConfigureAgent` helper

Extract the common agent-setup sequence into a helper. Lives in `pkg/webui/`
or a new shared package (not `pkg/agent` — circular dependency). Signature:

```go
type AgentConfig struct {
    EventBus       *events.EventBus
    WorkspaceRoot  string
    // SlashCommands defaults to NewCommandRegistry() if nil
    SlashCommands  *agent_commands.CommandRegistry
    EventMetadata  map[string]interface{}
}

func ConfigureAgent(a *agent.Agent, cfg AgentConfig)
```

Migrate the 4+ webui call sites to use it.

### Phase 2: Eliminate direct `eventBus.Publish` calls

Audit all `eventBus.Publish` calls outside `pkg/agent/`. Each should route
through `agent.PublishEvent()` instead. Key areas to check:
- `pkg/webui/api_query_shared.go` (slash-command dispatch events)
- `pkg/workflow/runner.go` (budget events — already migrated in `38984fd99`)

### Phase 3: Regression test

Add a test that creates agents through each public creation path and asserts:
- `SlashCommands()` is non-nil
- `EventBus` is wired
- `WorkspaceRoot` is set
- Events published via the agent carry `client_id` / `chat_id` metadata
