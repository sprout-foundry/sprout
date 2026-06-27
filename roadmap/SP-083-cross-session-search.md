# SP-083: Cross-Session Search — Find Past Conversations by Content

**Status:** 📋 Spec
**Date:** 2026-06-27
**Depends on:** none (sessions are JSON-on-disk in `~/.sprout/sessions/scoped/<hash>/session_<id>.json`)
**Priority:** Medium (high user value, low technical risk)
**Effort Estimate:** ~1–2 days

## Problem

A user who has been using sprout for a few weeks accumulates dozens of saved sessions. The `/sessions` command lists them newest-first and lets you load one by number, picker, or session ID — but **there is no way to find a session by what was said inside it**.

Concretely, "what did I do last time the OpenAI auth was breaking?" or "find the session where I fixed the embedding index" requires the user to remember which date / working directory the session was in, open `/sessions`, eyeball the picker labels (which only show the *first user message* or the *human name*, both often vague), and click around. After ~10 sessions this is a real chore.

Sessions are stored as plain JSON in `~/.sprout/sessions/scoped/<hash>/session_<id>.json`. The structure is well-defined (`SessionInfo` + `messages[]` with `role` + `content`). Building a search index is straightforward.

## Goals

1. CLI: `/search <query>` slash command (and an optional `sprout search <query>` subcommand for scripting).
2. WebUI: Sessions sidebar gains a search input that filters the list live.
3. Both: Results show session name, working directory, date, and the matching message excerpt with the search term highlighted.
4. Index is built lazily on first search; persisted to `~/.sprout/sessions/search-index.json` so subsequent searches are sub-100ms even with hundreds of sessions.
5. Index rebuilds automatically when a session is saved (incremental) or via `sprout search --reindex`.

## Design

### Index format

```go
// pkg/search/session_index.go (new)
type SessionIndex struct {
    Version   int                          `json:"version"`
    BuiltAt   time.Time                    `json:"built_at"`
    Sessions  map[string]SessionIndexEntry `json:"sessions"`
}

type SessionIndexEntry struct {
    SessionID    string    `json:"session_id"`
    Name         string    `json:"name"`
    WorkingDir   string    `json:"working_directory"`
    LastUpdated  time.Time `json:"last_updated"`
    TotalCost    float64   `json:"total_cost"`
    MessageCount int       `json:"message_count"`
    Tokens       map[string][]int `json:"tokens"` // [start, end] byte offsets in text
    Text         string    `json:"text"`         // Concatenated user/assistant messages, lowercased
}
```

The `Text` field is a single lowercased concatenation of all `role=user` and `role=assistant` `content` strings, separated by `\n`. The `Tokens` map records where each token (user/assistant message boundary) sits in `Text` so result excerpts can map back to original messages.

### Build flow

1. Walk `~/.sprout/sessions/scoped/<hash>/session_*.json`.
2. For each session file, parse the JSON, build the `SessionIndexEntry`, update the in-memory map.
3. Write atomically (`write tmp, rename`) to `~/.sprout/sessions/search-index.json`.
4. Record `mtime` of each session file; skip unchanged sessions on subsequent builds.

### Search flow

1. Load index (or build if missing).
2. For each query term, scan `entry.Text` for occurrences; record (entry, position).
3. Rank: exact phrase > all terms present > any term present; tie-break by `LastUpdated` desc.
4. Format result: name, date, working directory, and a 200-char excerpt around the first match with `[brackets]` around the matched substring.

### Incremental update on session save

Hook into `pkg/agent/persistence.go::SaveSession` (or its wrapper). On save, update the in-memory index entry and persist (debounced — at most once per 5s to avoid disk thrash on bursty workflows).

### CLI

`/search <query>` slash command — same machinery as `/sessions` (registered in `pkg/agent_commands/commands.go`).

```
$ /search "embedding index"
3 sessions matched:

  [2026-06-20] embeddings-pop-2025 — /home/u/proj
    "...rewriting the embedding index after the schema migration..."

  [2026-05-14] test repro — /tmp/repro
    "...the embedding index seems to be holding stale results..."

  [2026-04-02] migrate-emb — /home/u/proj
    "...build the new embedding index from the conversation store..."

Use '/sessions <#>' to load, or '/search --reindex' to rebuild the index.
```

Flags:
- `--reindex` — force full rebuild
- `--cwd <dir>` — restrict to sessions in a specific working directory
- `--since <date>` / `--until <date>` — date range
- `--limit <N>` — max results (default 20)
- `--json` — machine-readable output for scripting

### WebUI

The `Sessions` sidebar (`webui/src/components/Sidebar.tsx` or wherever the session list lives) gets a search input at the top. Typing filters by name **and** triggers a `/api/sessions/search?q=...` request that returns full results with excerpts. The results pane below the input shows the matches with the same format as the CLI. Clicking a result loads the session.

`/api/sessions/search` is a new endpoint in `pkg/webui/`. It reuses the same index and same result formatting as the CLI command; the WebUI gets `name`, `working_directory`, `last_updated`, `total_cost`, `excerpt`, `session_id` per hit.

### Tests

- `pkg/search/session_index_test.go`: build/load round-trip, incremental update after file mtime change, persistence.
- `pkg/search/session_search_test.go`: ranking, phrase vs term match, date filter, working-directory filter, limit, JSON output.
- `pkg/webui/search_api_test.go`: endpoint contract, empty query handling, malformed index fallback (returns error, forces rebuild).
- `webui/src/components/SessionsSearch.test.tsx`: typing filters, click loads session, empty state.

### Phase plan

| Phase | Scope |
|-------|-------|
| 1 | `pkg/search/` package — index format, build, load, incremental update, search query, result formatting. |
| 2 | `pkg/agent_commands/search.go` slash command + flag parsing + tests. |
| 3 | `pkg/webui/sessions_search_api.go` HTTP handler reusing `pkg/search`. |
| 4 | WebUI sidebar search input + result list + click-to-load wiring. |

## Success Criteria

- `grep -rn "SearchSession\|searchHistory\|searchAcrossSessions" pkg/ webui/src/` (excluding this spec) returns nothing pre-implementation.
- `/search "embedding"` returns matches in <100ms once the index is built.
- Index rebuild is incremental: saving a new session updates the index without scanning all sessions.
- WebUI sidebar search filters and lists matches with excerpts; clicking loads the session.
- `sprout search --json` emits a parseable JSON array for scripting.
- All tests green; `make build-all` clean.

## Risks

- **Index corruption** — partial writes, mid-build crashes. Mitigation: atomic rename; if the index is missing or fails to parse, the search path rebuilds from scratch.
- **Privacy** — the index contains the full text of user/assistant messages. Same risk profile as the session files themselves (already on disk in plaintext). No new attack surface. Mitigation: the index file is in the same directory as the session files; same permissions model.
- **Stale index after manual session deletion** — if a user deletes a session file out-of-band, the index entry remains. Mitigation: on load, cross-check session IDs against the live directory; drop missing entries (re-validate without rebuilding from scratch).

## Open Questions

1. Should `/search` search across all sessions regardless of working directory, or scope to current directory by default? **Recommendation:** default to current working directory (matches `/sessions` behavior); `--all` flag for global. This is less surprising than discovering "the embedding session" came from a project the user no longer works in.
2. Should the index include tool-call payloads? **Recommendation:** no — only `role=user` and `role=assistant` `content`. Tool payloads dominate disk size and are noisy to search.