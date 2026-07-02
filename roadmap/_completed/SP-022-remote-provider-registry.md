# SP-022: Remote Provider Registry

**Status**: ✅ Implemented
**Priority**: High
**Depends on**: None
**Blocks**: Future provider additions (Mistral, Grok/xAI, Google, etc.)

> **Note (2026-06-04):** This spec is implemented; all phases tracked in TODO.md are checked off. The SP-022 number is also used by `SP-022-workspace-management.md` (still Proposed) — kept as-is to preserve git history references.

## Problem

Provider connection configs (endpoints, auth, retry, streaming, context limits) are embedded in the Go binary via `//go:embed configs/*.json`. Adding or updating a provider requires a code change + release cycle. Only 11 providers have configs today, and only 4 of those (deepinfra, minimax, openrouter, zai) publish model lists to GitHub Pages — the rest have no model registry entries.

Meanwhile, the model registry (`pkg/modelregistry/`) already fetches per-provider model files from `https://sprout-foundry.github.io/sprout/models/{provider}.json` with caching, singleflight, and graceful fallback — but the provider *connection configs* don't use any of this infrastructure.

## Goal

1. **Publish provider configs remotely** alongside model data on the GitHub Pages site
2. **Runtime fetch** — on startup, the Go binary fetches the latest provider configs from the remote registry, merging them with the embedded baseline
3. **Community PRs** — a clear file convention so anyone can open a PR adding a new provider config to the repo
4. **Graceful degradation** — if the remote fetch fails (offline, firewall), the embedded configs work exactly as today
5. **No breaking changes** — custom providers (`~/.config/sprout/providers/`) remain local and take highest priority

## Architecture

### Remote Registry Layout (GitHub Pages)

The existing `model-registry-publish` action already deploys to `https://sprout-foundry.github.io/sprout/`. Add a `providers/` path:

```
sprout-foundry.github.io/sprout/
├── models/
│   ├── index.json                  # (existing) lists providers with model data
│   ├── openrouter.json             # (existing) canonical model list
│   └── ...
└── providers/
    ├── index.json                  # NEW: { schema_version, updated_at, providers: ["openrouter","cerebras",...] }
    ├── openrouter.json             # NEW: full ProviderConfig (same schema as configs/openrouter.json)
    ├── cerebras.json               # NEW
    ├── deepinfra.json              # NEW
    └── ...
```

### Provider Config JSON Schema

The remote files use the **exact same JSON schema** as the current embedded `configs/*.json` files — the `ProviderConfig` struct defined in `pkg/agent_providers/provider_config.go`. The embedded files become the source of truth that get published, not a separate schema to maintain.

Remote files add two metadata fields at the top level (non-destructive — `ProviderConfig` ignores unknown JSON fields via standard Go unmarshaling):

```json
{
  "schema_version": 1,
  "published_at": "2026-06-01T00:00:00Z",
  "name": "openrouter",
  "endpoint": "https://openrouter.ai/api/v1/chat/completions",
  "auth": { ... },
  ...
}
```

### Import Cycle Avoidance

`pkg/providerregistry/` MUST NOT import `pkg/agent_providers/` directly — that would create a cycle since `provider_factory.go` would import `providerregistry` for the async refresh.

**Solution**: Define a lightweight `RemoteProviderConfig` struct in `pkg/providerregistry/` that contains only the connection-relevant fields (name, endpoint, auth, headers, defaults, conversion, streaming, models, retry, cost). The struct is intentionally duplicated from `ProviderConfig` to avoid the import cycle — the same pattern already used by `modelregistry.ModelInfo` vs `api.ModelInfo`. A `ToProviderConfig()` converter method translates between them.

### Priority Chain (highest → lowest)

1. **Custom providers** — `~/.config/sprout/providers/*.json` (user-defined, local only)
2. **Remote registry** — fetched from GitHub Pages at startup, cached locally
3. **Embedded configs** — `//go:embed configs/*.json` (shipped with the binary)

