/**
 * Shared wire-format types between the Go backend and the TS frontend.
 *
 * **Status: hand-maintained today.** SP-034-5a (Go→TS code generation
 * via tygo or equivalent) is deferred as a separate tooling task. Until
 * that lands, this file is the single source of truth on the TS side —
 * the Go side has matching definitions in the files cross-referenced
 * below. The companion Makefile target `make generate-ts-types` is a
 * placeholder that will run the generator when it's wired up.
 *
 * When you change a shared type:
 * 1. Update the Go definition (see cross-references on each interface)
 * 2. Mirror the change here
 * 3. Run `make generate-ts-types` (no-op today; verification only)
 *
 * The Go source files carry `// @ts-generated` markers near the types
 * that should round-trip through this file. When the real generator
 * lands, it'll pick those up automatically.
 *
 * Cross-reference (Go → TS):
 *   pkg/webui/chat_sessions.go:chatSession         → ChatSession
 *   pkg/events/events.go:UIEvent                    → UIEvent
 *   pkg/events/events.go:EventType*                 → ServerEventType
 *   pkg/webui/api_query.go:publishSessionChanged    → SessionChangedData
 *   pkg/webui/chat_run_replay.go:wsMessageType*     → ChatRunRestoredData
 *   pkg/configuration/errors.go:ConfigConflictError → ConfigConflictData
 *
 * SP-034-5b: the listed Go types carry the `// @ts-generated` marker
 * comment so the eventual generator can find them. SP-034-5c: this
 * file is the canonical TS import target — services/chatSessions.ts
 * re-exports `ChatSession` from here.
 */

/**
 * Wire-format chat session fields persisted server-side. Computed-only
 * UI fields (`is_default`, `is_active`) are NOT in this canonical
 * shape — they're added by the frontend at fetch time in the wrapper
 * type defined in services/chatSessions.ts.
 *
 * Go: pkg/webui/chat_sessions.go::chatSession
 */
export interface ChatSession {
  id: string;
  name: string;
  created_at: string;
  last_active_at: string;
  message_count: number;
  current_session_id: string;
  active_query: boolean;
  current_query?: string;
  is_pinned: boolean;
  provider?: string;
  model?: string;
  worktree_path?: string;
}

/**
 * Canonical server event-type strings. Mirrors the
 * `events.EventType*` Go constants — keep in sync with the registry
 * in pkg/webui/websocket_outbound_registry.go (which is also covered
 * by a smoke test that fails if a Go EventType is missing from it).
 *
 * Go: pkg/events/events.go (`EventType*` constants)
 */
export type ServerEventType =
  | 'query_started'
  | 'query_progress'
  | 'query_completed'
  | 'error'
  | 'tool_execution'
  | 'tool_start'
  | 'tool_end'
  | 'subagent_activity'
  | 'todo_update'
  | 'file_changed'
  | 'file_content_changed'
  | 'stream_chunk'
  | 'metrics_update'
  | 'validation'
  | 'security_approval_request'
  | 'security_prompt_request'
  | 'ask_user_request'
  | 'agent_message'
  | 'workspace_changed'
  | 'session_terminated'
  | 'drift_detected'
  | 'session_changed'
  | 'delegate_activity';

/**
 * The envelope every event flows through. `data` shape varies per
 * `type` — narrow with the per-event-type interfaces below.
 *
 * Go: pkg/events/events.go::UIEvent
 */
export interface UIEvent {
  id: string;
  type: ServerEventType | string;
  timestamp: string;
  data: unknown;
}

/**
 * Data payload for `session_changed` events emitted on chat
 * rename/pin/unpin/switch. `change` carries the mutation kind so the
 * frontend can react contextually (flash the tab title on rename, etc.).
 *
 * Go: pkg/webui/api_query.go::publishSessionChanged
 */
export interface SessionChangedData {
  client_id?: string;
  chat_id: string;
  user_id?: string;
  change: 'rename' | 'pin' | 'unpin' | 'switch' | string;
  summary: Partial<ChatSession>;
}

/**
 * Control message sent at the start of a WebSocket reattach replay.
 * `gap=true` means the buffer evicted events the client expected to
 * see — local state is stale, hard-refresh required.
 *
 * Go: pkg/webui/chat_run_replay.go::buildChatRunReplayMessages
 */
export interface ChatRunRestoredData {
  chat_id: string;
  after_seq: number;
  last_seq: number;
  missed_chunks_count: number;
  gap: boolean;
}

/**
 * Error data payload for `code === 'config_conflict'`. Surfaced when
 * the server detects the on-disk config has been modified since the
 * in-memory copy was loaded.
 *
 * Go: pkg/webui/config_conflict_envelope.go::configConflictEnvelope
 */
export interface ConfigConflictData {
  code: 'config_conflict';
  message: string;
  path: string;
  current_summary: {
    provider?: string;
    model?: string;
  };
}
