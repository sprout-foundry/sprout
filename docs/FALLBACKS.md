# Fallback & Default Resolution Chains

Every place sprout substitutes a value with a default, inherits from a parent config, or silently falls back to a secondary option. Understanding these chains is essential for debugging unexpected behavior (wrong model, wrong provider, wrong context window).

> **Policy (v0.17+):** Provider selection is explicit — no implicit fallback to a default provider. If no provider is configured, sprout surfaces an error and offers interactive provider selection. Model and config defaults remain but are being made progressively more visible.

---

## 1. Provider Resolution

### Entry point: `ResolveProviderModel` (`pkg/configuration/provider_resolution.go`)

```
1. Explicit --provider flag
2. provider:model prefix in --model flag (e.g. --model openai:gpt-5-mini)
3. SPROUT_PROVIDER env var (LEDIT_PROVIDER backward-compat)
4. SPROUT_MODEL env var (parsed for provider:model prefix)
5. Config last_used_provider
6. If still empty: error returned. Caller surfaces interactive provider picker.
```

### `DetermineProvider` (`pkg/agent_api/interface.go`)

```
1. Explicit provider argument
2. SPROUT_PROVIDER env var
3. Config last_used_provider (if available — has API key)
4. Error: "no provider available"
```

**v0.17 change:** Steps 4 (priority scan) and 5 (ollama-local ultimate fallback) were removed. If steps 1-3 fail, the caller receives an error and can offer interactive provider selection via the existing `SelectNewProvider()` path.

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

### `GetModelForProvider`

```
1. cfg.ProviderModels[provider] (user-set)
2. "" (empty — caller must handle)
```

**v0.17 change:** The `NewConfig()` baked-in defaults (gpt-5-mini, GLM-4.6, DeepSeek-V3.1-Terminus, etc.) were removed. If a user hasn't explicitly set a model, the caller uses the live provider API to select one.

---

## 3. Subagent Provider & Model

### Per-spawn resolution: `resolveSubagentProviderModel`

```
Tier 1: Persona-specific config (persona's Provider / Model fields)
Tier 2: Global subagent config (SubagentProvider / SubagentModel)
Tier 3: Parent agent inheritance (field-by-field)
        ── Provider inherits if neither persona nor global config set it
        ── Model inherits if neither persona nor global config set it
```

**v0.17 fix:** Inheritance is now field-by-field. Setting only SubagentProvider no longer blocks model inheritance, and vice versa.

### `GetSubagentProvider`

```
1. cfg.SubagentProvider (direct field only — may be empty)
```

**v0.17 change:** The fallback chain (→ LastUsedProvider → ProviderPriority[0] → ollama-local) was removed. An empty field is simply empty; callers handle this by inheriting from the parent agent.

### `GetSubagentModel`

```
1. cfg.SubagentModel
2. GetModelForProvider(GetSubagentProvider())  — caller's provider's model or ""
```

### `GetSubagentTypeProvider` (persona-specific)

```
1. Persona's own Provider field
2. GetSubagentProvider() (may be empty)
```

### `GetSubagentTypeModel` (persona-specific)

```
1. Persona's own Model field
2. GetSubagentModel() (may be empty)
```

---

## 4. Commit & Review Provider/Model

### `GetCommitProvider` / `GetReviewProvider`

```
1. cfg.CommitProvider / cfg.ReviewProvider (direct field only — may be empty)
```

**v0.17 change:** The 4-level fallback chain (→ LastUsedProvider → ProviderPriority[0] → ollama-local) was removed. These now return only the explicitly configured value. Callers that need a provider fall back to the main agent's provider.

### `GetCommitModel` / `GetReviewModel`

```
1. cfg.CommitModel / cfg.ReviewModel
2. GetModelForProvider(GetCommitProvider() / GetReviewProvider())  — may be ""
```

---

## 5. Context Window Limit

The context window pipeline has two layers (SP-126): a **native** value
from the model client and an **effective** value that may be capped by
the user's `Config.MaxContextTokens` setting.

### `getNativeModelContextLimit` (`pkg/agent/utils.go`) — raw value

```
1. Client's GetModelContextLimit() → model's reported context window
2. → 32000 (hardcoded fallback when client is nil or API call fails)
   ⚠ A warning is logged via the agent logger when this fallback fires.
```

This is the **uncapped** model window. Returned exactly as the client
reports it (or the 32K fallback). Used by `ResolveEffectiveContextCap`
to compute the cap.

### `ResolveEffectiveContextCap` (`pkg/configuration/context_profile.go`) — cap resolver

```
1. cfg.MaxContextTokens is nil or 0 → return native (no cap)
2. cfg.MaxContextTokens < EffectiveContextCapMinimum (1024) → error
   (matches the /max-context and settings_defs validators)
3. Otherwise → min(native, cfg.MaxContextTokens)
```

Resolved once at agent creation and stored on `Agent.effectiveContextCap`.
Every downstream call site (seed_provider.Info, seed_query.OnIteration,
metrics.GetEffectiveContextCap) reads from this field. Call sites MUST
NOT re-derive from `Config.MaxContextTokens` or call
`client.GetModelContextLimit()` directly — those paths bypass the cap.

### `getModelContextLimit` (`pkg/agent/utils.go`) — capped value

```
1. getNativeModelContextLimit() → model's native window
2. Capped by user's MaxContextTokens setting (if set and lower)
3. → 32000 (hardcoded fallback when client is nil or API call fails)
   ⚠ A warning is logged via the agent logger when this fallback fires.
```

Backward-compatible: existing call sites that want the capped value
keep working unchanged. New code should prefer
`Agent.GetEffectiveContextCap()` which goes through the resolved
Agent field rather than re-reading the config.

### Activation notice

When the user explicitly sets a cap lower than the native window, a
one-time stderr notice fires at agent creation:

```
⚡ Context cap active: 300.0K (native: 1.00M)
  All requests will use at most 300.0K of context.
  /max-context clear to remove, /max-context <N> to change.
```

Skip when the cap equals the native window (no-op) and skip when no
cap was set.

**Implication:** If the client can't report a context window (nil client, API error, unknown model), the limit falls back to 32K and a warning is emitted. A 1M-token model would run at 3% of capacity with a visible log warning. The cap, if set, applies on top of whatever the native-fallback yields (so a user-configured 300K cap beats the 32K fallback).

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
| 🟡 Medium | Model selection heuristics | Wrong model variant picked, especially on DeepInfra/OpenRouter |
| 🔵 Low | Resolve() overrides zero-values | `0` might mean "unlimited" but gets overridden by positive default |
| 🔵 Low | Vision silently falls back to OCR | Different model, different quality, only logged to stderr |

### Resolved (v0.17)

| Was | Fix |
|---|---|
| 🔴 Provider → ollama-local | Removed. Error surfaced, interactive picker offered. |
| 🔴 Model → NewConfig() defaults | Removed. Live API model selection used instead. |
| 🔴 Context limit → 32K silent | Warning logged when fallback fires. |
| 🟡 Subagent partial inheritance | Field-by-field resolution. |
| 🟡 3 copies of 4-level fallback | Removed. Direct field access only. |

---

## Related

- [Configuration](CONFIGURATION.md) — config files, env vars
- [Security](SECURITY.md) — risk profiles and tool gating
- [Architecture](ARCHITECTURE.md) — package layout and data flow
