# SP-144 — CLI Handling: Errors, Command Surface, REPL

Status: Implemented (2026-09-29) — 144.1–144.4 landed together. Foundry
integration run against the new binary: the task-runner contract tests
(`internal/task/contract_test.go`, which pass the retained
`--skip-prompt --output-json --output-path` spellings) pass;
`platform/COMPATIBILITY.md` names the canonical flags and the 1/2/130 exit
split. Two platform failures (`cmd/sprout-foundry` test build,
`TestEndpointManifestCoverage`) reproduce identically with the prior
v0.18.13 binary and are unrelated.

## Problem

An audit of the built binary (every subcommand's `--help` walked, edge
inputs probed with an isolated `SPROUT_CONFIG`, the REPL driven in a pty
against `--mock-llm`) found the CLI works but handles its edges unevenly.

### Errors & exit behavior

1. **One exit code for everything.** `Execute()` maps every error to
   `os.Exit(1)` (`cmd/root.go:136`). A bad flag, a missing arg, a provider
   400, and a Ctrl-C-aborted one-shot are indistinguishable to a script.
2. **Raw `log.Printf` on stderr before the real message.** Any command
   that loads config prints `[config] Warning: failed to get key … from
   keyring` lines (`pkg/configuration/api_keys.go:142,201`) ahead of its
   own error when the keychain read fails. REPL startup prints
   `[computer-use] chord watcher …` lines
   (`pkg/agent/computer_use_registration.go:85`,
   `panic_key_chord_*.go:50`) because `redirectGoLogToWorkspace` is
   installed after agent init.
3. **Arg validation inside RunE instead of cobra.** `sprout explain` with
   no args prints a usage paragraph *and* `✗ [InvalidInput] no input
   provided for tool "shell_command"`.
4. **Conflicting flags silently resolved.** `sprout upgrade --check
   --rollback` runs the rollback.
5. **Bad input reported as success.** `sprout automate status --dir
   /nonexistent` exits 0 with "No automate sessions found."

### Command & flag surface

6. **Confirmation-bypass flags use three names:** `--yes` (automate,
   embeddings clear, service, upgrade), `--force` (audit clear),
   `--skip-prompt` (agent, commit, pr, review). `history clear` has both
   `--force` and `--yes`.
7. **Output flags:** `--json` (automate status, explain, search, shell-bg
   list, diag computer-use), `--output-json` (agent), `--format` (export,
   export-training, policy dump/export). Path flags: `--output` (export, plan,
   export-training), `--output-path` (agent), `--out` (txn-pull).
8. **Directory flags:** `--dir` (automate, sync, txn-*), `--cwd` (search),
   `--workspace` (history clear).
9. **Model selection:** `agent`/`plan`/`shell` take `--model` +
   `--provider`; `commit`/`review` take `--model` only.
10. **`skill` vs `skills`** are sibling top-level commands (install/list
    vs allow/list/revoke).
11. **Flat, ungrouped top level (35 commands).** Foundry plumbing
    (`txn-pull`, `txn-push`, `txn-status`, `sync`) and the E2E helper
    `serve` ("with a mock LLM") are listed alongside `agent` and `commit`.
    `log` ("revision history") and `history` ("project history") read as
    the same thing.

### Interactive REPL

12. **Slash-command errors print twice.** `cmd/agent_query.go:135-137`
    prints `✗ Slash command error: …` then returns the error unmarked, so
    the REPL prints `✗ Error: slash command failed: …` again. The "did you
    mean" list offers `/?`, rendering as `did you mean /m or /??`.
13. **The recent-sessions picker eats typeahead.** "Any other key start
    fresh" consumes the dismissing key; a paste of `/nosuchcmd⏎` while the
    picker is up arrives as `md`. After dismissal, pasted input arrives
    intact.
14. **`ToolTimeline` is dead code.** `pkg/console/tool_timeline.go` is
    constructed only by its tests, though comments in `cmd/agent_modes.go`
    (87, 532) describe it as wired. SP-048's "tool timeline + silence-fill
    pending → see SP-101" points at a spec that was never written.
15. **Two turn summaries that disagree.** `✓ turn complete · 0.0s` is
    followed by `this turn: … · 187ms`. The first formats with `%.1fs` and
    shows the *session* cost (`GetTotalCost`) as if it were the turn's;
    the second never shows cost (`PrintPerTurnSummary` passed `0`).

### Found while implementing

16. **`sprout pr` and `sprout review` exit 0 on failure.** Both used
    cobra `Run` (not `RunE`) and printed `Error: …` themselves.
