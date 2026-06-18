# Foundry Chat API Contract

**Status:** Canonical (SP-015-R7)
**Date:** 2026-06-18

## Overview

This document defines the contract between the Sprout WebUI and the Foundry
backend for chat/conversation API calls. Both the `CloudAdapter`
(`webui/src/services/cloudProxyRoutes.ts`) and the now-removed Service Worker's
`chat-bridge.ts` implement this contract.

## Endpoints

### `POST /api/query` → `POST /api/proxy/chat`

The primary chat endpoint. The webui sends a simple query; Foundry receives
a Foundry-compatible chat request.

### `POST /api/query/steer` → `POST /api/proxy/chat`

Steer an in-progress chat (inject a follow-up without restarting).

### `POST /api/query/stop` → `POST /api/proxy/chat/stop`

Stop an in-progress chat stream.

### `GET /api/query/status` → `GET /api/proxy/chat/status`

Check the status of a chat session.

## Request Body Translation

### Webui → Foundry Body Mapping

| Webui Field | Foundry Field | Required | Description |
|---|---|---|---|
| `query` | `messages[0].content` | Yes | The user's message text |
| _(constructed)_ | `messages[0].role` | Yes | Always `"user"` |
| _(constructed)_ | `stream` | Yes | Always `true` (SSE streaming) |
| `chat_id` | `chat_id` | No | Session identifier for continuity |
| `provider` | `provider` | No | LLM provider override (e.g., `"openai"`) |
| `model` | `model` | No | Model override (e.g., `"gpt-4"`) |
| `workspace_root` | `workspace_root` | No | Project root for file context |
| `system_prompt` | `system_prompt` | No | Custom system prompt |
| _(from URL path)_ | `steer` | No | Set to `true` when path is `/api/query/steer` |

### Translation Rules

1. **`query` is always wrapped in a single-message array:**
   ```json
   { "query": "hello" }
   ```
   becomes:
   ```json
   { "messages": [{ "role": "user", "content": "hello" }], "stream": true }
   ```

2. **Empty or missing `query`** is passed through as `content: ""`. The Foundry
   backend is responsible for validating/rejecting empty queries.

3. **`chat_id` is omitted** when absent, null, or empty string (falsy check).

4. **`steer` is set to `true`** only when the request path is `/api/query/steer`.
   It is omitted (not `false`) for regular chat requests.

5. **Existing `messages` field is overwritten** with the translated single-message
   array. A console warning is emitted.

6. **`stream` is always `true`** — the webui expects SSE streaming responses.

### Stop Endpoint (No Translation)

The `POST /api/query/stop` endpoint does NOT translate the body. The request
body `{ "chat_id": "..." }` is passed through unchanged.

### Status Endpoint (GET)

The `GET /api/query/status` endpoint takes no body. It returns the current
chat status (idle, streaming, etc.).

## Response Format

### Chat (SSE Stream)

Foundry returns a `text/event-stream` response with server-sent events:

```
data: {"type":"token","content":"Hello"}

data: {"type":"token","content":" world"}

data: {"type":"done","content":""}
```

Event types:
- `token` — A streaming token/chunk
- `done` — Stream complete
- `error` — Error occurred (with error message)

### Stop

Returns `{ "success": true }` on success.

### Status

Returns `{ "status": "idle" | "streaming" | "error", "chat_id": "..." }`

## Authentication

All requests include:
- `credentials: 'include'` — sends session cookies (Kratos)
- `X-Sprout-Client-ID` header — for session correlation
- `Content-Type: application/json`

## Edge Cases

| Case | Behavior |
|---|---|
| Empty `query` (`""`) | Passed through as `content: ""`; backend validates |
| Missing `query` field | Treated as empty string |
| Null `query` | Treated as empty string |
| Non-string `query` | Treated as empty string |
| Missing `chat_id` | Omitted from translated body |
| Invalid JSON body | Passed through as-is (no translation) |
| Existing `messages` field | Overwritten with translated array (console warning) |

## Test Coverage

Edge-case tests are in `webui/src/services/chatTranslation.test.ts` (19 tests)
and `webui/src/services/cloudAdapter.test.ts` (integration-level tests for
steer, stop, chat_id, empty query, provider/model passthrough).
