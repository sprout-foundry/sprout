# SP-062: CLI-Native Background Shell Execution

**Status:** ✅ Implemented (`BackgroundProcessManager` wired into shell dispatch)

Long-running shell commands (dev servers, file watchers, `npm run dev`,
`docker compose up`) used to block the agent's primary turn until they exited,
which meant the agent couldn't do anything else until the user manually killed
the process. SP-062 introduced a `BackgroundProcessManager` that any shell
invocation can opt into via a `&` suffix or explicit `--background` flag,
launches the process in its own process group, and exposes it through a
`background_process` tool so the agent can `list`, `check`, `tail` the output,
and `stop` later. The agent's primary turn returns immediately with a process
ID; subsequent turns can interact with the running process. This makes
"start the dev server, edit a file, run tests against it, then kill the
server" a real workflow instead of a copy-paste dance across terminals.

## Key decisions

- **Process group isolation.** Each backgrounded shell runs in its own
  process group so `stop` can SIGTERM the whole tree (the shell, the child
  it spawned, the grandchild) without leaking orphans.
- **Reuse the existing shell approval path** — backgrounded commands still
  go through `checkShellApproval`. There's no "background bypass."
- **PTY allocation is opt-in** — most background processes don't need a TTY,
  and allocating one complicates `tail` parsing. Default is no-PTY.
- **Output capture is line-buffered** so `tail` can stream without blocking
  the producer.
- **Stop is graceful-then-forceful** — SIGTERM, then SIGKILL after a
  configurable grace period.

## Artifacts

- code: `pkg/agent_tools/background_process.go` — manager + tool handler
- code: `pkg/agent_tools/shell_background*.go` — dispatch integration
- tests: `pkg/agent_tools/background_process_test.go`,
  `pkg/agent_tools/shell_background_test.go`
- companion: SP-014 (agent terminal sessions) — different scope but shared
  PTY/background patterns

Full specification archived — see git history for original content.