When configs overlap (same provider name), the higher-priority source wins. Custom providers always win. Remote always wins over embedded.

### Thread Safety

`ProviderFactory` currently has no mutex — all writes happen in `init()` before any reads. Adding an async goroutine changes this.

**Solution**: Add a `sync.RWMutex` to `ProviderFactory`.
- `GetProviderConfig()`, `GetAvailableProviders()`, `CreateProvider()`, `ListProvidersWithModels()` acquire RLock.
- `UpsertConfig()`, `LoadConfigFromFile()`, `LoadConfigFromBytes()` acquire Lock.
- The `configs` and `registry.ProviderConfigs` maps are always updated together inside the write lock.

### New Package: `pkg/providerregistry`

Mirrors the pattern established by `pkg/modelregistry/`:

```
pkg/providerregistry/
├── registry.go       # FetchProviderConfig(), FetchAllProviders(), cache, singleflight
└── registry_test.go  # unit tests with httptest server
```

Key design:
- `FetchProviderConfig(ctx, providerID) → *RemoteProviderConfig, error`
- `FetchAllProviders(ctx) → map[string]*RemoteProviderConfig, error` — fetches `providers/index.json`, then batch-fetches each provider file
- Same caching strategy as model registry: 5-min TTL, 30-sec negative cache, singleflight dedup
- 500ms HTTP timeout per provider file (index fetch: 1s)
- Configurable via `PROVIDER_REGISTRY_URL` env var (default: same GitHub Pages base as model registry)
- Shares the same base URL as `MODEL_REGISTRY_URL` if `PROVIDER_REGISTRY_URL` is not explicitly set

### Modified Flow: `factory.init()` → `factory.initAndRefresh()`

**Current** (`pkg/factory/factory.go`):
```go
func init() {
    globalProviderFactory = providers.NewProviderFactory()
    globalProviderFactory.LoadEmbeddedConfigs()      // always
    globalProviderFactory.LoadConfigsFromDirectory()  // dev-time override
}
```

**Proposed**:
```go
func init() {
    globalProviderFactory = providers.NewProviderFactory()
    globalProviderFactory.LoadEmbeddedConfigs()       // always — baseline

    // Skip network fetch in test binaries to avoid hitting GitHub Pages
    // from every test run (mirrors providercatalog.inTestBinary() pattern).
    if !inTestBinary() {
        go refreshFromRemote(context.Background())
    }
}

func refreshFromRemote(ctx context.Context) {
    remoteConfigs, err := providerregistry.FetchAllProviders(ctx)
    if err != nil {
        return // embedded configs are fine
    }
    for name, remoteCfg := range remoteConfigs {
        cfg := remoteCfg.ToProviderConfig() // converts to providers.ProviderConfig
        globalProviderFactory.UpsertConfig(name, cfg)
    }
}

func inTestBinary() bool {
    if len(os.Args) == 0 { return false }
    return strings.HasSuffix(os.Args[0], ".test") ||
        strings.Contains(os.Args[0], "/_test/") ||
        strings.HasSuffix(os.Args[0], ".test.exe")
}
```

This means:
- **First request** after startup uses embedded configs (instant availability)
- **~500ms later** the remote configs arrive and overwrite the embedded ones via `UpsertConfig()`
- Subsequent requests use the fresh remote configs
- If offline, embedded configs work forever
- Test binaries never hit the network

### `configuration.GetAvailableProviders()` Fix

**Current problem**: `GetAvailableProviders()` in `pkg/configuration/init.go` creates a **throwaway** `ProviderFactory`, loads embedded configs, and returns those providers. It never consults the global factory.

**Fix**: Replace the throwaway factory with a reference to the global factory from `pkg/factory/`:

```go
func GetAvailableProviders() []string {
    // Use the global factory (which has embedded + remote configs)
    factoryProviders := factory.GlobalFactory().GetAvailableProviders()
    // ... merge with special providers and custom providers as before
}
```

This requires exporting the global factory or adding a `GetAvailableProviders()` accessor to the `factory` package.

### `CreateProviderClient()` Discovery

