# SP-013: Agent Settings Management Tool

**Status:** ✅ Implemented (manage_settings tool registered; pkg/agent/settings_handler.go)
**Depends on:** SP-003
**Priority:** Medium
**Effort Estimate:** ~2-3 days

## Problem

Users frequently ask the agent things like:
- "Change my model to claude-sonnet-4-20250514"
- "Switch to OpenAI"
- "Turn on PDF OCR"
- "Set my commit provider to deepseek"
- "What model am I using?"
- "Does my OpenAI key work?"

The agent has **no tool to read or modify configuration**. It can only tell users to open Settings or edit config files manually.

## Design Principles

1. **No skill needed** — Settings management is only relevant when the user asks. Unlike `project-planning` which is useful at project start, settings are an occasional request. A skill would waste context on 99% of conversations. The tool is self-documenting instead.

2. **Self-documenting returns** — Invalid keys, invalid values, and unknown operations all return actionable guidance. The agent learns by calling, not by reading skill docs.

3. **Useful over terse** — Every return should give the agent enough information to act without a follow-up call. If `set` changed something, confirm what changed and what the agent should consider doing next. If `get` returns a key, include its type and valid values when relevant. Don't pad, don't dump, but don't be stingy either.

## Proposed Solution

### Tool: `manage_settings`

```go
registry.RegisterTool(ToolConfig{
    Name:        "manage_settings",
    Description: "Read and modify Sprout agent configuration (provider, model, features). Use 'get' to inspect, 'set' to change, 'list_providers' to see available providers, or 'test_credential' to validate an API key.",
    Parameters: []ParameterConfig{
        {"operation", "string", true, nil, "get | set | list_providers | test_credential"},
        {"key", "string", false, nil, "Setting key for get/set (e.g. 'provider', 'model', 'pdf_ocr_enabled')"},
        {"value", "string", false, nil, "New value for 'set' operations"},
        {"provider", "string", false, nil, "Provider name for test_credential"},
    },
    Handler: handleManageSettings,
})
```

The tool definition is intentionally minimal — 4 short parameter descriptions. All guidance lives in the return values.

### Operations

#### `get` — Read Configuration

**Without a key:** returns a summary of the current configuration that covers what users most commonly ask about.

```
manage_settings(operation="get")

→ Current configuration:
  Provider: openai
  Model: gpt-4o
  Reasoning effort: medium (valid: low, medium, high, "" for auto)
  Disable thinking: false
  Self-review gate: code (valid: off, code, always)
  PDF OCR: enabled, provider=ollama, model=llama3.2-vision
  Subagents: parallel enabled, max 2, provider=openai, model=gpt-4o-mini
  Commit messages: provider=deepseek, model=deepseek-chat
  Code review: provider=(inherits), model=(inherits)
  Zsh command detection: false
  Provider models:
    openai → gpt-4o
    deepseek → deepseek-chat
    ollama → llama3.2
```

Note: type and valid-value hints are included inline for enum/bool fields so the agent knows what it can change without a second call.

**With a key:** returns the value plus type context.

```
manage_settings(operation="get", key="provider_models")
→ provider_models (map of provider name → model name):
  openai → gpt-4o
  deepseek → deepseek-chat
  ollama → llama3.2
```

```
manage_settings(operation="get", key="subagent_max_parallel")
→ subagent_max_parallel: 2 (int, valid: 1–8)
```

**Invalid key:** returns guidance with the full list of valid keys.

```
manage_settings(operation="get", key="foo")
→ Unknown key "foo". Valid keys:
  provider, model — switch live agent (session only)
  last_used_provider — default provider (persisted)
  provider_models.{name} — model per provider (persisted)
  provider_priority — provider fallback order (persisted, JSON array)
  reasoning_effort — low|medium|high|"" (persisted)
  disable_thinking — true|false (persisted)
  self_review_gate_mode — off|code|always (persisted)
  pdf_ocr_enabled, pdf_ocr_provider, pdf_ocr_model (persisted)
  subagent_max_parallel, subagent_parallel_enabled, subagent_provider, subagent_model (persisted)
  commit_provider, commit_model, review_provider, review_model (persisted)
  enable_zsh_command_detection, auto_execute_detected_commands (persisted)
  resource_directory, system_prompt_text (persisted)
  Use 'get' without a key to see current summary.
```

This is longer but the agent only sees it once — when it guesses wrong. After that it knows the keys. And it's complete enough that no follow-up is needed.

