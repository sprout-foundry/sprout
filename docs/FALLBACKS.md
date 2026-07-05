# Fallback & Default Resolution Chains

Every place sprout substitutes a value with a default, inherits from a parent config, or silently falls back to a secondary option. Understanding these chains is essential for debugging unexpected behavior (wrong model, wrong provider, wrong context window).

> **Policy:** Fallbacks are never errors — they keep sprout working when configuration is incomplete. But they should be visible. This document is the canonical reference; the goal is to surface every chain so users and agents can trace what actually resolved.

---

## 1. Provider Resolution

### Entry point: `ResolveProviderModel` (`pkg/configuration/provider_resolution.go`)

```
1. Explicit --provider flag
2. provider:model prefix in --model flag (e.g. --model openai:gpt-5-mini)
3. SPROUT_PROVIDER env var (LEDIT_PROVIDER backward-compat)
4. SPROUT_MODEL env var (parsed for provider:model prefix)
5. Config last_used_provider
6. DetermineProvider() auto-detection (see §1a)
```

### Auto-detection: `DetermineProvider` (`pkg/agent_api/interface.go`)

```
1. Explicit provider argument
2. SPROUT_PROVIDER env var
3. Config last_used_provider (if available — has API key)
4. First available from priority list:
   openrouter → zai → deepinfra → deepseek → minimax →
   cerebras → chutes → openai → mistral → lmstudio →
   ollama-cloud → ollama-local
5. "ollama-local" (ultimate hardcoded fallback)
```

**Implication:** If your intended provider loses its API key (key file deleted, env var unset), sprout silently drops through the priority list and may land on ollama-local. There is no startup warning for this.

---

## 2. Model Resolution

### Model ID when switching providers (`SetProvider` / `SetProviderPersisted`)

```
1. Config provider_models[provider] (user-configured)
2. Factory default model (from embedded provider config)
3. selectDefaultModel() (see §2a)
4. Error: "no models available from provider X"
```

When the configured model isn't in the provider's available list:
```
1. Case-insensitive match attempt against provider's model list
2. Fallback to first available model (warning printed to stderr)
```

### `selectDefaultModel` heuristics (`pkg/agent/models.go`)

```
1. Probe-recommended "primary" model (passed complex stage)
2. Probe-recommended "subagent" model (passed gates stage)
3. Per-provider string-matching:
   - DeepInfra: prefers "deepseek" + "instruct"
   - OpenRouter: prefers ":free" models
   - Ollama Local: prefers "llama3.2" or "llama3.1"
   - Ollama Cloud: prefers "gpt-oss:20b"
   - LM Studio: skips embedding models
4. First model in the provider's list
```

### `GetModelForProvider` default chain

```
1. cfg.ProviderModels[provider] (user-set)
2. NewConfig().ProviderModels[provider] (baked-in defaults:
   openai→gpt-5-mini, zai→GLM-4.6, deepinfra→DeepSeek-V3.1-Terminus,
   openrouter→openai/gpt-5, ollama-local→qwen3-coder:30b,
   ollama-cloud→deepseek-v3.1:671b)
3. "" (empty — caller must handle)
```

**Implication:** If you've never explicitly set a model for a provider, you get whatever version is hardcoded in `NewConfig()`, which may be months out of date.

---

## 3. Subagent Provider & Model

### Per-spawn resolution: `resolveSubagentProviderModel`

```
Tier 1: Persona-specific config (persona's Provider / Model fields)
Tier 2: Global subagent config (SubagentProvider / SubagentModel)
Tier 3: Parent agent inheritance (parent's provider/model)
        ── ONLY when BOTH SubagentProvider="" AND SubagentModel=""
           AND persona has no explicit provider/model
```

**Footgun:** Setting _either_ `SubagentProvider` or `SubagentModel` (but not both) disables parent inheritance entirely. The unset field falls back to its own chain instead of inheriting from the parent.

### `GetSubagentProvider` chain

```
1. cfg.SubagentProvider
2. cfg.LastUsedProvider
3. cfg.ProviderPriority[0]
4. "ollama-local"
```

### `GetSubagentModel` chain

```
1. cfg.SubagentModel
2. GetModelForProvider(GetSubagentProvider())
   → user-set → NewConfig() defaults → ""

### `GetSubagentTypeProvider` (persona-specific)

```
1. Persona's own Provider field
2. GetSubagentProvider() (the 4-level chain above)
```

### `GetSubagentTypeModel` (persona-specific)

```
1. Persona's own Model field
2. GetSubagentModel() (inherits provider→model chain)
```

---

## 4. Commit & Review Provider/Model

Identical 4-level chain (duplicated 3 times — subagent, commit, review):

### `GetCommitProvider` / `GetReviewProvider`

```
1. cfg.CommitProvider / cfg.ReviewProvider
2. cfg.LastUsedProvider
3. cfg.ProviderPriority[0]
4. "ollama-local"
```

### `GetCommitModel` / `GetReviewModel`

```
1. cfg.CommitModel / cfg.ReviewModel
2. GetModelForProvider(GetCommitProvider() / GetReviewProvider())
```

---

## 5. Context Window Limit

### `getModelContextLimit` (`pkg/agent/utils.go`)

```
1. Client's GetModelContextLimit() → model's reported context window
2. Capped by user's MaxContextTokens setting (if set and lower)
3. → 32000 (hardcoded fallback when client is nil or API call fails)
```

**Implication:** If the client can't report a context window (nil client, API error, unknown model), the limit silently becomes 32K. A 1M-token model would be cut to 3% of its capacity with no user-visible warning.

Also appears in:
- `pkg/agent_api/token_utils.go:161` — `contextLimit = 32000 // Default fallback`
- `pkg/agent_api/ollama_local.go:424` — `contextLimit = 32000`
- `pkg/agent_api/models.go:93` — `// Fallback to a reasonable default`

