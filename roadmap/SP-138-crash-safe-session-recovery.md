# SP-138 — Crash-Safe Session Persistence & Interrupted-Turn Recovery

Status: 📋 Specified (not started)

## Problem

When a session is hard-stopped mid-turn — SIGKILL, OOM kill, terminal/app
closure, power loss, or daemon crash — the conversation state of the
in-flight turn is unrecoverable. This has caused significant real work loss.

Root causes, verified in code:

1. **Saves happen only at turn boundaries.** `autoSaveState()`
   (`pkg/agent/state.go:297`) is invoked exclusively from the deferred block
   in `ProcessQueryWithContinuity` (`pkg/agent/conversation.go:52`). There is
   no periodic save (the "on a timer" comment in
   `pkg/agent/testing_state_isolation.go:120` is stale — no ticker exists).
   A process death during `ProcessQuery`/`processQueryWithSeed` loses every
   message, tool call, and intermediate result since the previous completed
   turn. For long agentic turns this can be an hour or more of work.
2. **Session writes are non-atomic.** `SaveStateScoped`
   (`pkg/agent/persistence_message.go:141`) writes the full state JSON with a
   bare `os.WriteFile(stateFile, data, 0600)`. A crash mid-write leaves a
   truncated file; `LoadStateWithoutAgentScoped` then fails at `json.Unmarshal`
   and the *entire session* — not just the last turn — becomes unreadable.
   There is no backup, no temp+rename, no fsync. (Contrast:
   `pkg/agent/change_tracking_shell_persist.go:184` already does atomic
   tmp+rename for ChangeTracker revisions.)
3. **No repair of dangling tool exchanges.** When a turn is interrupted
   cooperatively (Ctrl+C → `TriggerInterrupt`), the deferred save *does* run,
   but the saved history can end with an assistant message containing
   `tool_calls` whose results were never produced. No orphan-repair code
   exists — `pkg/agent/e2e_orphaned_tool_result_removal_test.go` contains two
   skipped stubs and no implementation. Resuming such a session sends
   malformed histories to providers (OpenAI/Anthropic reject `tool_use`
   blocks without matching results).
4. **No crash detection or recovery UX.** `cmd/recent_sessions.go` and the
   WebUI sessions API (`pkg/webui/sessions_api.go`) list sessions with no
   notion of "ended abnormally". Nothing prompts the user to recover, and
   nothing tells the resumed agent what happened.

What is already durable: file edits made during the turn (ChangeTracker
revisions persist independently, atomically). It is the *conversation*
state — the expensive-to-reproduce part — that is lost. Recovery should
leverage this: a recovered agent can be pointed at `list_changes` /
`view_history` to re-verify what its interrupted turn already did to disk.

## Non-Goals

- Distributed / multi-host session sync.
- Replacing the JSON session format with SQLite/bbolt (revisit only if the
  journal proves insufficient).
- Resuming a turn *mid-flight* (re-issuing the in-progress LLM call). We
  recover state; the user or auto-resume decides whether to re-prompt.
- Journaled subagent sessions in v1 — subagent output is folded into the
  parent turn; if the parent crashes mid-subagent, the parent journal still
  records the turn up to the last completed parent iteration. Tracked as
  future work.
- Changing SP-110 wakeup/auto-resume semantics (recovery composes with it:
  a recovered session is a loadable session, and auto-resume may re-prompt).

## Design

### Phase 1 — Atomic, crash-safe session writes (foundation)

- Extract a `writeFileAtomic(path, data, perm)` helper (temp file in the same
  directory → write → `Sync` → close → `os.Rename`; optional dir fsync),
  following the existing pattern in `change_tracking_shell_persist.go`.
  Place it in `pkg/utils` or `pkg/agent` and reuse it there.
- `SaveStateScoped` switches to the helper. A crash mid-save can now only
  leave the previous good file in place.
