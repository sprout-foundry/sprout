Process incomplete roadmap items from PLAN.md in the current working directory.

For each `- [ ]` item:

1. Read **PLAN.md** and identify the first incomplete `- [ ]` item.
2. Look in the **roadmap/** directory for a spec file (e.g.
   `roadmap/SP-016-conformance-test-suite.md`) whose subject matches the
   item. Each spec has Requirements, Technical Design, Files to Create or
   Modify, and Acceptance Criteria sections — use that as the implementation
   guide. The item text includes an `SP-016-Xx` reference ID that maps to
   sections in the spec.
3. Create a task_queue entry for the item (status=in_progress) using
   the task tool.

4. Implement the work yourself by delegating to specialized subagents
   using `run_subagent` (serialized — NEVER `run_parallel_subagents`,
   which has caused file conflicts in this repo).

   For each delegation, the subagent should follow these rules:

   a) **Coder** — give it the task description, relevant file paths from
      the spec, and the acceptance criteria. Wait for completion.

   b) **Build verification** — after the coder reports back, run
      `go build ./...`. If it fails, delegate the specific error to the
      coder for a fix. Iterate until the build passes.

   c) **Tester** — delegate to the `tester` persona to write tests for
      every feature implemented. Per seed convention:
        - Use `test.NewHarnessWithT(t)` for all new e2e tests
        - Configure mocks via `h.Provider().AddTextResponse(...)` or
          `h.Executor().AddToolResult(...)`
        - Assert events with `h.AssertEventPublished(events.EventTypeX)`
        - Keep tests deterministic: avoid real network calls, real file
          I/O, or time-dependent logic without mocking
      Wait for the tester to report back.

   d) **Run tests** — `go test ./...`. If anything fails, delegate fixes
      to `coder` or `debugger` as appropriate. Iterate until green.

   e) **Code reviewer** — delegate to `code_reviewer` to perform a
      deep evidence-based review:
        - Read the staged diff via `git diff --cached`, then each
          changed file for full context
        - Analyze correctness, edge cases, error handling, security,
          code quality
        - Verify the spec's acceptance criteria are satisfied
        - Verify `go build ./...`, `go vet ./...`, `go test ./...`
          all pass
        - Report findings with file:line, severity (HIGH / MEDIUM /
          LOW / NIT), and clear descriptions. If clean, the reviewer
          says `REVIEW_STATUS: APPROVED`

   f) **Fix review findings** — for every HIGH and MEDIUM finding,
      delegate to `coder` with the specific issue. Re-run tests. Then
      re-review (back to step e). Loop until APPROVED.

   g) **Report back** — the orchestrator phase ends when build +
      tests + review all pass.

5. After the implementation completes, verify the orchestrator actually
   delegated to subagents (its output must show `run_subagent` calls
   to `coder`, `tester`, `code_reviewer`). If it did the work directly,
   treat as a failure and retry with a stronger reminder.
6. Run `go build ./...` and `go test ./...` one final time as a
   sanity check.
7. If build/tests fail at this final step, delegate another fix pass
   and re-verify.
8. Open the matching roadmap spec file and ensure every now-satisfied
   acceptance-criterion checkbox is `- [x]`.
9. Review staged changes with `git diff --cached`. Commit using the
   `commit` tool with the `notes` parameter (NOT the `message`
   parameter — `notes` lets the LLM generate a conventional commit
   message from your summary). Pass the roadmap item description and
   a brief summary of what changed.
10. Mark the PLAN.md item `- [x]` using `edit_file`.
11. Update the task_queue entry to completed.
12. Move to the next `- [ ]` item.

## Rules

- Process at most **50 roadmap items per session**.
- If a subagent fails or the build cannot be fixed after 2 attempts,
  mark the task_queue entry as failed with a description of the error,
  then continue to the next item.
- Do **NOT** use `git add .` or `git add -A` — only stage specific
  files you created or modified.
- Do **NOT** use `git push` — commits stay local until the user pushes
  manually.
- **Commit after each roadmap item**, not in bulk.
- Skip items already marked `- [x]`.
- Stop when no `- [ ]` items remain (or after 50 items processed).
- **Max 400 lines per file.** If a file exceeds this, extract logical
  units into separate files.
- **Run `make check`** before marking any task complete.

## Isolation rules for working alongside other active changes

- Focus ONLY on the current roadmap item. Do NOT modify, revert, or
  delete any other active changes that exist in the working tree or
  change sets.
- Do NOT run `git checkout`, `git restore`, `git reset`, or any command
  that would alter existing staged or unstaged changes that are not
  yours.
- If a build or test fails due to conflicts with OTHER unrelated
  changes (not caused by your current roadmap item work): pause for
  2 minutes, then retry. Repeat up to 3 times (total delay of up to
  6 minutes).
- After 3 failed retries due to external conflicts, stop and mark the
  task_queue entry as blocked with a summary of the conflicting
  changes. Escalate to the user — do NOT attempt to resolve other
  changes yourself.
- Pass these isolation rules verbatim when delegating to subagents.
