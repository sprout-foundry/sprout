# SP-022: Remote Provider Registry

**Status:** ✅ Implemented (runtime fetch from GitHub Pages, thread-safe factory, SSRF validation, CI publish pipeline)

Provider connection configs were embedded in the Go binary via `//go:embed configs/*.json`, requiring a code change + release cycle to add or update providers. This spec introduced a remote provider registry on GitHub Pages (`sprout-foundry.github.io/sprout/providers/`) that is fetched at startup and merged into the global factory via `UpsertConfig()`. A new `pkg/providerregistry/` package mirrors the `pkg/modelregistry/` pattern with 5-min TTL caching, singleflight dedup, and SSRF validation (rejects non-HTTPS endpoints and private IPs). Priority chain: custom providers > remote registry > embedded configs. Thread safety added via `sync.RWMutex` on `ProviderFactory`. CLI commands use embedded configs (async refresh may not complete before first request); the daemon benefits from the async refresh on startup.

## Key decisions

- `RemoteProviderConfig` struct duplicated from `ProviderConfig` in `pkg/providerregistry/` to avoid import cycles (same pattern as `modelregistry.ModelInfo` vs `api.ModelInfo`).
- Async refresh in `factory.init()` with `inTestBinary()` guard — test binaries never hit the network.
- SSRF validation in `providerregistry` rejects non-HTTPS endpoints and private/internal IPs.
- `GetAvailableProviders()` fixed to use the global factory instead of creating a throwaway instance.
- Community PR pattern: create JSON config → run `generate_providers.go` → open PR → CI auto-publishes.

## Artifacts

- code: `pkg/providerregistry/registry.go` — `FetchProviderConfig()`, `FetchAllProviders()`, cache, singleflight
- code: `pkg/agent_providers/provider_factory.go` — `sync.RWMutex`, `UpsertConfig()`
- code: `pkg/factory/factory.go` — async `refreshFromRemote()`, `inTestBinary()`
- code: `pkg/configuration/init.go` — `GetAvailableProviders()` uses global factory
- code: `scripts/generate-provider-index.sh` — generates `providers/index.json`
- code: `.github/workflows/model-registry-publish.yml` — extended to publish provider configs
- tests: `pkg/providerregistry/registry_test.go` — cache, negative cache, singleflight, TTL, SSRF validation

Full specification archived — see git history for original content.