- On load: if the main file is somehow still corrupt (pre-existing damage),
  attempt `<file>.bak` if present (Phase 1 also writes a one-generation
  `.bak` of the previous good state before rename — cheap insurance, since
  session files are rewritten, not appended).
- `DeleteSessionScoped` removes the `.bak` alongside the main file.

### Phase 2 — Turn journal (WAL) for in-flight turns

- New append-only file per session, next to the state JSON in the scoped
  session dir: `session_<id>.journal.jsonl`.
- Lifecycle: created when a turn starts (`processQueryWithSeed` entry),
  appended during the turn, and **deleted on successful full save at the
  turn boundary** (the deferred `autoSaveState`). Journal presence therefore
  means "this session ended with a turn in flight" — the crash-detection
  signal.
- Event schema (one JSON object per line, versioned `"v":1`):
  - `{"type":"turn_start","query":..., "ts":...}`
  - `{"type":"messages","msgs":[...],"base":N}` — appended at each seed-loop
    iteration boundary (after each assistant message + its tool results are
    synced back; anchor: `syncSeedStateToSprout`, `pkg/agent/seed_query.go`),
    recording the *new* messages since `base` (the message count at turn
    start). Snapshot-delta rather than per-mutation hooks keeps the write
    points few, well-defined, and race-free (no interception of internal
    state mutations).
  - `{"type":"turn_checkpoint","cp":...}` when
    `RecordTurnCheckpointAsync` fires mid-turn.
  - `{"type":"token_totals",...}` at iteration boundaries (cost/tokens).
- Write policy: append + flush per iteration. Full `fsync` per append is
  config-gated (`session_journal_fsync`, default off — OS-level durability
  is enough for SIGKILL/terminal-close, which is the actual failure mode;
  power-loss coverage is opt-in).
- Failure containment: journal write errors are logged and never fail the
  turn. WASM (`js` build tag) gets a no-op journal — same shape as the OOM
  watchdog's platform split.
- Cleanup: `DeleteSessionScoped` and `cleanupMemorySessions`
  (`persistence_session.go`) must remove journals with their sessions; the
  retention counter counts sessions, journals ride along.

### Phase 3 — Load-time recovery and repair

- New `LoadStateRecoverable(sessionID, workingDir)` (or an option on
  `LoadStateWithoutAgentScoped`): load base JSON; if a journal exists,
  replay events in order onto the state (replace messages after `base`,
  apply checkpoints/token totals). A partial final line (crash mid-append)
  is ignored by design — JSONL tail tolerance.
- **Repair pass** on any loaded state (also benefits non-journaled,
  interrupted-before-save states): walk the tail of `messages` and
  - drop tool-result messages whose `tool_call_id` no longer matches an
    assistant `tool_calls` entry,
  - strip trailing assistant `tool_calls` with no matching results
    (provider-breaking case), retaining the assistant text if any.
  Implement as a pure function `RepairMessageTail([]api.Message) ([]api.Message, RepairReport)`
  with unit tests — this finally lands the intent behind the two skipped
  orphan tests.
- Mark recovered state: `ConversationState` gains `InterruptedAt *time.Time`
  and `RecoveredFromJournal bool` (omitempty — old files unaffected).
- On `ApplyState` of a recovered session, inject a system-supplement note to
  the agent: the session was interrupted mid-turn, state is recovered to the
  last completed tool iteration, and disk may contain edits from the lost
  portion — verify with `list_changes` / `view_history` before redoing work.
  Mirrors the existing `Context From Previous Session` supplement pattern in
  `ProcessQueryWithContinuity`.

### Phase 4 — Crash detection UX

- `SessionInfo` gains `Interrupted bool` (journal exists ⇒ true).
  `readSessionInfo` (`persistence_session.go`) checks for the journal file.
- CLI `cmd/recent_sessions.go` picker: "(interrupted — recoverable)" badge;
  selecting it loads via the recovery path and prints what was recovered
  (turn start time, iterations replayed).
- WebUI `pkg/webui/sessions_api.go`: expose the flag; session list shows a
  recovery indicator; opening an interrupted session loads recovered state
  and surfaces a one-line notice in the chat.