---

## 6. Config Struct `Resolve()` Methods

These fill in defaults for zero-valued fields. Called at config load time.

| Struct | Key Defaults |
|---|---|
| `NotificationsConfig` | `MinSeconds=10` |
| `EditApprovalConfig` | `Mode="off"` |
| `PersistentContextConfig` | `ProactiveContextEnabled=true`, `MaxContextualResults=5`, `MinRelevanceScore=0.50`, `MaxContextChars=4000`, `WorkspaceScopedRetrieval=true`, `DriftDetectionEnabled=true`, `DriftThreshold=0.60`, `DriftCheckInterval=5` |
| `ComputerUseConfig` | `MaxActionsPerMinute=60`, `PanicKeyChord="ctrl+shift+escape"`, `DestructiveAppGate=true` |
| `VisionConfig` | `ParallelWorkers=3`, `MaxParallelRequests=8`, `EnableBatchProcessing=true`, `MaxBatchSize=4` |
| `ChangeTrackingConfig` | `Enabled=true`, `ShellWalkEnabled=true`, `MaxFiles=50000`, `MaxTotalBytes=32MiB`, `MaxDurationMs=500` |
| `RevisionRetentionConfig` | Retention settings with safe defaults |

**Caveat:** `Resolve()` treats zero-values as "not set" and fills defaults. A user who explicitly sets a field to `0` (intending "unlimited" or "off") may have it overridden by a positive default. Notable examples: `MaxActionsPerMinute=0`, `MaxBatchSize=0`.

---

## 7. Vision → OCR Fallback

### `fallbackToOCR` (`pkg/agent_tools/vision_fallback.go`)

```
1. Primary vision model (with retries via DoVisionRetry)
2. Gate: VISION_FALLBACK_TO_OCR env var (default: true)
3. Gate: PDFOCRModel must be configured and non-empty
4. Create one-off Ollama client targeting OCR model
5. Single-shot OCR attempt (no further retries)
6. Return lastErr if OCR also fails
```

Logged at INFO level. Visible in debug output but not surfaced in the UI unless the user is watching stderr.

---

## 8. Persona Resolution

### `GetSubagentType` (`pkg/configuration/config_subagent.go`)

```
1. Lookup by normalized ID, name field, or aliases
2. Check IsPersonaDisabled — returns nil if disabled
3. Deep-copy all fields and return
```

### `defaultSubagentTypes` fallback

If embedded persona definitions fail to load: stderr warning, empty map. No personas available.

---

## 9. Shell Editor Resolution

### `/edit` command (`pkg/agent_commands/edit.go`)

```
1. $VISUAL env var
2. $EDITOR env var
3. "vi" (if found in PATH)
4. Error: "no editor found"
```

---

## 10. Subagent Parallelism Defaults

| Method | Zero/Nil Behavior |
|---|---|
| `GetSubagentMaxParallel()` | `0` → `2` |
| `GetSubagentParallelEnabled()` | `nil` → `true` |
| `GetSubagentMaxDepth()` | `0` → `2` |

---

## 11. API Timeout Defaults

From `NewConfig()`:
```
ConnectionTimeoutSec:    300  (5 min)
FirstChunkTimeoutSec:    600  (10 min)
ChunkTimeoutSec:         600  (10 min)
OverallTimeoutSec:       1800 (30 min)
CommitMessageTimeoutSec: 300  (5 min)
```

---

## Risk Summary

| Risk | Chain | Impact |
|---|---|---|
| 🔴 High | Provider resolution → ollama-local | User thinks they're on a paid provider, actually on local LLM |
| 🔴 High | Context limit → 32K | 1M-token model runs at 3% capacity, no warning |
| 🟡 Medium | Model selection heuristics | Wrong model variant picked, especially on DeepInfra/OpenRouter |
| 🟡 Medium | Subagent inherits parent only when ALL fields empty | Setting SubagentProvider breaks the entire inheritance chain |
| 🟡 Medium | 3 copies of same 4-level fallback | Risk of drift between subagent/commit/review chains |
| 🔵 Low | Resolve() overrides zero-values | `0` might mean "unlimited" but gets overridden by positive default |
| 🔵 Low | Vision silently falls back to OCR | Different model, different quality, only logged to stderr |

---

## Related

- [Configuration](CONFIGURATION.md) — config files, env vars
- [Security](SECURITY.md) — risk profiles and tool gating
- [Architecture](ARCHITECTURE.md) — package layout and data flow