17. **Slash-command output is invisible in the WebUI for 15 commands.**
    The WebUI captures command output through
    `CommandRegistry.SetOutput`; only commands implementing
    `OutputCommand` honor it. `/verbose`, `/max-context`, `/risk-profile`,
    `/persona`, `/mcp`, `/keys`, `/model`, `/provider`, `/skill`,
    `/custom`, `/clear`, and the `/subagent-*` family printed to
    `os.Stdout` directly — the browser got 0 bytes.
18. **Output style drift.** Pre-SP-057 bracket tags (`[info]`, `[compact]`,
    `[index]`, `[transcript]`, `[...]`, `[~]`), three heading styles
    (`=== X ===`, underlined `X\n=====`, `## X`), outcome lines with no
    glyph or legacy `Error:`/`WARNING:`/`Note:` prefixes, hard-coded
    glyph characters that skip color/color-blind handling, 7-space
    continuation indents, and `console.Colorize`/`Color*` constants that
    ignored `NO_COLOR`.

## Design

### 144.1 — Error & exit contract

- Exit codes: `0` ok · `1` runtime failure · `2` usage error (unknown
  command/flag, arg-count, flag conflict, invalid flag value) · `130`
  interrupted. A `usageError` type (`cmd/cli_error.go`) carries code 2;
  `rootCmd.SetFlagErrorFunc` and the `Args` validators wrap into it;
  `Execute()` maps error → code in one function (unit-tested table).
- Foundry callers (`seed/process/process.py`, the platform task runner)
  only test `!= 0`, so splitting 1/2/130 is compatible.
- Usage errors render as `✗ <msg>` plus one dim hint line — cobra's
  "did you mean" candidates, a command-specific usage line, or `Run
  'sprout <cmd> --help' for usage.` — never the flag dump. Cobra's
  flag-group messages are rewritten (`--check and --rollback can't be
  used together`). Cobra messages detected by prefix are pinned by
  `TestExitCodeForCobraErrors`.
- Invalid flag *values* checked in RunE (`--format`, `--older-than`,
  `--since`, `--reasoning`, `--type`, `--source`, policy tier, keys backend
  mode) return `usageErrorf`/`usageErrorAt` → exit 2.
- Signal-driven force quits (`agent_modes.go`, `steer_coordinator.go`)
  exit 130.
- Go `log` output goes to `<state>/logs/cli.log` (lumberjack-rotated,
  same limits as the daemon log) from `PersistentPreRunE` whenever stderr
  is a terminal; piped/CI runs keep stderr logging, `SPROUT_DEBUG=1` opts
  back in. Keyring fallbacks are log-only — a command that actually needs
  the key fails with its own error.
- `initializeSystem` returns a hinted error instead of printing and
  calling `os.Exit(1)`.
- Fixes: `explain` returns a usage error carrying the expected invocation
  (it can't use `ExactArgs` — `--tool write_file --path x` has no
  positional); `upgrade` marks `check`/`rollback`, `rollback`/`version`,
  `rollback`/`pre-release` mutually exclusive; `automate status|logs|stop
  --dir` errors when the dir doesn't exist.

### 144.2 — Command & flag vocabulary

Canonical names, with old spellings kept as hidden deprecated aliases
(cobra `MarkDeprecated`, prints a one-line notice) for one minor release:

| Concept | Canonical | Deprecated aliases |
|---|---|---|
| skip confirmation | `--yes`, `-y` | `--force`, `--skip-prompt` |
| machine-readable stdout | `--json` | `--output-json` |
| write result to file | `--output`, `-o` | `--output-path`, `--out` |
| target directory | `--dir` | `--cwd`, `--workspace` |
| LLM selection | `--model` + `--provider` on every LLM command | — |

- `skills allow|revoke|list` fold into `skill allow`, `skill revoke`,
  `skill allowlist` (`skill list` already lists installed skills);
  `skills` keeps working with cobra's `Deprecated` notice.
- Cobra command groups in root help: **Core** (agent, plan, commit,
  review, pr, shell), **Sessions** (history, search, export,
  export-training, log), **Automation** (automate, shell-bg),
  **Configuration** (config, keys, custom, mcp, lsp, skill, policy,
  service, embeddings), **Diagnostics** (diag, explain, audit, version,
  upgrade). `txn-*`, `sync`, `serve` become `Hidden: true` (names
  unchanged — foundry invokes them).
- `log` / `history` Short strings rewritten: `log` shows file-change
  revisions, `history` prunes stored revisions/changes/run logs; each
  points at the other.
