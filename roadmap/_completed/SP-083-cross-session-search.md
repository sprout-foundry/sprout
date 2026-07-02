# SP-083: Cross-Session Search — Find Past Conversations by Content

**Status:** ✅ Implemented (2026-06-30; /search slash command, sprout search subcommand, sidebar search)

Users accumulating dozens of sessions had no way to find a past conversation by its content — only by date or first message. This spec added a lazy-built, persisted search index over all session files, with incremental updates on session save. The CLI got `/search <query>` and `sprout search <query>` (with --json for scripting), and the WebUI sidebar gained a live search input that filters sessions with excerpts.

## Key decisions

- Index is built lazily on first search; persisted to `~/.sprout/sessions/search-index.json`
- Incremental updates on session save with 5s debounce to avoid disk thrash
- Only user/assistant message content is indexed (tool-call payloads excluded)
- Ranking: exact phrase > all terms present > any term, tie-break by last updated
- Index cross-checks against live directory on load to drop entries for manually deleted sessions

## Artifacts

- code: `pkg/search/session_index.go` — index format, build, load, incremental update
- code: `pkg/search/session_search_test.go` — ranking, phrase vs term match, filters
- code: `pkg/agent_commands/search.go` — /search slash command + sprout search subcommand
- code: `pkg/webui/sessions_search_api.go` — /api/sessions/search HTTP handler
- tests: `pkg/search/session_index_test.go` — build/load round-trip, incremental update
- tests: `webui/src/components/Sidebar.sessionSearch.test.tsx` — sidebar search integration

Full specification archived — see git history for original content.
