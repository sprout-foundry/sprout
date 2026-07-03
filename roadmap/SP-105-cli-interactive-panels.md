# SP-105: CLI Interactive Panels — Settings Browser & Usage Dashboard

**Status:** 🔵 Proposed

The CLI REPL has a persistent status footer (model · context · cost) and a
`/stats` text dump, but no way to browse or change settings without dropping
to `sprout config show` (a redacted JSON blob) or opening the WebUI. This spec
adds two interactive slash commands that close the gap:

- **`/settings`** — a navigable settings panel using the existing `ask_user`
  interaction model. Arrow-key browsing, enter-to-change, immediate persistence.
- **`/usage`** — a formatted usage dashboard that replaces the plain-text
  `/stats` dump with visual bars for context window fill, cache efficiency,
  and per-turn cost.

Both are additive: new slash commands and rendering code, no changes to
existing agent or configuration logic. They reuse the existing
`getConfigValue`/`setConfigValue` validation and `Manager.UpdateConfig`
persistence path, plus the `Agent.state` getters that `/stats` already uses.

## Problem

1. **Settings are invisible in the CLI.** The only CLI config command is
   `sprout config show` — a full JSON dump, credential-redacted but
   otherwise unstructured. There's no `config get <key>` or `config set
   <key> <value>`. The help text in `cmd/github_setup_prompt.go` tells users
   to run `sprout config set dismissed_prompts.github_mcp_setup false` — a
   command that doesn't exist.

2. **The agent has full settings management but the user can't trigger it.**
   The `manage_settings` tool (`pkg/agent/settings_handler.go`) supports
   get/set/describe/preview/list_providers — but only reachable through the
   agent itself (natural language request) or the WebUI. A user typing
   `/settings` in the CLI gets "unknown command."

3. **Usage info is a text dump, not a dashboard.** `/stats` prints aligned
   columns of numbers (`Iterations: 12`, `Total: 50.2k`, `Cost: $0.084`).
   Useful data, poor presentation. No visual indication of how full the
   context window is, how much cache is saving, or cost trajectory.

## Design

### `/settings` — Interactive Settings Browser

**Entry point:** `/settings` slash command (new file
`pkg/agent_commands/settings_cmd.go`).

**Interaction model:** Uses `tools.AskUser` (the same `ask_user`
infrastructure the agent uses mid-turn). This is already wired into the CLI
with TTY detection, spinner suspension, and steer-input pausing. No new
terminal rendering infrastructure needed.

**Panel rendering:** The command builds an `AskUserRequest` with the
question being a formatted settings summary and the options being the
available settings keys. When the user selects a key, a second `AskUser`
prompt offers either a value picker (for enum settings like
`reasoning_effort`) or a freeform input (for string settings like `model`).

```
─────────────────────────────────────────
  Settings
─────────────────────────────────────────
  Provider:          anthropic
  Model:             claude-sonnet-4-20250514
  Subagent provider: anthropic
  Subagent model:    claude-haiku-4-5
  Reasoning effort:  medium
  Thinking:          enabled
  EA mode:           interactive
  History scope:     project
  Output verbosity:  default

  Select a setting to change (q to quit)
─────────────────────────────────────────
```

**Settings covered:** All 12 keys already supported by
`getConfigValue`/`setConfigValue`:
`provider`, `model`, `reasoning_effort`, `disable_thinking`,
`resource_directory`, `history_scope`, `ea_mode`, `subagent_provider`,
`subagent_model`, `default_subagent_persona`, `disabled_personas`,
`output_verbosity`.

**Value pickers for enum settings:**
- `reasoning_effort` → options: low, medium, high
- `disable_thinking` → options: enabled, disabled
- `history_scope` → options: project, global
- `ea_mode` → options: interactive, queue
- `output_verbosity` → options: compact, default, verbose
- `provider` → options: `Manager.GetAvailableProviders()`
- `model` → freeform input (validated against provider model list if available)
- `subagent_provider` → same as provider

**Persistence:** Uses `Manager.UpdateConfig(func(cfg *Config) error)` —
the same path the agent's `manage_settings` tool uses. Atomic, locked,
persists to disk immediately.

**Non-TTY fallback:** When `AskUser` returns `ErrAskUserNoChannel` (no TTY),
the command falls back to printing the settings summary as text (same output
as the panel body, minus the interactive prompt).

### `/usage` — Usage Dashboard

**Entry point:** `/usage` slash command (new file
`pkg/agent_commands/usage_cmd.go`). The existing `/stats` command remains
as an alias for backward compat.

**Panel rendering:** A formatted text panel with Unicode bar charts. Uses
the existing `pkg/console/glyphs.go` and `pkg/console/display_width.go`
utilities for terminal-width-aware formatting.

```
Session Usage                    claude-sonnet-4 · 12 turns
──────────────────────────────────────────────────────────────────
 Context    ████████░░░░░░░░  47.2k / 200k   (23.6%)
 Cached     ██████████░░░░░░  31.4k / 47.2k  (66.5% reused)

 Tokens     Prompt: 47.2k    Completion: 3.1k    Cache write: 12.0k
 Cost       $0.084712        ($0.007/turn)

 Cache savings  $0.031241    Efficiency: Excellent (66.5% cached)
