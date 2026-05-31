# TODO

## Open

- [ ] SP-008-C1-testEmbedDownloadTimeout: Embedding-dependent agent tests (`TestRetrieveProactiveContext_*`, `TestEmbedAndStoreTurn_*`) hang the entire `pkg/agent` suite when the ONNX model isn't cached or the network is degraded — `embedding.ModelDownloader.downloadFile` (`pkg/embedding/model_downloader.go:165`) blocks on an `net/http` body read with no timeout. Add a context/HTTP timeout to the downloader and `-short`/offline skips on these tests so the suite can never hang indefinitely.

- [ ] webui-coldHydrate-largePayloadFixture: Several `TestHandleColdHydrateRequest_*` cases that stream ≥1MB through the in-process WebSocket pair (`newTestingConnPair` in `pkg/webui/cold_hydrate_test.go`) are now skipped — the WS pair fails mid-stream and the read helper used to panic with "repeated read on failed websocket connection". Affected: `EstimateSeconds/medium_~1MB`, `EstimateSeconds/~2MB`, `EstimateSeconds/~4MB`, `BinaryAtBoundary`, `LargeNonBinaryIncluded`. Replace the fixture with one that handles large buffered writes (e.g. a real `net.Pipe` paired with `gorilla/websocket` over an `httptest.Server`), then drop the `t.Skip` calls.

- [ ] webui-credPut-validationMock: `TestPutProviderCredential_UpdatesGetResponse` in `pkg/webui/settings_api_credentials_test.go` is skipped because the PUT handler now calls `validateAndSetCredential` → `configuration.ValidateAndSaveAPIKey`, which performs a real ListModels API call to the provider. Fake test keys always 400. Either mock `ValidateAndSaveAPIKey` (preferred) or thread a test-mode escape hatch through `credentials` that skips validation, then re-enable the test.

- [ ] agent-subagentFallback-stubProvider: `TestHandleRunSubagent_NoPersona_WithDelegatablePersona` in `pkg/agent/tool_handlers_subagent_test.go` is skipped because the subagent fallback path spawns a real provider (`openrouter`/`openai/gpt-5`) and never returns under `-race`, hanging the whole `pkg/agent` suite for the 10-min test timeout. Wire a stub provider into the subagent runner for tests and re-enable.
