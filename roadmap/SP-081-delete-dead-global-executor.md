# SP-081: Delete the Dead `pkg/tools/global.go` Executor

**Status:** 📋 Spec
**Date:** 2026-06-27
**Depends on:** none
**Priority:** Low (cleanup; removes a misleading TODO and dead code path)
**Effort Estimate:** ~1 hour

## Problem

`pkg/tools/global.go` exposes a global tool executor (`InitializeGlobalExecutor` + `GetGlobalExecutor` + `ExecuteWithGlobal`) initialized with a hardcoded "allow all" permission checker carrying a TODO:

```go
// TODO: Make this configurable based on security settings
permissions := NewSimplePermissionChecker([]string{
    PermissionReadFile,
    PermissionWriteFile,
    PermissionExecuteShell,
    PermissionNetworkAccess,
    PermissionUserPrompt,
    PermissionWorkspaceRead,
    PermissionWorkspaceWrite,
})
```

Verified via `grep -rn "tools\.GetGlobalExecutor\|tools\.InitializeGlobalExecutor\|tools\.ExecuteWithGlobal" pkg/ cmd/`: **zero non-test callers**. Nothing in the production codebase ever calls these functions, so:

- The TODO is misleading — there's no consumer to "make configurable based on security settings" for.
- The hardcoded allow-all permission set never gates any real operation.
- The file occupies build time and cognitive overhead for no benefit.

SP-074 already established that the new `pkg/agent_tools` registry is the canonical tool-execution path; the global executor was a vestige of an earlier design.

## Goals

1. Delete `pkg/tools/global.go` outright.
2. Delete `pkg/tools/executor_behavior_test.go` if its tests only exist to exercise `pkg/tools/global.go`'s wiring. If they exercise the `NewSimplePermissionChecker` / `NewExecutor` constructors for *other* legitimate purposes, keep them and just drop the global-executor-dependent cases.
3. Remove `pkg/tools` from any package lists, build manifests, or docs that reference it (none expected, but verify).

## Design

### Audit phase

Before deleting, confirm:

```bash
# All callers in non-test code
grep -rn "tools\.GetGlobalExecutor\|tools\.InitializeGlobalExecutor\|tools\.ExecuteWithGlobal" pkg/ cmd/

# Test file purpose
head -20 pkg/tools/executor_behavior_test.go
```

If the audit confirms zero callers and the tests are exclusively about the global executor, delete both files. If `executor_behavior_test.go` exercises other code paths, prune the global-executor cases and keep the rest.

### Phase plan

| Phase | Scope |
|-------|-------|
| 1 | Audit callers (the grep above). Confirm zero non-test callers. |
| 2 | Delete `pkg/tools/global.go`. |
| 3 | Delete `pkg/tools/executor_behavior_test.go` if its only purpose was the global executor. |
| 4 | `go build ./...` and `go test ./...` to confirm nothing else referenced it. |

## Success Criteria

- `pkg/tools/global.go` does not exist.
- `grep -rn "tools\.GetGlobalExecutor\|tools\.InitializeGlobalExecutor\|tools\.ExecuteWithGlobal" pkg/ cmd/` returns zero matches.
- `pkg/tools/executor_behavior_test.go` either does not exist, or no longer references the global executor.
- `go build ./...` clean.
- `go test ./pkg/tools/...` (if directory remains) clean.

## Risks

- **Hidden caller via reflection or a generated file.** Mitigation: the grep above is exhaustive across `pkg/` and `cmd/`; reflection-based access to a package-level function is unusual and would have surfaced in code review.
- **Test scaffolding depended on the global executor's init.** Some test setup files may call `InitializeGlobalExecutor` to seed the global state. Mitigation: the grep covers all of `pkg/` and `cmd/`; if any test file uses it, the audit will surface it and the deletion becomes a refactor (move the init into the test itself with the same allow-all permission set) rather than a straight delete.

## Open Questions

None.