The current switch statement in `factory.CreateProviderClient()` has hardcoded cases for each `ClientType`. New providers added via remote registry won't have a `ClientType` constant.

This already works correctly: the `default` case calls `CreateGenericProvider(string(clientType))`, which looks up the provider in the global factory by name. Remote configs are in the global factory after the async refresh. No code change needed — just verify this path works for remote-only providers.

### Remote Config Validation

To prevent SSRF or malicious config injection via a compromised GitHub Pages:
- `FetchProviderConfig()` validates that `endpoint` is an `https://` URL (reject `http://`, `file://`, etc.)
- `endpoint` hostname must not be a private/internal IP (reject `127.0.0.1`, `10.x`, `192.168.x`, `localhost`)
- Validation is performed in `providerregistry` before caching

### CLI Path (Non-Daemon)

The CLI (`sprout --output-json`, `sprout agent`) runs synchronously and exits. The async goroutine may not complete before the first provider is needed.

**This is fine**: the first request uses embedded configs (identical to current behavior). If the user runs a second command later, the remote configs may have been cached to a local file (future enhancement). For now, CLI commands use embedded configs — only the daemon gets the benefit of async refresh. This matches how `providercatalog.RefreshFromRemoteAsync()` already works.

### Adding a New Provider (Community PR Pattern)

To add a new provider, a contributor:

1. Creates `pkg/agent_providers/configs/{provider}.json` using the existing schema
2. (Optional) Adds a model adapter in `pkg/modelcontract/{provider}.go` if the provider has a non-standard models endpoint
3. Runs `go run pkg/agent_providers/generate_providers.go` to regenerate `provider_gen.go`
4. Opens a PR

The CI workflow publishes the new config to GitHub Pages on merge. Existing binaries pick it up within 5 minutes of their next startup (daemon) or on next run (CLI with caching).

For providers that use the standard OpenAI-compatible `/v1/chat/completions` endpoint, only step 1 is required — `GenericProvider.ListModels()` already handles the `/models` endpoint.

### `ollama-local`, `test`, `editor` Special Cases

These have no JSON configs — they use dedicated Go code paths. They will **not** be published to the remote registry and remain hardcoded in `CreateProviderClient()`.

### Files Changed

