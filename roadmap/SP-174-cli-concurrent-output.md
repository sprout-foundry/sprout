# SP-174 — CLI Concurrent Output: Subagents and Background Tasks

**Status:** Implemented (2026-11-02). 174a–e landed. Two of the
original assumptions were wrong, and both were corrected during
implementation:

- **174b.** The subscriber's subagent "done" line never fired, because
  nothing published the `status: completed` activity it waited for. The
  duplicate was the opener, not the closer.
- **174c.** Parallel runs printed no per-run done line at all, so the
  completion block is new output rather than a replacement for an
  existing line.
**Origin:** user report: *"Tasks are not handled well. Output is not
organized, but just streamed as it comes."* The user later narrowed "tasks"
to subagents and `/task` background tasks.

This follows SP-171 (line integrity) and SP-172 (`/task`). SP-171 made each
individual write land in the right place. It did not decide **what** to write
when several agents produce output at once, and that is the problem this spec
addresses.

## Problem

The interactive CLI has one transcript and one spinner. Several producers can
write to them concurrently:

- the primary agent;
- foreground subagents (`run_subagent`);
- parallel subagents (`run_parallel_subagents`);
- background subagents;
- `/task` agents, which are full agents running in their own worktrees.

Nothing groups their output.

### 174a. Subagent events cannot be attributed

Subagent tool events carry `active_persona` and `subagent_depth`, but nothing
that names the run (`persona.go` `MergeEventMetadata`). Two parallel workers
with the same persona are indistinguishable. The subscriber keys its spawn
announcements, collapse runs and done lines by `depth:persona`
(`pkg/cliui/terminal_subscriber.go`), so they merge.

### 174b. A foreground subagent is announced three times

A single delegation produced three openers:

1. the `run_subagent` tool row;
2. the spawner's `▸ subagent [coder] starting · provider/model`
   (`tool_handlers_subagent_spawn.go:38`);
3. the subscriber's `↳ [coder] spawned: …` (`FormatSpawnLine`), printed
   whenever the subagent's first tool event happened to arrive — after any
   prose the subagent had already streamed.

The subscriber's matching done line (`FormatSubagentDoneLine`) was dead
code: nothing published the `status: completed` activity it waited for.

### 174c. Parallel subagents stream prose line by line, interleaved

Each subagent's stream callback splits chunks into lines and prints each one
immediately with a `[persona:taskID]` tag (`subagent_task.go:174-243`). N
subagents produce N interleaved streams. Their tool lines share one spinner
and one collapse state, so concurrent tools also overwrite each other's
spinner label.

### 174d. `/task` output appears as the primary's own

A task agent shares the primary's event bus. It copies the primary's
metadata and adds only `task_id` (`tasks_agent.go:137-145`), which nothing in
the CLI reads. Its tool lines, its `query_started` spinner and its todo panel
render as if the primary produced them, mid-turn or between turns.

### 174e. Background lines skip the transcript writer under contention

`PrintLine` checks whether a prompt is on screen with `outputMu.TryLock()`.
When the lock is busy (a spinner frame, another subagent's line) it writes
straight to stderr, with no scroll-region parking (`console_lock.go`). Under
parallel load, lines land on pinned rows.

## Design

### 174a. Attribution

When a subagent is created, it merges `subagent_task_id` (its run ID) and
`subagent_parallel` (true for parallel runs) into its event metadata, so every
event it emits carries both. The subscriber keys per-run state by task ID and
falls back to `depth:persona` only when the ID is absent.

### 174b. The spawner owns subagent openers and closers

The spawner prints at the right moment for every depth, so it keeps the
opener and closer. Its opener gains the task's first line
(`subagent [coder] starting: refactor auth · provider/model`). The
subscriber's late spawn line, its per-turn dedupe state and the dead done
line are removed.

Each run now publishes a per-run `subagent_activity` event with
`status: completed|cancelled` and its `task_id`. The CLI uses it to retire
the run's live state. The WebUI's per-task resource counts
(`useSubagentRuns`) already expected this status but never received it.

### 174c. Parallel subagents: a live status line and one block per run

While parallel subagents run:

- **No streamed prose.** The stream callback buffers each run's prose
  instead of printing it. The subagent's result already reaches the parent as
  the tool result, and the parent's `run_parallel_subagents` call lands in the
  `/expand` ring, so nothing is lost.
- **No per-tool transcript lines.** Tool events tagged `subagent_parallel`
  update a per-run status entry instead of printing. The spinner label shows
  the fleet, e.g. `3 subagents · coder:t1 read_file · reviewer:t2 thinking`.
- **One block per completed run,** written in a single transcript write so it
  cannot interleave: the done line, then up to six tail lines of the run's
  output, indented beneath it.

A single foreground subagent keeps streaming. It is the only producer, so its
lines read in order, and watching it work is the point of a foreground run.

### 174d. `/task` events stay out of the primary transcript

The subscriber drops every event that carries `task_id` before dispatch:
tool start/end, `query_started`, stream chunks, todo updates, agent messages
and progress events. A task's progress is visible through `/tasks`, which
already reads the registry. Its completion still arrives through the wakeup
queue (SP-172), which is the one moment the primary needs to hear about it.

### 174e. Background lines wait for the transcript writer

`PrintLine` waits a bounded time (250ms) for the output lock instead of
giving up on the first busy check. Once it has the lock, it writes through
`writeTranscriptLocked`, or through the active reader's `printExternalLocked`
when a prompt is on screen. It still falls back to a raw write after the
bound, so a caller that already holds the lock cannot deadlock.

## Non-goals

- **A pane or alt-screen view per task.** SP-172 declined this, and the
  reasoning still holds.
- **WebUI changes.** The WebUI already renders `subagent_activity` per task.
- **Live-updating rows per subagent in the pinned footer.** This is a
  possible follow-up if the single spinner label proves too dense with many
  workers.

## Testing

- Subscriber integration tests publish events through a live subscriber and
  assert what reaches stdout, stderr and the indicator's writer:
  - `task_id` events never reach the transcript;
  - a concurrent run's tool lines stay off it, while its warnings still
    print.
- Fleet unit tests cover per-run labels for same-persona workers and their
  retirement.
- Agent tests cover the completion block (tail and earlier-lines marker)
  and the foreground / parallel / background classification.
- `PrintLine` under a held lock: the line waits for the holder, and the
  bounded fallback still fires.
