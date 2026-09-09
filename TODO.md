# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## bg-sessions: inactivity expiry kills long-running quiet watchers

Observed 2026-09-09 while watching a ~55m CI run via a background
`gh run watch` session: the session repeatedly vanished mid-run
("session was closed, reaped, or the PTY died before finishing", later
"session not found"), forcing manual re-polls. Root cause is
`BackgroundProcessManager.cleanup()` (`pkg/agent_tools/background_process_log.go`):
any session whose `LastPolled` (or `StartedAt`) is older than the 2h
inactivity expiry is killed and deleted — but *watcher* sessions
(`gh run watch`, `tail -f`, log followers, wait-loops) are silent for
hours while perfectly healthy. LastPolled measures our attention, not
the process's liveness.

- [ ] Rework the expiry signal: reap only when the process is actually
      dead (pid probe), or when alive AND idle where "idle" accounts for
      process activity (CPU time / output-file mtime), not just
      LastPolled. A sleeping-but-alive watcher must survive.
- [ ] Add an explicit escape hatch: `--ttl` duration at promotion time
      (default keeps current 2h) and/or a `sprout shell-bg keepalive ID`
      subcommand that touches the session file so agent-driven watchers
      can renew intentionally.
- [ ] Tests: quiet-but-alive session survives past the old 2h expiry;
      dead session still reaped after the 5-min idle window; ttl flag
      honored; keepalive resets the timer.
- [ ] `shell-bg list/status` output stays accurate for long-lived
      sessions (no phantom 'exited' after the expiry pass).

Exit: a 3h+ silent watcher survives; suite green including new expiry
tests; no behavior change for interactive background shells.

---