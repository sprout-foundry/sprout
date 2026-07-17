# Sprout Web UI Protocol Specification

> Documenting all communication between the React SPA frontend and Go backend server.

**Version**: 1.0.0
**Last Updated**: 2024-12-19
**Source Files**: `pkg/webui/*.go`, `pkg/events/*.go`, `packages/events/src/*.ts`, `webui/src/**/*.ts`

---

## Table of Contents

1. [Overview](#1-overview)
2. [REST Endpoints](#2-rest-endpoints)
   - [Query](#query)
   - [Proxy/Cloud](#proxycloud)
   - [Diagnostics](#diagnostics)
   - [Files](#files)
   - [Settings](#settings)
   - [Workspace](#workspace)
   - [Git](#git)
   - [Sessions](#sessions)
   - [Search](#search)
   - [Terminal](#terminal)
   - [Misc](#misc)
3. [WebSocket Endpoints](#3-websocket-endpoints)
4. [WebSocket Inbound Messages (Client → Server)](#4-websocket-inbound-messages-client--server)
5. [WebSocket Outbound Messages (Server → Client)](#5-websocket-outbound-messages-server--client)
6. [Reattach Flow](#6-reattach-flow)
7. [Error Envelope](#7-error-envelope)
8. [Type Generation Workflow](#8-type-generation-workflow)

---

## 1. Overview

Sprout uses a **Go backend** that serves both a **REST API** and **WebSocket** endpoints. The **React SPA frontend** communicates via:

- **HTTP/JSON** for request/response operations (REST endpoints)
- **WebSocket/JSON** for real-time events, streaming, and bidirectional communication

### Architecture Summary

```
┌─────────────────────────────────────────────────────────────────┐
│                       React SPA (Browser)                        │
│  ┌─────────────────┐  ┌──────────────────┐  ┌────────────────┐  │
│  │  REST API Calls  │  │  WebSocket (/ws) │  │  Terminal WS   │  │
│  │  /api/query,     │  │  Chat, commands, │  │  (/terminal)   │  │
│  │  /api/settings,  │  │  events, reattach│  │  PTY sessions  │  │
│  │  /api/git, etc.  │  │                  │  │                │  │
│  └────────┬────────┘  └────────┬─────────┘  └───────┬────────┘  │
└───────────┼────────────────────┼────────────────────┼────────────┘
            │                    │                    │
            ▼                    ▼                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                  Go Backend (pkg/webui)                          │
│  ┌────────────┐  ┌──────────────┐  ┌────────────────┐          │
│  │ REST Router │  │ WS Handler   │  │ Terminal WS    │          │
│  │ (mux.Handle)│  │ (AttachWS,   │  │ (handleTerminal)│          │
│  │             │  │  message     │  │                │          │
│  │             │  │  handlers)   │  │                │          │
│  └──────┬─────┘  └──────┬───────┘  └───────┬────────┘          │
│         │               │                   │                    │
│         ▼               ▼                   ▼                    │
│  ┌──────────────────────────────────────────────────────┐       │
│  │              Agent (pkg/agent)                        │       │
│  │  Query execution, tool calls, streaming events        │       │
│  └──────────────────────────────────────────────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

### Communication Patterns

| Channel | Protocol | Direction | Use Case |
|---------|----------|-----------|----------|
| REST API | HTTP/JSON | Request/Response | Configuration, Git, File ops, Settings, Query |
| Main WebSocket | WS/JSON | Bidirectional | Chat, real-time events, commands |
| Terminal WebSocket | WS/JSON | Bidirectional | PTY terminal sessions |
| LSP WebSocket | WS/JSON | Proxy | LSP language server bridge |

### Client Identification

All REST endpoints and WebSocket connections use a **client ID** to isolate state per browser tab/connection. The client ID is:
- Appended as a query parameter `?client_id=<uuid>` to all requests
- Managed by the `clientSession.ts` frontend service
- Generated as a v4 UUID on first load and persisted in `localStorage`

```typescript
// webui/src/services/clientSession.ts
export function getWebUIClientId(): string {
  const key = 'sprout.webui.clientId';
  let id = localStorage.getItem(key);
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem(key, id);
  }
  return id;
}
```

The `appendClientIdToUrl()` helper ensures every API call includes the client ID.

---

## 2. REST Endpoints

All REST endpoints are prefixed with `/api/`. Every endpoint accepts the `?client_id=<uuid>` query parameter for client isolation.

### Query

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `POST` | `/api/query` | Submit a query to the agent for processing | `{ "query": string, "image_path?: string, "image_data?: string, "conversation_id?: string, "context?: string, "persona?: string, "mode?: string, "model?: string }` | `{ "success": bool, "query_id?: string, "message?: string }` |
| `POST` | `/api/query/steer` | Send a steering message to influence a running query | `{ "steer": string, "conversation_id?: string }` | `{ "success": bool, "message": string }` |
| `POST` | `/api/query/stop` | Stop an active query | `{ "conversation_id?: string }` (body optional) | `{ "success": bool, "message": string }` |
| `GET` | `/api/query/status` | Check the status of a running query | Query: `?conversation_id=<id>` (optional) | `{ "is_processing": bool, "query": string, "conversation_id": string }` |

**Details:**
- `POST /api/query` (handler: `handleQuery` in `api_query.go`): Initiates an agent query. Accepts JSON body with query text. Optionally includes `image_path` (file path to a pasted image), `image_data` (base64-encoded image), `persona` (persona alias), `model` (specific model), and `context` (additional context). If `conversation_id` is omitted, a new conversation is created. Returns a `query_id` on success.
- `POST /api/query/steer` (handler: `handleQuerySteer` in `api_query.go`): Sends a steering/influence message to a running query. The agent incorporates it into its current reasoning.
- `POST /api/query/stop` (handler: `handleQueryStop` in `api_query.go`): Stops the current query. Can target a specific `conversation_id` or defaults to the active query for this client.
- `GET /api/query/status` (handler: `handleQueryStatus` in `api_query.go`): Returns whether the agent is currently processing a query and the active query text.

### Proxy/Cloud

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `POST` | `/api/proxy/chat` | Send message to remote cloud/proxy agent | `{ "query": string, "session_id?: string, ... }` | `{ "success": bool, "session_id": string, ... }` |
| `POST` | `/api/proxy/chat/stop` | Stop a proxy chat session | `{ "session_id": string }` | `{ "success": bool }` |
| `GET` | `/api/proxy/chat/status` | Check proxy chat status | Query: `?session_id=<id>` | `{ "is_processing": bool, ... }` |
| `GET` | `/api/proxy/stats` | Get proxy/cloud agent stats | — | Stats object |

**Details:**
- Proxy endpoints enable remote agent sessions (e.g., via SSH).
- `POST /api/proxy/chat` creates or continues a proxied agent session. Returns a `session_id` for subsequent operations.

### Diagnostics

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/stats` | Server statistics and agent status | — | `{ "provider": string, "version": string, "uptime": string, "model": string, "active_chat_id": string, "chat_session_count": number, ... }` |
| `GET` | `/api/embedding-index` | Embedding index status | — | `{ "enabled": bool, "index_size": number, "initialized": bool, "building": bool }` |
| `POST` | `/api/embedding-index` | Toggle embedding index | `{ "enabled": bool }` | Same as GET response |
| `GET` | `/api/costs/summary` | Cost summary | — | Cost summary object (incl. `total_cost`, `by_provider`, `by_model`, `last_30_days`, `by_billing_type`, `first_activity`, `last_activity`) |
| `GET` | `/api/costs/history` | Daily cost history | Query: `?days=N` (default 30) | `{ "daily_costs": [...], "days": number }` |
| `GET` | `/api/costs/detail` | Detailed cost breakdown | Query: `?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD` | `{ "total_cost": number, "by_provider": {...}, "by_model": {...}, "start_date": string, "end_date": string }` |
| `GET` | `/api/providers` | Available LLM providers | — | `{ "providers": [...] }` |
| `GET` | `/api/providers/models` | Available models | — | Models list |
| `GET` | `/api/diagnostics` | System diagnostics | — | Diagnostics object |
| `GET` | `/api/semantic` | Semantic search status/info | — | Semantic search state |
| `GET` | `/api/support-bundle` | Download support bundle (ZIP) | — | ZIP download |

**Details:**
- `GET /api/stats` (handler: `handleAPIStats` in `api_workspace.go`): Returns comprehensive server stats including provider info, version, uptime, model, active chat session, and agent statistics.
- `GET/POST /api/embedding-index` (handler: `handleAPIEmbeddingIndex` in `api_embedding_index.go`): GET returns index status. POST toggles indexing with `{ "enabled": bool }`.
- Cost tracking endpoints (`cost_tracking_api.go`): `/api/costs/history` defaults to last 30 days but accepts `?days=N`. `/api/costs/detail` accepts `?start_date` and `?end_date` in `YYYY-MM-DD` format. `/api/costs/summary` response includes `first_activity` and `last_activity` (RFC 3339 UTC strings, omitted when the store is empty) covering all recorded records independent of any date-range filter; the WebUI uses these to surface a "no activity in the last 30 days" banner when `total_cost > 0` but `last_30_days == 0`.
- `GET /api/support-bundle`: Returns a ZIP file with logs, config, and diagnostics via `Content-Disposition: attachment`.

### Files

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/files` | List workspace files | Query: `?path=<dir>&depth=N&glob=<pattern>` | `{ "path": string, "files": [...], "count": number }` |
| `GET` | `/api/file` | Read file contents | Query: `?path=<file>` | `{ "path": string, "content": string, "is_text": bool, "size": number }` |
| `POST` | `/api/create` | Create a new file | `{ "path": string, "content": string }` | `{ "path": string, "message": string }` |
| `POST` | `/api/delete` | Delete a file | `{ "path": string }` | `{ "path": string, "message": string }` |
| `POST` | `/api/rename` | Rename/move a file | `{ "old_path": string, "new_path": string }` | `{ "old_path": string, "new_path": string, "message": string }` |
| `POST` | `/api/open-in-file-browser` | Open file in system browser | `{ "path": string }` | `{ "path": string, "success": bool }` |
| `POST` | `/api/browse` | Browse directory contents | `{ "path": string }` | `{ "path": string, "files": [...], "daemon_root": string, "workspace_root": string }` |
| `POST` | `/api/file/consent` | Request/revoke file write consent | `{ "paths": string[], "granted": bool }` | `{ "message": string }` |
| `POST` | `/api/file/check-modified` | Check which files changed on disk | `{ "files": { "path": mtime } }` | `{ "modified": [{ "path": string, "mod_time": number, "size": number }] }` |
| `GET` | `/api/files/prettier-config` | Get Prettier config for workspace | — | Prettier config object or `{}` |

**Details:**
- `GET /api/files`: Supports `?path`, `?depth=N` (default 3), `?glob=<pattern>`.
- `GET /api/file`: Supports `?encoding=utf8` or `?encoding=base64`.
- `POST /api/file/check-modified` (handler: `handleAPIFileCheckModified` in `api_file_check_modified.go`): Takes `{ "files": { "path": mtime } }` where `mtime` is Unix timestamp. Registers files with the file watcher for real-time notifications.

### Settings

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/config` | Get server configuration | — | `{ "port": number, "daemon_root": string, "workspace_root": string, "agent": {...}, "features": {...} }` |
| `PUT` | `/api/config` | Update server configuration | Config object | Updated config |
| `GET` | `/api/settings` | Get agent settings | — | Settings object |
| `PUT` | `/api/settings` | Update agent settings | Settings object | Updated settings |
| `GET` | `/api/settings/mcp` | List MCP servers | — | `{ "servers": [...] }` |
| `POST` | `/api/settings/mcp` | Add MCP server | MCP server config | `{ "success": bool, "message": string }` |
| `DELETE` | `/api/settings/mcp` | Remove MCP server | Query: `?name=<name>` | `{ "success": bool, "message": string }` |
| `GET` | `/api/settings/providers` | Get provider settings | — | Provider settings |
| `PUT` | `/api/settings/providers` | Update provider settings | Provider config | `{ "success": bool }` |
| `GET` | `/api/settings/credentials` | Get stored credentials | — | Credentials list |
| `PUT` | `/api/settings/credentials` | Update credentials | Credentials object | `{ "success": bool }` |
| `GET` | `/api/settings/skills` | List available skills | — | Skills list |
| `PUT` | `/api/settings/skills` | Update skills config | Skills config | `{ "success": bool }` |
| `GET` | `/api/settings/subagent-types` | List subagent types | — | Subagent types list |
| `PUT` | `/api/settings/subagent-types` | Update subagent types | Subagent types config | `{ "success": bool }` |
| `GET` | `/api/hotkeys` | Get keyboard shortcuts | — | Hotkeys object |
| `POST` | `/api/hotkeys/validate` | Validate hotkey binding | `{ "shortcut": string, "exclude_key?: string }` | `{ "valid": bool, "conflict?: string }` |
| `POST` | `/api/hotkeys/preset` | Apply a hotkey preset | `{ "preset": string }` | `{ "success": bool }` |

### Workspace

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/workspace` | Get current workspace info | — | `{ "daemon_root": string, "workspace_root": string, "is_project": bool, "project_markers": [...], "needs_workspace_selection": bool, "recent_workspaces": [...] }` |
| `POST` | `/api/workspace` | Set workspace root | `{ "path": string }` | `{ "daemon_root": string, "workspace_root": string, "message": string }` |
| `POST` | `/api/workspace/browse` | Browse directory in workspace | Query: `?path=<dir>` | `{ "files": [...], "daemon_root": string, "workspace_root": string }` |
| `GET` | `/api/workspace/symbols` | Get workspace symbols/outline | Query: `?path=<file>&top=N` | `{ "symbols": [...], "path": string }` |
| `GET` | `/api/instances` | List running Sprout instances | — | `{ "instances": [{ "pid": number, "port": number, "workspace": string, "is_current": bool }] }` |
| `POST` | `/api/instances/select` | Select a running instance | `{ "pid": number }` | `{ "success": bool, "instance": {...} }` |

**Details:**
- `POST /api/workspace`: Rejects with `409 Conflict` (`{ "code": "query_in_progress" }`) if an agent query is active. On success, publishes a `workspace_changed` WebSocket event.

### Git

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/git/status` | Get repository status | — | `{ "staged": [...], "unstaged": [...], "untracked": [...], "ahead": number, "behind": number, "branch": string }` |
| `POST` | `/api/git/stage` | Stage files | `{ "paths": string[], "all?: bool }` | `{ "success": bool, "message": string }` |
| `POST` | `/api/git/unstage` | Unstage files | `{ "paths": string[] }` | `{ "success": bool, "message": string }` |
| `POST` | `/api/git/discard` | Discard unstaged changes | `{ "paths": string[] }` | `{ "success": bool, "message": string }` |
| `POST` | `/api/git/commit` | Commit staged changes | `{ "message": string, "all?: bool }` | `{ "success": bool, "hash": string }` |
| `GET` | `/api/git/diff` | Get diff | Query: `?path=<file>&cached=true` | `{ "diff": string, "path": string }` |
| `GET` | `/api/git/branches` | List branches | — | `{ "branches": [...], "current": string }` |
| `POST` | `/api/git/branches` | Create a new branch | `{ "name": string, "from?: string }` | `{ "success": bool, "branch": string }` |
| `GET` | `/api/git/worktrees` | List worktrees | — | `{ "worktrees": [...] }` |
| `POST` | `/api/git/checkout` | Checkout branch/commit | `{ "ref": string, "flags?: string[] }` | `{ "success": bool, "output": string }` |
| `POST` | `/api/git/pull` | Pull from remote | `{ "remote?: string, "branch?: string, "flags?: string[] }` | `{ "success": bool, "output": string }` |
| `POST` | `/api/git/push` | Push to remote | `{ "remote?: string, "branch?: string, "flags?: string[] }` | `{ "success": bool, "output": string }` |
| `GET` | `/api/git/log` | Get commit log | Query: `?max=N&path=<file>` | `{ "commits": [{ "hash", "subject", "author", "date" }] }` |
| `POST` | `/api/git/revert` | Revert a commit | `{ "hash": string }` | `{ "success": bool, "message": string }` |
| `POST` | `/api/git/deep-review` | Request deep code review | `{ "files": string[], "message": string }` | `{ "success": bool, "query_id": string }` |

### Sessions

All chat-session routes use **flat paths** (no path parameters). The chat ID is passed via request body or query parameter.

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/sessions` | List agent sessions (browser tabs) | — | `{ "sessions": [...], "count": number }` |
| `POST` | `/api/sessions/restore` | Restore a specific session from saved state | `{ "session_id": string }` | Session state object |
| `POST` | `/api/chat-sessions` | Create a new chat session | `{ "title?: string }` | Chat session object |
| `POST` | `/api/chat-sessions/create` | Create a new chat session (alias) | `{ "title?: string }` | Chat session object |
| `POST` | `/api/chat-sessions/create-in-worktree` | Create a new chat session in a specific worktree | `{ "title?: string, "worktree?: string }` | Chat session object |
| `GET` | `/api/chat-sessions` | List all chat sessions | — | `{ "sessions": [...], "count": number }` |
| `POST` | `/api/chat-sessions/delete` | Delete a chat session | `{ "chat_id": string }` | `{ "success": bool }` |
| `POST` | `/api/chat-sessions/delete-all` | Delete all non-default/non-active chat sessions | — | `{ "success": bool }` |
| `POST` | `/api/chat-sessions/rename` | Rename a chat session | `{ "chat_id": string, "title": string }` | `{ "success": bool }` |
| `POST` | `/api/chat-sessions/pin` | Pin a chat session | `{ "chat_id": string }` | `{ "success": bool, "pinned": true }` |
| `POST` | `/api/chat-sessions/unpin` | Unpin a chat session | `{ "chat_id": string }` | `{ "success": bool, "pinned": false }` |
| `POST` | `/api/chat-sessions/switch` | Switch to a chat session | `{ "chat_id": string }` | `{ "success": bool }` |
| `POST` | `/api/chat-sessions/compact` | Compact a chat session history | `{ "chat_id": string }` | `{ "success": bool, "message": string }` |
| `GET` | `/api/chat-sessions/worktree-mappings` | List chat-to-worktree mappings | — | `{ "mappings": [...] }` |
| `GET` | `/api/chat-session/` | Get worktree info for a chat session | Query: `?chat_id=<id>` | Worktree info object |

**History** (registered via `registerSessionRoutes()` in `history_api.go`):

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/history/changelog` | Get revision changelog | — | `{ "revisions": [...], "message": string }` |
| `GET` | `/api/history/revision` | Get detailed changes for a revision | Query: `?revision_id=<id>` | `{ "revision": {...} }` |
| `POST` | `/api/history/rollback` | Rollback to a previous revision | `{ "revision_id": string }` | `{ "message": string, "revision_id": string }` |
| `GET` | `/api/history/changes` | Get current session changes | — | `{ "changes": [...], "message": string }` |

**Clear History** (handler: `handleAPIChatSessionClearHistory` in `chat_sessions_api.go`):

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `POST` | `/api/chat-sessions/history` | Clear conversation history for a chat session (keeps session and config intact) | `{ "id?: string }` (id optional, defaults to active) | `{ "success": bool, "chat_id": string, "messages": string }` |

### Search

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/search` | Text search in workspace files | Query: `?q=<pattern>&path=<dir>&glob=<pattern>&limit=N&caseSensitive=bool&regex=bool` | `{ "matches": [...], "count": number, "query": string }` |
| `POST` | `/api/search/replace` | Find and replace in workspace | `{ "search": string, "replace": string, "files": string[], ... }` | `{ "replacements": number, "files": number, "errors": [...] }` |
| `GET` | `/api/search/semantic` | Execute semantic search (method-based dispatch: GET = search, POST = status) | Query: `?query=<text>&top_k=N&threshold=T` | `{ "results": [...], "total": number, "query": string, "duration": string }` |
| `GET` | `/api/search/semantic/status` | Get semantic search index status | — | `{ "available": bool, "initialized": bool, "building": bool, "record_count": number }` |
| `POST` | `/api/search/semantic/build` | Build/rebuild semantic index | `{ "force?: bool }` | `{ "success": bool, "message": string }` |
| `GET` | `/api/search/semantic/preview` | Preview semantic search results | Query: `?query=<text>` | `{ "results": [...], "count": number }` |

**Note:** `/api/search/semantic` uses **method-based dispatch**: `GET` executes semantic search, while `POST` is handled by a different handler for status. See `handleAPISemanticSearch()` and `handleAPISemanticStatus()` in `search_semantic_api.go`.

### Terminal

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/api/terminal/history` | Get terminal command history | Query: `?session_id=<id>` | `{ "history": string[], "session_id": string, "count": number }` |
| `POST` | `/api/terminal/history` | Add command to history | `{ "session_id": string, "command": string }` | `{ "stored": bool, "command": string }` |
| `GET` | `/api/terminal/sessions` | List terminal PTY sessions | — | `{ "sessions": [...], "count": number, "active_count": number }` |
| `GET` | `/api/terminal/shells` | List available shells on system | — | `{ "shells": [...] }` |
| `GET` | `/api/terminal/agent-sessions` | List agent terminal sessions | — | `{ "sessions": [...] }` |

### Diagnostics

| Method | Path | Description | Request Body | Response Fields |
|--------|------|-------------|--------------|-----------------|
| `GET` | `/health` | Health check endpoint | — | `OK` (text/plain) |
| `GET` | `/api/lsp/status` | List available LSP server info | — | `{ "servers": [{ "id", "languages", "binary", "available", "binaryPath" }], "active": number, "workspace": string }` |
| `POST` | `/api/upload/image` | Upload an image file | Multipart form (`image` field) or raw binary | `{ "path": string, "filename": string }` |
| `POST` | `/api/confirm` | Respond to a security prompt/approval | `{ "request_id": string, "response": bool }` | `{ "success": bool, "message": string }` |

**Details:**
- `POST /api/upload/image`: Accepts multipart or raw binary. Detects format via magic bytes. Max size: `MaxPastedImageSize`.
- `POST /api/confirm` (handler: `handleAPIConfirm` in `security_api.go`): Responds to security approval/prompt requests. Publishes response event back to the agent. Returns `404` if request ID is unknown.

---

## 3. WebSocket Endpoints

### `/ws` — Main Chat/Command WebSocket

**Primary bidirectional channel** for agent interaction. Supports reattach for disconnection recovery.

**Connection URL:**
```
ws[s]://<host>/ws?client_id=<uuid>[&chat_id=<chat-uuid>][&reattach=<chat-uuid>&after_seq=<n>]
```

**Query Parameters:**
- `client_id` — Required. Unique browser tab identifier (UUID).
- `chat_id` — Optional. Attach to a specific chat session.
- `reattach` — For reconnection. Replays buffered events from the specified chat.
- `after_seq` — Sequence number after which to replay events.

**Handler:** `handleWebSocket()` → `attachWS()` in `websocket_handler.go`

### `/terminal` — Terminal WebSocket

**Bidirectional PTY terminal** for interactive shell sessions.

**Connection URL:**
```
ws[s]://<host>/terminal?client_id=<uuid>[&reattach=<session-id>][&shell=<shell-name>]
```

**Query Parameters:**
- `client_id` — Required. Browser tab identifier.
- `reattach` — Reconnect to an existing PTY session.
- `shell` — Preferred shell for new sessions.

**Handler:** `handleTerminal()` → `attachTerminal()` in `terminal_websocket.go`

### `/api/lsp/ws` — LSP Proxy Bridge

**WebSocket proxy** bridging LSP client (editor) to LSP server.

---

## 4. WebSocket Inbound Messages (Client → Server)

All inbound WebSocket messages are JSON objects with a `type` field.

| Type | Purpose | Payload Fields |
|------|---------|----------------|
| `query` | Submit a query to the agent | `{ "query": string, "image_path?: string, "image_data?: string, "persona?: string, "model?: string, "context?: string }` |
| `steer` | Influence a running query | `{ "steer": string }` |
| `stop` | Stop the active query | `{ }` |
| `command` | Execute a direct command | `{ "command": string }` |
| `ping` | Keepalive heartbeat | `{ }` |
| `chat_session_switch` | Switch active chat session | `{ "chat_id": string }` |
| `chat_session_rename` | Rename active chat session | `{ "title": string }` |
| `chat_session_create` | Create a new chat session | `{ "title?: string }` |
| `chat_session_delete` | Delete a chat session | `{ "chat_id": string }` |
| `chat_session_pin` | Pin a chat session | `{ "chat_id": string }` |
| `chat_session_unpin` | Unpin a chat session | `{ "chat_id": string }` |
| `workspace_set` | Change workspace root | `{ "path": string }` |
| `session_keepalive` | Client keepalive | `{ }` |
| `terminal_output` | Send terminal input | `{ "session_id": string, "input": string }` |
| `resize` | Terminal resize | `{ "cols": number, "rows": number }` |
| `provider_change` | Change the LLM provider for the current chat session | `{ "provider": string }` |

---

## 5. WebSocket Outbound Messages (Server → Client)

### Event Structure

```typescript
interface WsEvent {
  type: string;        // Event type string
  __seq?: number;      // Per-chat sequence number (for reattach)
  data: Record<string, any>; // Event-specific payload
}
```

### Event Types (from `pkg/events/events.go`)

| Event Type | Purpose | Payload Fields |
|------------|---------|----------------|
| `query_started` | Agent started processing a query | `{ "query_id": string, "query": string, "conversation_id": string }` |
| `query_progress` | Progress update during query | `{ "query_id": string, "message": string, "conversation_id": string }` |
| `query_completed` | Agent finished processing | `{ "query_id": string, "result": string, "conversation_id": string }` |
| `stream_chunk` | Partial output chunk (streaming) | `{ "text": string, "query_id": string }` |
| `error` | Error occurred | `{ "message": string, "code?: string }` |
| `tool_start` | Agent started a tool call | `{ "tool": string, "args": {...}, "tool_call_id": string }` |
| `tool_end` | Agent finished a tool call | `{ "tool": string, "result": string, "tool_call_id": string }` |
| `tool_execution` | Tool execution event (tool output/result) | `{ "tool_call_id": string, "tool_name": string, "result": string }` |
| `subagent_activity` | Subagent started/stopped | `{ "action": string, "description": string }` |
| `agent_message` | Agent produced a message | `{ "message": string, "query_id": string }` |
| `todo_update` | Todo list changed | `{ "todos": [{ "content": string, "status": string }] }` |
| `file_changed` | File was modified/created/deleted | `{ "path": string, "action": string }` |
| `file_content_changed` | File content changed | `{ "path": string, "content?: string }` |
| `metrics_update` | Query metrics updated | `{ "tokens": number, "cost": number, "duration_ms": number }` |
| `validation` | Validation event (config/syntax checks) | `{ "valid": bool, "messages": string[] }` |
| `workspace_changed` | Workspace root changed | `{ "workspace_root": string, "daemon_root": string, "previous_workspace_root": string }` |
| `security_approval_request` | Agent needs approval | `{ "request_id": string, "operation": string, "details": string }` |
| `security_prompt_request` | Agent needs user response | `{ "request_id": string, "prompt": string }` |
| `ask_user_request` | Agent asks a question | `{ "request_id": string, "question": string }` |
| `session_terminated` | Session termination signal | `{ "session_id": string }` |
| `session_changed` | Chat session metadata changed | `{ "session": {...} }` |
| `drift_detected` | File drift detected | `{ "path": string, "content?: string }` |
| `connection_status` | Connection state (legacy) | `{ "connected": bool }` |
| `connection_state` | Connection state change | `{ "connected": bool, "state": string }` |
| `stats_update` | Periodic stats broadcast | `{ "stats": {...} }` |
| `ping` | Server ping | `{ }` |
| `pong` | Response to client ping | `{ }` |
| `session_restored` | Terminal reattach signal (session restored) | `{ "session_id": string, "state": string }` |
| `chat_run_restored` | Chat run recovery signal (reattach complete) | `{ "chat_id": string }` |
| `chat_session_created` | New chat session created | `{ "session": {...} }` |
| `chat_session_switched` | Active chat changed | `{ "chat_id": string }` |

### Sequence Numbers (`__seq`)

Every event associated with a chat session includes a `__seq` field — a monotonically increasing counter per chat. Stored per-chat in `chatRunRingBuffer.nextSeq` (see `pkg/webui/chat_run_buffer.go`).

### Reattach-Buffered Event Types

The following **8 event types** are buffered (up to 5000 events per chat) for reattach replay (defined in `reattachBufferedEventTypes` in `api_query.go`):

`query_started`, `query_progress`, `query_completed`, `stream_chunk`, `tool_start`, `tool_end`, `agent_message`, `error`

**Non-buffered types** (not replayed during reattach): `subagent_activity`, `todo_update`, `file_changed`, `file_content_changed`, `metrics_update`, `security_approval_request`, `security_prompt_request`, `ask_user_request`, `workspace_changed`, `terminal_session_ready`, `terminal_output`, `terminal_pty_exit`, `drift_detected`, `connection_status`, `pong`, `chat_session_created`, `chat_session_switched`, `validation`, `session_terminated`, `session_changed`, `connection_state`, `stats_update`, `session_restored`, `chat_run_restored`, `tool_execution`.

---

## 6. Reattach Flow

The reattach mechanism allows the frontend to recover missed events after a WebSocket disconnection.

### Protocol Steps

```
┌──────────┐                          ┌──────────┐
│ Frontend  │                          │  Backend  │
│           │                          │           │
│ 1. Track  │                          │ 1. Assign │
│    __seq  │                          │    __seq  │
│    per     │                          │    to     │
│    chat    │                          │    events  │
│            │                          │           │
│ 2. On disc,│                          │           │
│    reconnect│                         │           │
│    with:    │                         │           │
│    ?reattach= │                      │           │
│     <chat-id>&│                      │           │
│     after_seq=│                      │           │
│     <last-seq>│                      │           │
│            │──── WS connect ────────>│           │
│            │                          │ 3. Look up │
│            │                          │    buffer  │
│            │                          │ 4. Replay  │
│            │<── Events after seq ────│    missed  │
│            │                          │    events  │
│ 5. Resume  │                          │ 5. Send   │
│    normal   │                          │    live   │
│    events   │                          │    events │
└──────────┘                          └──────────┘
```

**1. Client tracks `__seq`:**
```typescript
// webui/src/services/websocket.ts
private lastSeq = 0;
private handleMessage(event: MessageEvent) {
  const data = JSON.parse(event.data);
  if (data.data?.__seq != null) {
    this.lastSeq = data.data.__seq;
  }
}
```

**2. On disconnection, client reattaches:**
```typescript
// webui/src/services/websocket.ts
private reconnectWithReattach() {
  const url = new URL(this.baseURL);
  url.searchParams.set('client_id', this.clientId);
  url.searchParams.set('reattach', this.activeChatId);
  url.searchParams.set('after_seq', String(this.lastSeq));
  this.ws = new WebSocket(url.toString());
}
```

**3-4. Server replays buffered events** via `ChatRunBuffer.EventsAfterSeq(seq)` — returns all events with `__seq > afterSeq` in order.

**5. Normal live events resume** after replay is complete.

### Key Constraints
- **Buffer size**: Maximum 5000 events per chat (`defaultRunBufferMaxEvents`)
- **Single chat**: Reattach works per chat session
- **Seq is per-chat**: Independent sequence numbering per chat

---

## 7. Error Envelope

### Structured Error Responses

All REST API structured errors follow the format in `pkg/webui/errors.go`:

```json
{
  "code": "string",           // Machine-readable identifier
  "message": "string",        // Human-readable description
  "details": {},              // Optional structured data
  "retryable": true           // Whether client should auto-retry
}
```

### Common Error Codes

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `query_in_progress` | 409 | Cannot change workspace while query is active |
| `provider_unavailable` | 503 | LLM provider not responding |
| `rate_limited` | 429 | Rate limit exceeded (retryable) |
| `config_conflict` | 400 | Configuration conflict |

### Legacy Plain-Text Errors

Some handlers still use `http.Error()` producing plain text:
```
Invalid JSON          → 400
Method not allowed    → 405
Unauthorized          → 401
Not found             → 404
```

### WebSocket Error Events

```json
{
  "type": "error",
  "data": { "message": "Description", "code": "optional_code" }
}
```

### HTTP Status Code Convention

| Status | Usage |
|--------|-------|
| 200 | Success |
| 400 | Bad request (invalid JSON, missing fields) |
| 401 | Unauthorized |
| 403 | Forbidden (path outside workspace) |
| 404 | Not found |
| 405 | Method not allowed |
| 409 | Conflict (e.g., query in progress) |
| 429 | Rate limited |
| 500 | Internal server error |
| 503 | Service unavailable (agent not ready) |

---

## 8. Type Generation Workflow

### Current State

**No automatic code generation** is used. Types are **hand-authored** in TypeScript based on Go source types.

1. **Go types** (e.g., `pkg/events/events.go`, `pkg/webui/chat_sessions.go`) carry `// @ts-generated` marker comments pointing to their TypeScript counterparts.
2. **TypeScript types** live in `packages/events/src/types.ts` and `webui/src/types/generated.ts`.
3. **`@sprout/events`** (`packages/events/`) is a shared TypeScript package linked via `file:../packages/events` in the web UI's `package.json`.
4. **Verification** via `make generate-ts-types`: scans Go files for `@ts-generated` markers and confirms `webui/src/types/generated.ts` exists. **Verification only** — does not generate.

### Marker Format

```go
// pkg/events/events.go
// @ts-generated  webui/src/types/generated.ts::UIEvent
type UIEvent struct {
    Type  string                 `json:"type"`
    Seq   int64                  `json:"__seq,omitempty"`
    Data  map[string]interface{} `json:"data"`
}
```

### Future Plans

Per `TODO.md` (SP-034-5a/5b): When `tygo` or an equivalent generator is adopted, `make generate-ts-types` will produce TypeScript types automatically from Go source annotations.

---

## Appendix A: File Locations

| Component | Source Path |
|-----------|-------------|
| Route registration | `pkg/webui/routes.go` |
| Main WebSocket handler | `pkg/webui/websocket_handler.go` |
| WebSocket message handlers | `pkg/webui/websocket_message_handlers.go` |
| WebSocket message types | `pkg/webui/websocket_message_types.go` |
| Outbound event registry | `pkg/webui/websocket_outbound_registry.go` |
| Chat run buffer | `pkg/webui/chat_run_buffer.go` |
| Chat run replay | `pkg/webui/chat_run_replay.go` |
| Terminal WebSocket | `pkg/webui/terminal_websocket.go` |
| Error types | `pkg/webui/errors.go` |
| Query API | `pkg/webui/api_query.go` |
| Workspace API | `pkg/webui/api_workspace.go` |
| Files API | `pkg/webui/api_files.go` |
| File check-modified | `pkg/webui/api_file_check_modified.go` |
| Search API | `pkg/webui/search_api.go` |
| Semantic search API | `pkg/webui/search_semantic_api.go` |
| Chat sessions API | `pkg/webui/chat_sessions_api.go` |
| Sessions API | `pkg/webui/sessions_api.go` |
| Providers API | `pkg/webui/providers_api.go` |
| Cost tracking API | `pkg/webui/cost_tracking_api.go` |
| Embedding index API | `pkg/webui/api_embedding_index.go` |
| Security API | `pkg/webui/security_api.go` |
| Misc API | `pkg/webui/api_misc.go` |
| Event types (Go) | `pkg/events/events.go` |
| Event types (TS) | `packages/events/src/types.ts` |
| WebSocket service (TS) | `webui/src/services/websocket.ts` |
| Client session (TS) | `webui/src/services/clientSession.ts` |
| Generated types (TS) | `webui/src/types/generated.ts` |

## Appendix B: Client ID Management

Every browser tab gets a unique `client_id` persisted in `localStorage` (`sprout.webui.clientId`):

```typescript
// webui/src/services/clientSession.ts
export function getWebUIClientId(): string {
  const key = 'sprout.webui.clientId';
  let id = localStorage.getItem(key);
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem(key, id);
  }
  return id;
}
```

The server uses `client_id` to isolate agent instances per tab, route WebSocket events, manage workspace roots, and track queries/sessions. When absent, defaults to `"web"`.

## Appendix C: Ping/Pong Heartbeat

**Main WebSocket (`/ws`):** Server sends `ping` events; client responds with `pong` message. No pong within timeout → connection considered dead.

**Terminal WebSocket (`/terminal`):** Client sends `ping` every 30s; server responds with `pong`. Watchdog checks every 30s if last pong > 90s old → forces reconnect.