#### `set` — Change a Setting

All values are strings. The tool coerces automatically (`"true"` → bool, `"4"` → int).

**Successful set:** confirm what changed, note persistence behavior.

```
manage_settings(operation="set", key="provider", value="openai")
→ ✅ Provider switched to openai.
  This is a session override — it will reset when the conversation ends.
  To persist across sessions, also set: last_used_provider=openai
  Live agent is now using openai.
```

```
manage_settings(operation="set", key="pdf_ocr_enabled", value="true")
→ ✅ pdf_ocr_enabled set to true (saved to global config).
  PDF OCR is now enabled. Current OCR provider: ollama, model: llama3.2-vision.
  To change the OCR model: set pdf_ocr_provider or pdf_ocr_model.
```

```
manage_settings(operation="set", key="subagent_max_parallel", value="4")
→ ✅ subagent_max_parallel changed from 2 to 4 (saved to global config).
```

**Invalid value:** explain what went wrong and list valid options.

```
manage_settings(operation="set", key="reasoning_effort", value="maybe")
→ Invalid value "maybe" for reasoning_effort (string).
  Valid values: low, medium, high, "" (auto)
```

```
manage_settings(operation="set", key="subagent_max_parallel", value="0")
→ Invalid value "0" for subagent_max_parallel (int).
  Must be between 1 and 8.
```

**`provider` and `model` are special:** they switch the live agent immediately (session override, not persisted). The return value always explains the session-vs-persist distinction.

**All other keys** persist to global config (`~/.config/sprout/config.json`).

#### `list_providers` — Show Available Providers

```
manage_settings(operation="list_providers")

→ Available providers:
  openai       ✅ has API key   model: gpt-4o
  deepseek     ✅ has API key   model: deepseek-chat
  openrouter   ✅ has API key   model: (default)
  deepinfra    ✅ has API key   model: (default)
  ollama       (local)          model: llama3.2
  lmstudio     (local)          model: (default)
  ollama-turbo (local)          model: (default)
  mistral      ❌ no API key    model: (default)
  minimax      ❌ no API key    model: (default)
  chutes       ❌ no API key    model: (default)
  cerebras     ❌ no API key    model: (default)
  zai          ❌ no API key    model: (default)
  ✅ = has API key stored, ❌ = no API key, (local) = runs locally
```

One line per provider. Shows credential status and current/default model. Complete enough for the agent to recommend a provider.

#### `test_credential` — Validate API Key

```
manage_settings(operation="test_credential", provider="openai")
→ ✅ openai API key is valid.
```

```
manage_settings(operation="test_credential", provider="mistral")
→ ❌ mistral API key is invalid or missing (HTTP 401: Unauthorized).
  The stored key may have expired or been revoked. The user can update it in Settings.
```

### Self-Documenting Behavior

Every invalid input produces a helpful message that teaches the agent the valid options. This means:

- **No upfront documentation needed** — the agent discovers valid keys/values through interaction
- **First bad guess teaches everything** — an unknown key returns the complete list of valid keys with type hints
- **Validation messages are the docs** — every enum field lists its valid values on invalid input
- **Successful operations confirm and guide** — `set` confirms the change and mentions related keys the agent might want to adjust

This replaces the need for a skill. The agent's context is only populated with settings info when the user actually asks about settings.

### Key Reference (for handler implementation)

The handler validates against this whitelist. Any key not listed returns the "Unknown key" guidance message.

| Key | Go type | Validation | Persist target |
|-----|---------|-----------|---------------|
| `provider` | string | Must be known provider | Session override + live agent |
| `model` | string | Any non-empty string | Session override + live agent |
| `last_used_provider` | string | Must be known provider | Global config |
| `provider_models.{name}` | string | Any non-empty string | Global config |
| `provider_priority` | []string | JSON array of provider names | Global config |
| `reasoning_effort` | string | `low` \| `medium` \| `high` \| `""` | Global config |
| `disable_thinking` | bool | `true` \| `false` | Global config |
| `self_review_gate_mode` | string | `off` \| `code` \| `always` | Global config |
| `pdf_ocr_enabled` | bool | `true` \| `false` | Global config |
| `pdf_ocr_provider` | string | Must be known provider | Global config |
| `pdf_ocr_model` | string | Any non-empty string | Global config |
| `subagent_max_parallel` | int | 1–8 | Global config |
| `subagent_parallel_enabled` | bool | `true` \| `false` | Global config |
| `subagent_provider` | string | Must be known provider | Global config |
| `subagent_model` | string | Any non-empty string | Global config |
| `commit_provider` | string | Must be known provider | Global config |
| `commit_model` | string | Any non-empty string | Global config |
| `review_provider` | string | Must be known provider | Global config |
| `review_model` | string | Any non-empty string | Global config |
| `enable_zsh_command_detection` | bool | `true` \| `false` | Global config |
| `auto_execute_detected_commands` | bool | `true` \| `false` | Global config |
| `resource_directory` | string | Valid filesystem path | Global config |
| `system_prompt_text` | string | Any text (requires user approval) | Global config |

