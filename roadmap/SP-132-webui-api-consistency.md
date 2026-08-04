# SP-132 — WebUI API Consistency & Boilerplate Elimination

## Problem

A systematic audit of `pkg/webui/` (2026-08-04) found widespread boilerplate
and inconsistency in HTTP handler patterns. The webui has 228 handler
functions across ~90 files, and the same low-level patterns are copy-pasted
hundreds of times with slight variations — creating maintenance burden,
inconsistent error responses, and missed error handling.

### Finding 1: Method-guard boilerplate (134 copies)

134 handlers open with the same 3-line guard:

```go
if r.Method != http.MethodGet {
    http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
```

Problems:
- Uses `http.Error` (plain text) instead of `writeJSONErr` (JSON) — API
  clients expecting JSON get plain text for method errors.
- Copy-pasted 134 times across 16+ files.
- Some handlers guard multiple methods with repeated checks.

### Finding 2: Bare JSON encoding (183 calls)

183 sites call `json.NewEncoder(w).Encode(payload)` directly instead of
using the `writeJSON` helper. These:
- Don't set `Content-Type: application/json`
- Don't use a consistent encoding path
- Silently ignore encode errors

### Finding 3: Two overlapping error helpers

- `writeJSONError(w, status, message)` — 147 callers, sets `{"error": msg}`
- `writeJSONErr(w, status, code, message)` — 283 callers, sets `{"error": msg, "code": code}`

Both serve the same purpose. Should be consolidated to one.

### Finding 4: Inconsistent error response format

377 `http.Error` calls (plain text) vs 430 `writeJSONErr/writeJSONError`
calls (JSON). The frontend receives different response formats depending on
which error path triggers. The 116 "Method not allowed" responses are the
largest source of inconsistency.

### Finding 5: Files exceeding 500-line limit (16 files)

| File | Lines |
|------|-------|
| chat_sessions.go | 809 |
| settings_api_credentials.go | 774 |
| client_context.go | 697 |
| settings_api_partial_settings.go | 689 |
| search_semantic_api.go | 676 |
| onboarding_api.go | 670 |
| git_api_review.go | 668 |
| search_api.go | 626 |
| terminal_create.go | 594 |
| api_query.go | 589 |
| settings_api_put.go | 578 |
| terminal_types.go | 562 |
| websocket_handler_mode.go | 558 |
| chat_sessions_worktree_api.go | 541 |
| instances_api.go | 524 |
| settings_api_mcp.go | 519 |

### Finding 6: Functions exceeding 200 lines (complexity hotspots)

| Function | File | Lines |
|----------|------|-------|
| handleTerminalWebSocket | terminal_websocket.go | 365 |
| runChatQuery | api_query_shared.go | 319 |
| ExecuteCommandAndWait | terminal_agent_exec.go | 277 |
| runConnectionLiveLoop | websocket_handler_mode.go | 270 |
| launchSSHWorkspace | ssh_launch_workspace.go | 256 |

### Finding 7: cs.Agent race potential

15 sites access `chatSession.Agent` with inconsistent locking — some under
`cs.mu.Lock()`, some under `ws.mutex`, some without clear ownership.

### Finding 8: Unrecovered goroutines

~5 bare `go func()` calls still lack panic recovery (file_watcher,
terminal_lifecycle, server_lifecycle, run_buffer_subscriber, file_browser).
These are long-running goroutines with different lifecycle patterns than
the fire-and-forget sites addressed in SP-131 Phase 4.

## Goals

1. **One method-guard helper.** `requireMethod(w, r, allowed)` returns false
   + writes JSON 405 on mismatch. Eliminates 134 copy-paste blocks.
2. **All JSON responses via `writeJSON`.** No bare `json.NewEncoder` calls
   outside the helper itself.
3. **One error helper.** Consolidate `writeJSONError` into `writeJSONErr`
   with an optional code parameter.
4. **Consistent error format.** Migrate `http.Error` calls to `writeJSONErr`
   where the response goes to an API client.
5. **Split oversized files** per SP-075 conventions.

## Phases

### Phase 1: `requireMethod` helper (Finding 1)

Add a `requireMethod(w, r, method) bool` helper that checks the method and
writes a JSON 405 on mismatch. Migrate the 134 method-guard sites.

### Phase 2: Consolidate error helpers (Finding 3)

Make `writeJSONErr` accept an optional code (variadic or separate function).
Migrate all 147 `writeJSONError` callers. Remove `writeJSONError`.

### Phase 3: Migrate bare JSON encoding (Finding 2)

Replace 183 `json.NewEncoder(w).Encode()` calls with `writeJSON(w, 200, payload)`.

### Phase 4: Migrate `http.Error` to JSON (Finding 4)

Replace `http.Error` calls with `writeJSONErr` where the response goes to
an API endpoint (not WebSocket handlers, which have different error semantics).

### Phase 5: Split oversized files (Finding 5)

Split the 16 files over 500 lines following SP-075 conventions. Priority:
chat_sessions.go, settings_api_credentials.go, client_context.go.

### Phase 6: cs.Agent locking audit (Finding 7)

Audit all 15 `cs.Agent` access sites and ensure consistent lock ownership.

### Phase 7: Long-running goroutine recovery (Finding 8)

Add panic recovery to the ~5 remaining bare `go func()` sites that aren't
suited for `SafeGo` (long-running loops with cleanup).
