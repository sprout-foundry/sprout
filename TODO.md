# TODO

Active work tracked here. Each item is a small, independently committable
unit for the workflow automation (~30 min – 2 h). Every item cites its spec
section — read the cited spec before starting. Completed work lives in git
history; finished sections are removed.

Validation gate for every item: `make vet && make fmt-check && make lint &&
make lint-go-new && make build-all`, plus bounded tests for the packages the
item touched (`go test -p 2 ./<pkg>/...`, never a bare `go test ./...`).
Webui items additionally: `cd webui && npx prettier --check` on changed files
and `make test-webui-vitest`.

Items are ordered so nothing precedes what it depends on. Work that needs a
human decision or external accounts is listed under **Not automatable** at
the end, without checkboxes.

---

## Benchmark and CLI fixes (found in the first real benchmark run)

A one-task run (static-site/add-about-section, ai-worker/qwen3.8-27b)
recorded "fail" with `Result: null`, `Err: null` and an empty failure
section: the turn-end verification never ran. Reproduced by hand, the same
model made the correct edit to `src/pages/about.astro`, so the harness, not
the model, lost the result.

- [x] **bench.8** Benchmark runs always verify: find why the run's turn
      reported no changed application paths (the verification gate stayed
      closed) — check whether edits made through shell commands, or by a
      subagent, are missing from the turn's change window
      (`Agent.TurnChangedPaths`) and whether `ProcessQueryWithContinuityAs`
      opens the window in the benchmark path. Fix the root cause; as a
      backstop the runner initializes a git baseline in each fresh copy
      and, when the tracker reports no changes but `git status` shows
      changed application paths, runs verification anyway. Tests: a
      scripted run editing through the edit tool, through a shell
      command, and through a subagent all produce a verification result.
      Spec: SP-154 §154b, SP-149.
- [x] **bench.9** Report the reason, never a silent fail: a run without a
      verification result records why (the hook's not-verified reason:
      no code changes, verification disabled, setup error, timeout) and
      the report lists it under failure categories. Test: the empty
      "Failure categories: No failures" next to a failed run can no
      longer happen. Spec: SP-154 §154c.
- [x] **bench.10** Keep evidence for failed runs: save each failed run's
      working copy diff (`git diff` against the baseline), the agent
      transcript and the verification output under
      `<output>/runs/<task>-<model>-<n>/` (`--keep-runs=failed|all|none`,
      default `failed`). Spec: SP-154 §154c.
- [x] **cli.1** `sprout agent` must not silently hand a turn to a daemon of
      a different binary or config: today, when a daemon is already
      running (e.g. the web UI's backend), the CLI forwards the turn to it
      ("Running via daemon at …/agent.sock") even when the invoked binary
      is a different version and `--isolated-config` names another
      config. Run in-process when the daemon's version or config differs
      from the invoking binary's (or refuse with a clear message and a
      `--no-daemon` flag), and always print which binary and config ran
      the turn. Tests for version mismatch and isolated config.

## Tool robustness

- [ ] **tool.1** `read_file` range requests that miss: a coordinator called
      `read_file` on a ~1000-line `TODO.md` 14 times in a row, each time
      getting the same middle-truncated output while asking for "lines
      615-680", until it fell back to `sed -n`. The tool takes the range as
      `view_range: [start, end]` and silently ignores any other argument
      name. Fix: reject unknown arguments with an error that names the
      valid ones (`path`, `view_range`), or accept the common aliases
      (`start_line`/`end_line`, `offset`/`limit`, `line_start`/`line_end`)
      and map them; when output is truncated, the notice states the total
      line count and the exact `view_range` to read the omitted part. Add a
      repeat guard: the same `read_file` call with identical arguments a
      third time in one turn returns a short note ("identical to your last
      read — use view_range [a, b] for lines a-b") instead of the content
      again. Tests for each.

## Not automatable

- Resolved 2026-10-07: starter frameworks, local storage emulation and the
  Pages/Workers rule (see "SP-153 reference starters" above).
- **Decision + accounts:** running the benchmark on real models and
  publishing results — needs API keys and spend; also decide where
  results live and the run cadence (SP-154 open questions).
- **Accounts:** live Cloudflare end-to-end deploy of each reference
  starter (preview, then confirmed production) — needs a real Cloudflare
  account and token (SP-156 acceptance).
