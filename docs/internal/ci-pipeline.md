# CI Pipeline Details

CI (`.github/workflows/build.yml`) matrix-tests on ubuntu/macOS/Windows, then runs a WASM smoke test and builds 6 binaries.

## Gates to run before pushing

```bash
make vet && make fmt-check && make lint && make build-all   # fast (~30s)
SKIP_NETWORK_TESTS=1 make test-coverage                      # slow (~15min)
make prepare-grammars && bash scripts/wasm-tool-roster-smoke.sh   # WASM
```

## `//go:embed` of gitignored directories

`pkg/webui/static/` and `pkg/ast/grammars/bin/` are generated at build time and gitignored.

1. **Add a tracked placeholder** via `.gitignore` `!` exception (see `pkg/webui/static/placeholder`). Keeps `go vet` green on clean checkouts.
2. **Run `make prepare-grammars`** in any CI step that compiles standalone (not through a Make target).

## Hermetic test requirements

- Use `newTestAgent(t)` / `createTestAgentWithTempConfig(t)`, never `agent.NewAgent()` directly.
- Scope env with `t.Setenv`.
- Guard network tests behind `SKIP_NETWORK_TESTS` or credential skip.
- Set `GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`, `GIT_MERGE_AUTOEDIT=no` in test helpers to prevent editor hangs.

## Map iteration flakiness

Don't take the first entry from `range` over a map — Go randomizes order. Filter to one satisfying preconditions.

## Background goroutine field guarding

Every field a background goroutine reads must go through the same mutex as setter methods. Snapshot under RLock, release before I/O.

## Windows Go 1.25 workarounds

Search `STATUS_DLL_INIT_FAILED` for context:
1. No-test packages filtered from `go test -coverprofile`.
2. Windows test step uses `continue-on-error` + `TEST_COVER=no`.
