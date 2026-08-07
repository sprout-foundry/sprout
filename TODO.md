# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## SP-136: Daemon-First Architecture — Shared Process Model

Full spec: `roadmap/SP-136-daemon-first-architecture.md`

The daemon already deduplicates model loads, inference permits, and index
writers *within one process*. The missing piece is routing CLI processes
through the daemon instead of each loading its own 155 MB model and racing
on the same index files. These phases are sequential — each builds on the
previous and ships independently.

---

- [x] **SP-136 P0: File-lock index writes (urgent correctness fix)** — Two processes writing to the same HNSW index directory corrupt the graph and records JSON. `HNSWStore` uses an in-process `sync.Mutex` with no cross-process locking. Add `flock` on `indexDir/.build.lock` before any `BuildIndex` or `UpdateFromGitDiff` call. If another process holds the lock, skip the build. Read-only queries need no lock. Fallback: if `flock` is unavailable (WASM, restricted sandbox), proceed without locking. Write a `TestConcurrentBuilders` test: two goroutines calling `BuildIndex` on the same index dir, verify no corruption. **~2 hours.** Touches `pkg/embedding/store_hnsw.go` and `pkg/embedding/index.go`. Gate: `go test ./pkg/embedding/ -run TestConcurrentBuilders -v` passes, `make build-all` clean.

- [x] **SP-136 P1: Daemon multi-workspace hardening** — Fix the coordination bugs that would make CLI-on-daemon unsafe. (1) Investigate the folder-scoped WebUI port assignment (5600x vs 56000) — the daemon should serve all workspaces from one port, routing internally by workspace root. (2) Verify N concurrent CLI sessions across K workspaces get isolated agents, isolated embedding managers, and isolated stores via the `clientContext`/`chat_sessions` machinery. (3) Add health-check-based graceful degradation: if the daemon is unhealthy (OOM, stuck inference), connected clients must detect this and fall back with a clear warning, not hang silently. Write an integration test: 5 concurrent agent sessions across 3 workspaces, all complete; daemon RSS stays bounded. **~4 hours.** Touches `pkg/webui/server.go`, `pkg/webui/client_context.go`, `pkg/webui/chat_sessions.go`. Gate: integration test passes, `make build-all` clean.

- [ ] **SP-136 P2: Lazy daemon auto-start** — When `sprout` starts and no daemon is detected, automatically start one in the background and connect to it. Detection: check `GET /health` on port 56000 or Unix socket at `~/.local/share/sprout/daemon.sock`. Startup race: use PID file + `flock` on `daemon.pid` to elect a single starter; losers wait for the winner's daemon. Lifecycle: daemon stays alive while it has active connections; a shutdown timer (60s after last disconnect) reaps idle daemons; new connections during teardown cancel the timer. Auth: Unix socket with 0600 permissions. Fallback: if daemon fails to start within 10s, CLI falls back to in-process execution. Add `SPROUT_DAEMON=0` escape hatch to force in-process. Write test: `sprout` in clean env starts daemon, connects, executes query, disconnects; daemon shuts down after timeout. **~4 hours.** New `pkg/daemon/` package. Touches `cmd/root.go`, `cmd/agent_command.go`. Gate: test passes, `make build-all` clean.

- [ ] **SP-136 P3: Embedding service via daemon socket** — CLI processes route embedding operations through the daemon via a JSON-over-Unix-socket protocol: `Embed(text)→[]float32`, `Query(text,k,threshold)→results`, `BuildIndex(workspaceRoot)→stats`, `CheckDuplicates(filePath,content)→matches`. Implement `RemoteEmbeddingProvider` implementing `EmbeddingProvider` that proxies to the daemon socket. If socket unavailable, fall back to in-process ONNX. The daemon owns the sole model copy, the sole writer per workspace index, and coordinates inference via its existing `inferenceGate`. Connection management: pool or multiplex over one socket, reconnect on transient failure. Write test: 3 CLI processes querying the same workspace — all get results from one model load, one index, zero corruption. **~4 hours.** New `pkg/embedding/remote_provider.go`, new socket handler in `pkg/webui/` or `pkg/daemon/`. Touches `pkg/agent/agent_embedding.go`. Gate: test passes, `make build-all` clean.

- [ ] **SP-136 P4: Full CLI-on-daemon (agent execution)** — The CLI becomes a thin client for all agent work. Expand the socket protocol: `Query(prompt)→stream`, `ExecuteTool(name,args)→result`, `ListSessions/CreateSession/SwitchSession`. The daemon owns agent state, conversation history, tool dispatch, embedding index; the CLI is a presentation layer. One-shot mode: `sprout agent --json "query"` connects, runs, prints, disconnects. Fallback retained: if daemon unavailable and auto-start fails, fall back to in-process. Simplify `daemonMode` flag branches once CLI-on-daemon is default. Write gate test: full CLI session over daemon — interactive mode, streaming, tools, subagents, git operations — indistinguishable from today's in-process CLI. **~4 hours.** Touches `cmd/agent_command.go`, `pkg/agent/`, `pkg/webui/`. Gate: gate test passes, `make build-all` clean, existing CLI tests pass.
