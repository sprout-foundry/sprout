# Sprout-Repo AUTOM/GEMFIX Worktree Runner — TODO (2026-08-24)

Scope: **sprout repo only** (`/home/aprice/dev/sprout-foundry/sprout-automate`, branch `autom-work`). Another agent is working in `platform-gap` and `platform` — never edit outside this worktree.

Items copied from root TODO.md Step 20 + GEMFIX section. **GEMFIX-2 is excluded** — already implemented in the main checkout's dirty tree (the `Response` field + `lastAssistantResponse` helper in `cmd/agent_result.go`); committing it there is the user's main-checkout action, not this runner's.

## AUTOM items

- [x] **AUTOM-1 [sprout] --detach mode: file-backed stdio for launched workflows (SHIPPED — verify + mark done)**
  - Shipped in commit ee05b4317 on sprout main (verify it's an ancestor of this worktree's HEAD `025740c3a`). `sprout automate run <wf> --detach` redirects child stdio to `.sprout/automate/logs/<sessionID>.log` (0700/0600), records OutputFilePath in the PID file, returns immediately.
  - Verify: `git merge-base --is-ancestor ee05b4317 HEAD` exits 0; build + relevant tests green (cmd/ + pkg/automate/); e2e test pins the contract. Then mark [x] with evidence — this is a verify-and-mark item, not new code.
  - Note: session discovery (status/logs/stop) cwd-relativity is AUTOM-2, separate.
  - **Evidence 2026-08-24 (worktree autom-work @ 025740c3a):** `git merge-base --is-ancestor ee05b4317 HEAD` exit 0; `go build ./...` clean; `go test ./cmd/... ./pkg/automate/...` all ok after `go clean -testcache` — incl. `TestAutomateRun_Detach_EndToEnd` (e2e contract), `TestOpenDetachLogFile`, `TestAutomateDetachFlagDefaults` all PASS. Full `go test ./...` has unrelated env-only failures: pkg/webui + scripts (gitignored `pkg/webui/static/*` assets absent in fresh worktree) and pkg/agent `TestFindRepoRootFromCWD` (stray `/tmp/go.mod` pollutes the walk-up fixture) — none touch the detach path.

- [ ] **AUTOM-2 [sprout] Session discovery is cwd-relative — status/logs/stop silently find nothing outside the repo root**
  - `cmd/automate_status.go`, `cmd/automate_logs.go`, `cmd/automate_stop.go` each do `filepath.Join(cwd, ".sprout")` — running `sprout automate status` from a subdirectory shows "No automate sessions found" even though sessions exist at the foundry root, and `stop`/`logs` fail the same way. Hit in practice 2026-08-21: status showed nothing while a session ran 8h+.
  - Implement: walk up from CWD to find the nearest `.sprout/automate/` directory (stop at filesystem root); if none found and `--dir` not given, fall back to the central registry dir (~/.local/state/sprout/automate) if it exists; final fallback cwd. Optional `--dir` flag on status/logs/stop to point at an explicit `.sprout` root. Fix the same pattern in `runWorkflowByPath` (PID files written relative to CWD — a run started from a subdir writes its session record where status-from-root can't see it).
  - SCOPE LEASH: implement ONLY the discovery walk-up + --dir flag. Do not fix unrelated bugs discovered along the way — note them in this file as new `[ ]` items and move on.
  - Acceptance: start a session from repo root, cd into a subdirectory, `sprout automate status` lists it; `logs` reads its log; `stop` stops it. Unit tests for the discovery walk-up with fixture dirs.

- [ ] **AUTOM-3 [sprout] Zombie window: detached child exit not reaped → status lies "running" (VERIFY-6)**
  - In --detach mode the launcher never calls cmd.Wait(). If the child exits fast, kill(pid,0) succeeds against a zombie → `automate status` shows "running" until the launcher process exits. Milliseconds in the normal case (launcher exits right after spawn).
  - Decide: accept + document, or implement the reaper (`go func() { _ = cmd.Wait() }()` before return in the detach branch — only matters for long-lived callers, e.g. an in-process agent that called run_automate).
  - Acceptance: whichever lands, a test/comment pins the behavior; no user-visible regression in status accuracy.

- [ ] **AUTOM-4 [sprout] Detached sessions can never report success — only "exited, exit -" (VERIFY-7)**
  - No finalizer in detach mode → EndedAt/ExitCode/Status stay empty forever. User can't distinguish "finished cleanly" from "killed" without tailing the log.
  - Implement (chosen approach): child-side self-finalization — `sprout agent --workflow-config` writes its own session record on exit when given a session-file path flag (e.g. `--automate-session-file <path>`, writing FinalizeSessionFile-equivalent fields with its own PID). The launcher passes the path in detach mode. This gives clean status semantics without a waiter process.
  - Acceptance: detached run finishing cleanly shows status=success + exit_code=0 in `sprout automate status`; failing run shows error + code; attached-mode behavior unchanged; unit tests for the child-side finalize helper.

- [ ] **AUTOM-5 [sprout] automate_logs help/docs: mention --detach and the log location**
  - `sprout automate run --help` text and docs/ (if an automate doc exists) should mention --detach, the log-file location, and that `logs` follows until process exit.
  - Acceptance: help text updated; no doc drift.

## GEMFIX items

- [ ] **GEMFIX-3 [sprout] P3: stale Gemini entries in provider catalog configs**
  - `pkg/agent_providers/configs/openrouter.json` model_overrides carries only ancient entries (gemini-1.5-flash/pro, gemini-2.0-flash-exp); `deepinfra.json` has zero gemini entries while DeepInfra now serves 7 (2.5-flash/pro, 3.7-flash, 3-pro-image, 3.1-pro, 3.1-flash-lite). Live model-list fetch populates pickers and unknown models fall back to default_context_limit, so this is hygiene + correct context limits.
  - Fix: refresh overrides with current models' real context limits (openrouter /api/v1/models and deepinfra /v1/openai/models expose them), drop the 1.5/2.0-exp entries.
  - Acceptance: `GetContextLimit("google/gemini-2.5-flash")` returns the real limit (1,048,576), not the default. Unit test pins it.
