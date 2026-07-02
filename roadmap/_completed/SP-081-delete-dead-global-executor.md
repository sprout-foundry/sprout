# SP-081: Delete the Dead `pkg/tools/global.go` Executor

**Status:** ✅ Implemented (2026-06-30; dead global executor and its test file deleted)

`pkg/tools/global.go` exposed a global tool executor (`InitializeGlobalExecutor` + `GetGlobalExecutor` + `ExecuteWithGlobal`) initialized with a hardcoded "allow all" permission checker and a misleading TODO. An exhaustive grep confirmed zero non-test callers across `pkg/` and `cmd/`. The file was a vestige of an earlier design, superseded by the `pkg/agent_tools` registry established in SP-074. Both `pkg/tools/global.go` and its exclusive test file `pkg/tools/executor_behavior_test.go` were deleted outright.

## Key decisions

- Straight delete rather than refactor — the global executor had zero callers and no legitimate consumers.
- `executor_behavior_test.go` deleted entirely since it exclusively exercised the global executor wiring.
- No replacement needed — `pkg/agent_tools` registry is the canonical tool-execution path.

## Artifacts

- code: `pkg/tools/global.go` — deleted (was dead code)
- code: `pkg/agent_tools/registry.go` — canonical replacement for global executor
- tests: `pkg/tools/executor_behavior_test.go` — deleted (exclusively tested global executor)

Full specification archived — see git history for original content.