- `commit` and `review` gain `--provider/-p` (and `-m`); `agent --json`
  / `-o/--output` become canonical; internal self-invocations (`automate
  run`, the automate tool handler, `commit`) pass `--yes`; `/commit`
  accepts `--yes`/`-y`. `agent --automate-session-file` (set only by
  `automate run --detach`) is hidden.
- Guard tests: `TestFlagVocabularyIsCanonical` walks `rootCmd` and fails
  on any visible retired spelling; `TestEveryVisibleTopLevelCommandHasAGroup`
  fails on an ungrouped top-level command.
- The root Long text drops the duplicated "run sprout alone" paragraph.

Contract exception: foundry's task runner invokes `sprout agent
--no-web-ui --skip-prompt --output-json --output-path …`
(`platform/docker/entrypoint.sh`), `seed/process/process.py` runs `sprout
commit --skip-prompt`, and `platform/COMPATIBILITY.md` documents
`--output-json` as the Mode A contract. On `agent` and `commit` those
three stay **permanent hidden aliases with no deprecation notice** (a
stderr notice would land in the runner's captured logs). Only the
non-contract spellings (`--force`, `--cwd`, `--workspace`, `--out`) get
`MarkDeprecated`. Run foundry's `make test-integration` before merging
144.2 and update `COMPATIBILITY.md` to name the canonical flags alongside
the retained aliases.

### 144.3 — REPL polish

- Slash errors: `ProcessQuery` prints one `✗` line plus a hint and returns
  `markReported(err)`. Suggestions name canonical commands (aliases route
  to their command), skip punctuation aliases, and never exceed a
  candidate's own length in edit distance.
- Session picker: the picker reads 8 bytes at a time and only the first
  byte of a dismissing read was forwarded. It now forwards the whole
  printable run of that read (`printableRun`); later bytes already reach
  the REPL.
- `ToolTimeline`: superseded by `cliui.StartTerminalToolSubscriber`, which
  already renders the per-tool timeline — deleted. SP-048's index row no
  longer points at SP-101.
- Turn summary: the REPL owns it (`cliui.SetREPLOwnsTurnSummary`) and
  prints one `⎯ this turn: … ⎯` line with the real per-turn cost delta;
  the event-driven `✓ turn complete` line remains only for one-shot runs
  and uses the shared `CompactDuration`/`CompactCost` formatters.
- The Ctrl+C double-press hint renders as a dim hint line.

### 144.4 — Output conventions

The rules, applied across `cmd/`, `pkg/agent_commands`, `pkg/cliui`:

- **Stream:** a command's *result* (data a script would capture — a URL,
  a list, JSON) goes to stdout, plain. Status, progress, outcomes, and
  guidance go to stderr. `sprout pr` prints only the PR URL on stdout.
- **Glyph-led lines** for every outcome (`✓ ✗ ⚠ ⓘ → ⏹`), via
  `console.Glyph*` — never a hard-coded glyph character, never a text
  prefix (`Error:`, `WARNING:`, `Note:`, `[tag]`).
- **Hints** (secondary guidance under a glyph line) via `console.Hintln`:
  2-space indent, dim, no glyph.
- **Headings** via `console.Heading`: bold title, no banners, underlines,
  or `##`.
- **Detail rows** under a heading or outcome indent 2 spaces.
- **Color** only through helpers that honor `NO_COLOR`/`FORCE_COLOR`:
  `Colorize`/`ColorizeBold`/`BoldText` now gate at the source, and raw
  SGR constants are spliced via `console.Esc(code)`.
- **Slash commands the WebUI can run** (`SafeDuringSteer`) embed
  `outputSink` and print through `out()`;
  `TestSteerCapableCommandsWriteThroughRegistryOutput` enforces it.
- Commands use `RunE`; failures are returned, never printed-and-swallowed.

Frames around captured tool output (`exec`'s rules, `shell-bg`'s
`--- Output ---`) are content delimiters, not headings, and stay.

## Acceptance

- `cmd` unit table: unknown flag / missing arg / flag conflict → 2;
  runtime error → 1; context-canceled one-shot → 130.
- `sprout explain`, `sprout upgrade --check --rollback`, `sprout automate
  status --dir /nope` each print exactly one `✗` line, exit 2 (first two)
  or 1.
- No `YYYY/MM/DD HH:MM:SS [` log-prefixed lines on stderr for any
  command in the help walk (smoke test with a locked/absent keyring).
- Flag-vocabulary guard test passes; every deprecated alias still works
  and prints its notice; the foundry contract aliases work and print
  nothing.
- pty run (mock LLM): unknown slash command → one `✗` line; paste while
  picker is up → full text reaches the prompt; one turn-summary line.
- WebUI capture: every steer-capable slash command produces output
  through the registry writer.
