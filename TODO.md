# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## test-isolation: TestRunAgentWorkflowLoop runs live git against the developer worktree

Observed 2026-08-19 while running `go test ./pkg/workflow/` on a dev
machine: mid-run the repo acquired a conflicting `git merge` (8 files
UU + conflict markers), five `git reset` entries in the reflog, a
`git stash` ("Cerebras client" message, base commit never in the
reflog), and a staged edit to `packages/ui/src/components/ChatPanel.tsx`
— none issued by the developer. The 10-minute `TestRunAgentWorkflowLoop`
hang coincided. Worktree was recovered manually (merge state cleared,
files restored to HEAD); HEAD never moved.

Root cause to pin down: the loop test exercises `RunWorkflowLoopInProcess`
which constructs real agents; some path reaches real git (commit tool /
automate workflow runner / agent-created `git merge`) against the CWD
repo instead of a fixture. Also worth auditing `cmd/agent_workflow*`
tests for the same class.

Fix shape (pick after diagnosis):
- Run the loop against a throwaway clone: `t.TempDir()` + `git init` +
  copy a minimal fixture, `os.Chdir` (or set the workspace root the
  config already supports) so every git op lands in the fixture.
- Alternatively assert on a fake git runner seam if one exists; add one
  if it doesn't.
- Add a leak detector: snapshot `git status --porcelain` + reflog length
  at test start, fail the test if they changed (guards the whole
  package, catches future regressions of this class).

**Gate:** `go test ./pkg/workflow/ -count=1` passes on a developer
machine with a dirty worktree and leaves the worktree byte-identical
(no reflog growth, no status change). **~2-3 hours.**

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