"Known providers" = the 12 built-in (`openai`, `deepseek`, `openrouter`, `deepinfra`, `mistral`, `minimax`, `chutes`, `cerebras`, `zai`, `ollama`, `ollama-turbo`, `lmstudio`) + any entries in `CustomProviders`.

### Security

- API keys are **never included** in any return value
- `system_prompt_text` requires explicit user intent (the agent should already have it from the user's message)
- `test_credential` makes a real API call — the tool refuses to repeat it for the same provider within 60 seconds

### Handler Implementation

```go
// pkg/agent/tool_handlers_settings.go (new file)

func handleManageSettings(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
    operation, _ := args["operation"].(string)
    cm := a.configManager
    if cm == nil {
        return "", fmt.Errorf("configuration manager not available")
    }
    switch operation {
    case "get":
        return handleSettingsGet(cm, args)
    case "set":
        return handleSettingsSet(a, cm, args)
    case "list_providers":
        return handleSettingsListProviders(cm)
    case "test_credential":
        return handleSettingsTestCredential(cm, args)
    default:
        return "", fmt.Errorf("unknown operation %q. Use: get, set, list_providers, test_credential", operation)
    }
}
```

Each sub-handler is responsible for:
1. Validating inputs against the whitelist/enums above
2. Returning actionable guidance on invalid input (include type + valid values)
3. Applying changes (global via `cm.UpdateConfig()`, session via `a.SetProvider()`/`a.SetModel()`)
4. Confirming what changed and suggesting related actions

## Implementation Plan

### Phase 1: Core Tool (Day 1-2)

**New files:**
- `pkg/agent/tool_handlers_settings.go` — All 4 operation handlers + validation

**Modified files:**
- `pkg/agent/tool_definitions.go` — Register `manage_settings` tool
- `pkg/configuration/persona_tools.go` — Add `manage_settings` to known tool names

**Tasks:**
1. Register tool with parameter schema
2. Implement `get` — summary mode (no key) and single-key mode with type hints
3. Implement `list_providers` — credential status + model per provider
4. Implement `test_credential` — lightweight API validation with 60s cooldown
5. Implement validation/helpers — key whitelist, type coercion, provider lookup
6. Implement unknown-key guidance message with full valid key list

### Phase 2: Set Operations (Day 2-3)

**Tasks:**
1. Implement `set` — type coercion, whitelist validation, invalid-value guidance with valid options
2. `provider`/`model` special path: session override via `a.SetProvider()`/`a.SetModel()`
3. All other keys: persist via `cm.UpdateConfig()`
4. Add `GetConfigManager()` accessor on `Agent` if not already public
5. Test with `make build-all`

### Phase 3: Validation & Build (Day 3)

**Tasks:**
1. Verify all enum fields return valid-value lists on bad input
2. Verify `set` confirms the change and mentions related keys
3. Verify API keys never appear in output
4. `make build-all`

## Success Criteria

| Metric | Target |
|--------|--------|
| `get` (no key) | Returns full summary with type hints for enums, no follow-up needed |
| `get` (with key) | Returns value + type + valid values for enums |
| `get` (bad key) | Returns complete list of valid keys with descriptions |
| `set` valid | Confirms change, notes persistence behavior, suggests related keys |
| `set` invalid key | Returns list of valid keys |
| `set` invalid value | Returns valid values with type info |
| `list_providers` | One line per provider with credential status and model |
| `test_credential` | Returns valid/invalid with reason, never exposes key |
| Context overhead | 0 tokens when not used |
| Build | `make build-all` passes |

## Files Reference

| File | Action |
|------|--------|
| `pkg/agent/tool_handlers_settings.go` | Create: all 4 handlers + validation |
| `pkg/agent/tool_definitions.go` | Modify: register tool |
| `pkg/configuration/persona_tools.go` | Modify: add to known tools |