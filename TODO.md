# TODO

## SP-022: Remote Provider Registry
_Spec: `roadmap/SP-022-remote-provider-registry.md`_

### Phase 1: Foundation (pkg/providerregistry + thread safety)
- [x] SP-022-1a: Create `pkg/providerregistry/` package — define `RemoteProviderConfig` struct (duplicated fields from `ProviderConfig` to avoid import cycle), `ToProviderConfig()` converter, `FetchProviderConfig(ctx, providerID)`, `FetchAllProviders(ctx)` with cache/singleflight/TTL/negative-cache
- [x] SP-022-1b: Add `sync.RWMutex` to `ProviderFactory` — protect `configs` and `registry.ProviderConfigs` maps; all read methods acquire RLock, all write methods acquire Lock
- [x] SP-022-1c: Add `UpsertConfig(name string, cfg *ProviderConfig)` to `ProviderFactory` — acquires write lock, updates both `f.configs[name]` and `f.registry.ProviderConfigs[name]`
- [x] SP-022-1d: Add SSRF validation to `pkg/providerregistry/` — reject non-HTTPS endpoints, private IPs, localhost

### Phase 2: Runtime Integration
- [x] SP-022-2a: Add async `refreshFromRemote()` to `factory.init()` with `inTestBinary()` guard — fetches all remote provider configs and upserts into the global factory
- [x] SP-022-2b: Export global factory accessor from `pkg/factory/` (e.g., `GlobalFactory() *providers.ProviderFactory` or `GlobalAvailableProviders() []string`)
- [x] SP-022-2c: Fix `GetAvailableProviders()` in `pkg/configuration/init.go` to use the global factory instead of creating a throwaway instance
- [x] SP-022-2d: Add `PROVIDER_REGISTRY_URL` env var support — default reuses same base as `MODEL_REGISTRY_URL`; support `"off"`/`"none"`/`"disabled"` to disable

### Phase 3: CI Publishing
- [ ] SP-022-3a: Create `scripts/generate-provider-index.sh` — generates `providers/index.json` listing all provider config files with timestamps
- [ ] SP-022-3b: Extend `.github/workflows/model-registry-publish.yml` — add step to copy `configs/*.json` to the GitHub Pages artifact with `schema_version` + `published_at` metadata injection via `jq`
- [ ] SP-022-3c: Publish the 7 missing provider model files (cerebras, chutes, deepseek, lmstudio, mistral, ollama-turbo, openai) — ensure `refresh_provider_catalog` covers all 11 providers (may require adding API keys for missing providers to CI secrets)

### Phase 4: Bug Fixes
- [ ] SP-022-4a: Fix `lmstudio` API key inconsistency — update `pkg/agent_providers/configs/lmstudio.json` auth type to `"none"`, regenerate `provider_gen.go`, and update `credentials/resolve.go` to consistently mark lmstudio as not requiring a key

### Phase 5: Documentation & Testing
- [ ] SP-022-5a: Add `CONTRIBUTING.md` section documenting the provider addition pattern: create JSON config → run `generate_providers.go` → open PR → CI auto-publishes
- [ ] SP-022-5b: Unit tests for `pkg/providerregistry/` — cache hit/miss, negative cache, singleflight dedup, TTL expiry, offline fallback, SSRF rejection
- [ ] SP-022-5c: Unit tests for `UpsertConfig()` — concurrent read/write safety, both maps updated atomically
- [ ] SP-022-5d: Integration test: embedded-only mode (no remote) works correctly; remote configs merge over embedded
- [ ] SP-022-5e: Verify `make build-all` passes after all changes

## Open

- [ ] SP-008-C1-testEmbedDownloadTimeout: Embedding-dependent agent tests (`TestRetrieveProactiveContext_*`, `TestEmbedAndStoreTurn_*`) hang the entire `pkg/agent` suite when the ONNX model isn't cached or the network is degraded — `embedding.ModelDownloader.downloadFile` (`pkg/embedding/model_downloader.go:165`) blocks on an `net/http` body read with no timeout. Add a context/HTTP timeout to the downloader and `-short`/offline skips on these tests so the suite can never hang indefinitely.

- [ ] webui-coldHydrate-largePayloadFixture: Several `TestHandleColdHydrateRequest_*` cases that stream ≥1MB through the in-process WebSocket pair (`newTestingConnPair` in `pkg/webui/cold_hydrate_test.go`) are now skipped — the WS pair fails mid-stream and the read helper used to panic with "repeated read on failed websocket connection". Affected: `EstimateSeconds/medium_~1MB`, `EstimateSeconds/~2MB`, `EstimateSeconds/~4MB`, `BinaryAtBoundary`, `LargeNonBinaryIncluded`. Replace the fixture with one that handles large buffered writes (e.g. a real `net.Pipe` paired with `gorilla/websocket` over an `httptest.Server`), then drop the `t.Skip` calls.

- [ ] webui-credPut-validationMock: `TestPutProviderCredential_UpdatesGetResponse` in `pkg/webui/settings_api_credentials_test.go` is skipped because the PUT handler now calls `validateAndSetCredential` → `configuration.ValidateAndSaveAPIKey`, which performs a real ListModels API call to the provider. Fake test keys always 400. Either mock `ValidateAndSaveAPIKey` (preferred) or thread a test-mode escape hatch through `credentials` that skips validation, then re-enable the test.

- [ ] agent-subagentFallback-stubProvider: `TestHandleRunSubagent_NoPersona_WithDelegatablePersona` in `pkg/agent/tool_handlers_subagent_test.go` is skipped because the subagent fallback path spawns a real provider (`openrouter`/`openai/gpt-5`) and never returns under `-race`, hanging the whole `pkg/agent` suite for the 10-min test timeout. Wire a stub provider into the subagent runner for tests and re-enable.