| File | Change |
|---|---|
| `pkg/providerregistry/registry.go` | NEW — remote provider config fetcher with its own `RemoteProviderConfig` struct |
| `pkg/providerregistry/registry_test.go` | NEW — cache, negative cache, singleflight, TTL, SSRF validation tests |
| `pkg/agent_providers/provider_factory.go` | Add `sync.RWMutex`, `UpsertConfig()` (updates both `configs` and `registry.ProviderConfigs` under write lock) |
| `pkg/factory/factory.go` | Add async `refreshFromRemote()`, `inTestBinary()`, export global factory accessor |
| `pkg/configuration/init.go` | `GetAvailableProviders()` uses global factory instead of throwaway instance |
| `.github/workflows/model-registry-publish.yml` | Add provider config publish step |
| `scripts/generate-provider-index.sh` | NEW — generates `providers/index.json` |
| `pkg/agent_providers/configs/*.json` | No change (become the source for remote publish) |
| `pkg/credentials/resolve.go` | Fix `lmstudio` inconsistency — mark as NOT requiring API key (it's a local server) |
| `pkg/agent_providers/generate_providers.go` | Fix `lmstudio` auth type to `"none"` in generated code |

### Not In Scope

- **Custom providers** — remain file-based in `~/.config/sprout/providers/`, never remote
- **Provider credential storage** — API keys remain local (env var, keyring, encrypted file)
- **Hot reload mid-session** — configs refresh at startup only
- **WebUI changes** — the webui already queries `GET /api/providers` which uses the factory; no frontend changes needed
- **Local file cache for CLI** — future enhancement; CLI uses embedded configs for now

### Risks

| Risk | Mitigation |
|---|---|
| GitHub Pages outage | Embedded configs are the baseline; binary works offline |
| Malicious config in a PR | Code review on the repo; endpoint URL validation in `providerregistry` |
| Config schema drift | Same `ProviderConfig` struct used for embedded and remote |
| Race between init() and first request | Mutex on factory; first request uses embedded (0-latency) |
| Import cycle | `providerregistry` defines its own config struct, converts at factory boundary |
| Test binary network chatter | `inTestBinary()` guard skips async goroutine |
| `configuration.GetAvailableProviders()` misses remote providers | Use global factory instead of throwaway |

### Implementation Todos

#### Phase 1: Foundation (pkg/providerregistry + thread safety)
- [ ] SP-022-1a: Create `pkg/providerregistry/` package — define `RemoteProviderConfig` struct (duplicated fields from `ProviderConfig` to avoid import cycle), `ToProviderConfig()` converter, `FetchProviderConfig(ctx, providerID)`, `FetchAllProviders(ctx)` with cache/singleflight/TTL/negative-cache
- [ ] SP-022-1b: Add `sync.RWMutex` to `ProviderFactory` — protect `configs` and `registry.ProviderConfigs` maps; all read methods acquire RLock, all write methods acquire Lock
- [ ] SP-022-1c: Add `UpsertConfig(name string, cfg *ProviderConfig)` to `ProviderFactory` — acquires write lock, updates both `f.configs[name]` and `f.registry.ProviderConfigs[name]`
- [ ] SP-022-1d: Add SSRF validation to `pkg/providerregistry/` — reject non-HTTPS endpoints, private IPs, localhost

#### Phase 2: Runtime Integration
- [ ] SP-022-2a: Add async `refreshFromRemote()` to `factory.init()` with `inTestBinary()` guard — fetches all remote provider configs and upserts into the global factory
- [ ] SP-022-2b: Export global factory accessor from `pkg/factory/` (e.g., `GlobalFactory() *providers.ProviderFactory` or `GlobalAvailableProviders() []string`)
- [ ] SP-022-2c: Fix `GetAvailableProviders()` in `pkg/configuration/init.go` to use the global factory instead of creating a throwaway instance
- [ ] SP-022-2d: Add `PROVIDER_REGISTRY_URL` env var support — default reuses same base as `MODEL_REGISTRY_URL`; support `"off"`/`"none"`/`"disabled"` to disable

#### Phase 3: CI Publishing
- [ ] SP-022-3a: Create `scripts/generate-provider-index.sh` — generates `providers/index.json` listing all provider config files with timestamps
- [ ] SP-022-3b: Extend `.github/workflows/model-registry-publish.yml` — add step to copy `configs/*.json` to the GitHub Pages artifact with `schema_version` + `published_at` metadata injection via `jq`
- [ ] SP-022-3c: Publish the 7 missing provider model files (cerebras, chutes, deepseek, lmstudio, mistral, ollama-turbo, openai) — ensure `refresh_provider_catalog` covers all 11 providers (may require adding API keys for missing providers to CI secrets)

#### Phase 4: Bug Fixes
- [ ] SP-022-4a: Fix `lmstudio` API key inconsistency — update `pkg/agent_providers/configs/lmstudio.json` auth type to `"none"`, regenerate `provider_gen.go`, and update `credentials/resolve.go` to consistently mark lmstudio as not requiring a key

#### Phase 5: Documentation & Testing
- [ ] SP-022-5a: Add `CONTRIBUTING.md` section documenting the provider addition pattern: create JSON config → run `generate_providers.go` → open PR → CI auto-publishes
- [ ] SP-022-5b: Unit tests for `pkg/providerregistry/` — cache hit/miss, negative cache, singleflight dedup, TTL expiry, offline fallback, SSRF rejection
- [ ] SP-022-5c: Unit tests for `UpsertConfig()` — concurrent read/write safety, both maps updated atomically
- [ ] SP-022-5d: Integration test: embedded-only mode (no remote) works correctly; remote configs merge over embedded
- [ ] SP-022-5e: Verify `make build-all` passes after all changes
