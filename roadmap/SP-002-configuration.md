# SP-002: Configuration, Credentials & Providers

**Status:** ✅ Active  
**Location:** `pkg/configuration/`, `pkg/credentials/`, `pkg/agent_providers/`  
**Test Coverage:** Good (layered config tests, credential tests)

## Current State

Three interconnected systems provide configuration management, credential storage, and provider model catalogs. Recently enhanced with layered config (global → workspace → session).

## Configuration System (`pkg/configuration/`)

### Layered Config

Three config layers resolved in order (later overrides earlier):

1. **Global:** `~/.sprout/config.json` — user-wide defaults
2. **Workspace:** `{workspace}/.sprout/config.json` — per-project overrides
3. **Session:** `chatSession.ConfigOverrides` — per-chat ephemeral overrides

Merged via `MergeConfig(base, override)` — maps merged additively, scalars overridden.

### Config Struct (key fields)

```go
type Config struct {
    Version               string                 `json:"version"` // "2.0"
    LastUsedProvider      string                 `json:"last_used_provider"`
    ProviderModels        map[string]string      `json:"provider_models"`
    ProviderPriority      []string               `json:"provider_priority"`
    APIKeys               APIKeys                `json:"api_keys"`
    CustomProviders       map[string]CustomProviderConfig
    MCP                   mcp.MCPConfig          `json:"mcp"`
    SubagentProvider      string                 `json:"subagent_provider"`
    SubagentModel         string                 `json:"subagent_model"`
    SubagentTypes         map[string]SubagentType `json:"subagent_types"`
    SubagentMaxParallel   int                    `json:"subagent_max_parallel"`
    SubagentParallelEnabled *bool                 `json:"subagent_parallel_enabled"`
    Skills                map[string]Skill        `json:"skills"`
    // ... 30+ more fields
}
```

### SubagentType (Persona Config)

```go
type SubagentType struct {
    ID, Name, Description     string
    Provider, Model           string
    SystemPrompt, SystemPromptText, SystemPromptAppend string
    AllowedTools              []string
    Aliases                   []string
    Enabled                   bool
}
```

### CRUD Operations

- `Load() *Config` — loads global config
- `LoadFromDir(dir) *Config` — loads from specific directory
- `(c *Config) Save() error` — saves to global
- `(c *Config) SaveToDir(dir) error` — saves to specific directory
- Config auto-saves after provider/model changes

## Credential System (`pkg/credentials/`)

### Architecture

- **Three resolution paths** consolidated into one unified `Resolve()` function
- Credential hierarchy: `environment variable → credential store → config file metadata`
- Supports OS keyring integration (`zalando/go-keyring`)
- Supports encrypted-at-rest file storage (`age`, `nacl/secretbox`)

### Key Functions

- `Resolve(provider, key) (value, source, error)` — unified credential resolution
- `Store(provider, key, value)` — persist a credential
- `HasProviderCredential(provider) bool` — check if a provider has a stored key
- `Mask(value) string` — redact for logging

### Security

- API keys never logged in plaintext
- Config migration removes plaintext keys from `config.json` → moves to credential store
- MCP server env vars with secrets flagged for migration

## Provider System (`pkg/agent_providers/`)

### Provider Configs

Each provider has a JSON config in `pkg/agent_providers/configs/`:

| File | Provider | Details |
|------|----------|---------|
| `openai.json` | OpenAI | GPT-4o, GPT-5 series |
| `openrouter.json` | OpenRouter | Multi-model gateway |
| `deepseek.json` | DeepSeek | V4 Flash, V4 Pro, V3 |
| `deepinfra.json` | DeepInfra | Multi-model |
| `anthropic.json` | Anthropic | Claude series |
| `ollama.json` | Ollama | Local models |
| `minimax.json` | MiniMax | M2 series |
| `zai.json` | ZAI | GLM series |
| `google.json` | Google | Gemini |
| `mistral.json` | Mistral | Mistral/Large |

### Config Schema

```go
type ProviderConfig struct {
    Name           string
    Endpoint       string
    Auth           AuthConfig
    Cost           CostConfig
    Defaults       DefaultConfig
    MessageConversion MessageConversion
    Models         ModelsConfig
    Streaming      StreamingConfig
}
```

Each config defines: API endpoint, auth type, cost per token, default model, model list with context lengths, streaming format, and message conversion rules (tool call format, reasoning field, etc.).

### Custom Providers

Users can define custom providers in `config.json` → `custom_providers`. Each specifies endpoint, auth, model info.

### Model Registry

- `pkg/providercatalog/providers.json` — offline model catalog embedded in binary
- `cmd/refresh_provider_catalog/` — CLI tool to refresh from provider APIs
- Model info: context length, description, tags (coding, tools, reasoning)

## Open Work

No open configuration items in TODO.md — system is mature. Items below are from SP-006/SP-007 dependencies:

- Role config resolution chain (SP-007)
- Per-role MCP server additions (SP-007)
- Workspace-scoped role configs (SP-007)

## Key Files

| File | Purpose |
|------|---------|
| `pkg/configuration/config.go` | Config struct, Load, Save, MergeConfig, defaults |
| `pkg/configuration/layered_config.go` | Layer resolution and merge logic |
| `pkg/configuration/env.go` | Environment variable helpers |
| `pkg/credentials/store.go` | Unified credential resolution + storage |
| `pkg/credentials/keyring.go` | OS keyring integration |
| `pkg/agent_providers/configs/*.json` | Per-provider configuration |
| `pkg/agent_providers/provider_config.go` | Custom provider loading |
| `pkg/providercatalog/providers.json` | Offline model catalog |