- Interactive mode startup (`cmd/agent_mode_interactive.go`): if the most
  recent session in scope is interrupted, offer to resume it (consistent
  with the existing load mechanism at `agent_mode_interactive.go:96`).
- Daemon (`pkg/webui/api_query_shared.go` query goroutine): unchanged — it
  already funnels through `ProcessQueryWithContinuity`, so journalling is
  inherited. Daemon restart simply sees interrupted sessions via the API.

### Phase 5 — Signal hardening (narrow the window further)

- CLI: while a turn is active, SIGTERM/SIGHUP (already captured in
  `pkg/console/signal_compat_unix.go`) triggers the cooperative interrupt —
  which lets the deferred save run — plus a watchdog: if the interrupt
  hasn't completed within ~3s, perform a best-effort synchronous
  `autoSaveState` under the state lock and exit. Today a second Ctrl+C or an
  impatient kill during a stuck tool call bypasses the save entirely.
- Document (in `sprout agent --help` text): first signal = graceful stop +
  save, second = force. SIGKILL is unrecoverable by definition — the journal
  is the answer there, not signal handling.

## Testing

- **Phase 1:** unit test that a simulated failed rename/write leaves the
  prior file intact; `.bak` fallback loads when main file is corrupt.
- **Phase 2:** journal lifecycle — created at turn start, appended per
  iteration, removed at turn boundary (test via mock provider +
  `agent.NewTestAgent`/`newTestAgent(t)` per AGENTS.md; state isolation via
  `NewTestStateDir`). Simulated crash: truncate the journal mid-line and
  assert replay ignores the tail. Concurrency: journal writes vs.
  `RecordTurnCheckpointAsync` under `-race`.
- **Phase 3:** `RepairMessageTail` table tests (dangling tool results,
  trailing tool_calls, clean tail, empty history). Replay determinism
  golden-file test. Recovered-flags set correctly; old-format files load
  unchanged.
- **Phase 4:** `readSessionInfo` interrupted-flag test; picker badge is UI
  smoke-level.
- **Phase 5:** integration test sending SIGTERM to a busy agent process
  (guarded by `SKIP_NETWORK_TESTS` pattern / mock provider) asserting the
  saved file exists and contains the turn's messages.
- All tests hermetic via existing `NewTestStateDir` /
  `createTestAgentWithTempConfig` isolation.

## Rollout / Compatibility

- Purely additive on disk: journals are new files; unknown to older readers.
  New `ConversationState` fields are `omitempty`.
- No config migration. New optional settings (`session_journal_fsync`)
  default sensibly.
- Behavior change is strictly positive: corrupt files now recoverable,
  interrupted sessions now flagged. Worst case (journal replay bug) is
  bounded by repair + the `.bak` generation.

## Suggested sequencing

Phases 1 → 2 → 3 are the core value chain (atomic writes, WAL, recovery)
and should land in that order as separate reviewable PRs; 4 (UX) and 5
(signals) can proceed in parallel once 3 is in.

## References

- `pkg/agent/conversation.go:52` — the only `autoSaveState` call site
- `pkg/agent/state.go:297` — `autoSaveState`
- `pkg/agent/persistence_message.go:141` — `SaveStateScoped` (non-atomic write)
- `pkg/agent/seed_query.go` — `syncSeedStateToSprout`, iteration loop (journal anchor)
- `pkg/agent/e2e_orphaned_tool_result_removal_test.go` — skipped orphan-repair stubs
- `pkg/agent/change_tracking_shell_persist.go:184` — existing atomic-write pattern
- `cmd/recent_sessions.go`, `pkg/webui/sessions_api.go` — session list surfaces
- `pkg/console/signal_compat_unix.go` — signal capture points
- Related shipped work: SP-071 (rewind/checkpoints), SP-077 (ChangeTracker
  durability), SP-110 (auto-resume), SP-073 (cooperative cancellation)