──────────────────────────────────────────────────────────────────
```

**Data source:** Same `Agent.state` getters as `/stats`:
`GetTotalTokens`, `GetPromptTokens`, `GetCompletionTokens`,
`GetCachedTokens`, `GetCacheWriteTokens`, `GetCurrentContextTokens`,
`GetMaxContextTokens`, `GetTotalCost`, `GetCurrentIteration`,
`GetCachedCostSavings`.

**Bar chart logic:** Width-proportional fill based on terminal width (capped
at 16 segments). Context bar shows `current / max`. Cache bar shows
`cached / prompt`. No external dependencies — just string multiplication.

**Empty state:** If no tokens have been consumed yet (fresh session),
prints a friendly message instead of empty bars.

## Key decisions

- **No new terminal rendering framework.** The settings browser uses
  `AskUser` (already battle-tested in CLI + daemon modes). The usage
  dashboard is formatted text with Unicode bars — same approach as the
  status footer, no new dependencies.
- **`/settings` reuses the agent's get/set logic.** `getConfigValue` and
  `setConfigValue` are currently unexported in `pkg/agent`. Rather than
  duplicate them, export minimal wrappers (`GetSettingValue`,
  `SetSettingValue`) that the slash command calls. One validation path,
  one persistence path.
- **`/stats` stays as an alias.** Existing users and scripts that parse
  `/stats` output won't break. `/usage` is the new visual surface; `/stats`
  is the plain-text fallback.
- **Settings panel is read-then-write, not write-as-you-go.** Each setting
  change is an atomic `UpdateConfig` call. No batch mode — the user changes
  one thing at a time, sees confirmation, returns to the menu.
- **No config-key auto-discovery.** The 12 supported keys are explicitly
  listed, matching the existing `getConfigValue`/`setConfigValue` switch.
  Adding a new setting requires updating both the switch and the panel list;
  this is deliberate to avoid surfacing internal/debug keys.

## Phasing

### Phase 1 — Export settings accessors (foundation)

Extract the get/set logic from `pkg/agent/settings_handler.go` into exported
functions that both the agent tool and the CLI command can call. No behavior
change; the agent tool delegates to the new exports.

**Files:**
- `pkg/agent/settings_handler.go` — export `GetSettingValue`,
  `SetSettingValue`, `SupportedSettingKeys`
- `pkg/agent/settings_handler_test.go` — verify exports work from outside
  the package

### Phase 2 — `/settings` slash command

Implement the interactive settings browser.

**Files:**
- `pkg/agent_commands/settings_cmd.go` — new command, registers as
  `settings` in the command registry
- `pkg/agent_commands/settings_cmd_test.go` — tests for panel rendering,
  enum pickers, non-TTY fallback, persistence

### Phase 3 — `/usage` slash command

Implement the usage dashboard.

**Files:**
- `pkg/agent_commands/usage_cmd.go` — new command, registers as `usage`
- `pkg/agent_commands/usage_cmd_test.go` — tests for bar rendering, empty
  state, edge cases (zero tokens, missing context limit)

## Success Criteria

- `make build-all` clean.
- `go test ./pkg/agent_commands/... ./pkg/agent/...` green.
- `/settings` in the CLI shows all 12 supported settings with current values.
- `/settings` → select `reasoning_effort` → pick `high` → value persisted and
  reflected in the status footer.
- `/settings` on a non-TTY (piped stdin) prints the settings summary without
  crashing.
- `/usage` renders a dashboard with bar charts on a TTY.
- `/usage` on a fresh session (zero tokens) prints an empty-state message.
- `/stats` still works unchanged (alias compat).
- The agent's `manage_settings` tool still works unchanged (same validation).

## Out of Scope

- **WebUI changes.** The WebUI already has full settings and cost panels.
- **New settings keys.** This is a presentation layer; it surfaces existing
  keys only.
- **`sprout config get/set` CLI subcommand.** This spec is about the
  interactive REPL panels. A non-interactive `config get/set` is a separate
  (smaller) follow-up if desired.
- **Settings search/filter.** 12 keys is small enough to show in one screen.
- **Usage history across sessions.** This shows current-session data only,
  same as `/stats`. Cross-session analytics live in the WebUI Costs page
  (SP-085).
