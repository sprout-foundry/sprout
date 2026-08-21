# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## test-isolation: TestRunAgentWorkflowLoop suite hang — RESOLVED 2026-08-20

Original hypothesis (live git against the dev worktree) was disproven:
an instrumented full run left `git status`/HEAD/reflog byte-identical.
The actual root cause of the 10-minute suite hang was a hardcoded
`/tmp/test_sprout_retry_count` counter in the retry scenario — on hosts
where `/tmp` is not writable (Termux) the build step failed on every
invocation, the ScriptedClient exhausted its scripted responses and
returned nil, and the loop spun to MaxIterations until the `go test`
timeout killed the suite.

Shipped fixes:
- `pkg/workflow/run_loop_test.go`: counter now lives in `t.TempDir()`
  via a `__COUNTER_FILE__` placeholder substituted per-subtest. Suite
  runtime 422s-timeout-hang → ~5s.
- `pkg/workflow/git_leak_test.go`: `TestMain` guard snapshots
  `git status --porcelain` + `git rev-parse HEAD` before/after the
  package run and fails the run if either changed (regression guard
  for the original incident class; skip with `SPROUT_SKIP_GIT_LEAK_CHECK`).

---

## SP-136: Daemon-First Architecture — Shared Process Model

Status: ✅ All 5 phases shipped 2026-08-07 (P0 file-lock index writes,
P1 multi-workspace hardening, P2 lazy daemon auto-start, P3 embedding
service via Unix socket, P4 full CLI-on-daemon). See `git log --grep=SP-136`
for the per-phase commits; spec body deleted from `roadmap/` per index
policy.

The daemon deduplicates model loads, inference permits, and index writers
across CLI processes; the CLI routes embedding + one-shot agent work through
it with in-process fallback and `SPROUT_DAEMON=0` / `SPROUT_DAEMON_AGENT=0`
escape hatches. Follow-on workstream (open): `roadmap/SP-136-cross-platform-local-llm.md`.
