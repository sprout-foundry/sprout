/**
 * Events transport types for Sprout.
 *
 * Shared between webui and @sprout/ui. Canonical source —
 * do not duplicate; consume via `@sprout/events`.
 */

/**
 * A single event from the transport layer.
 * Compatible with the WsEvent shape used by the webui WebSocketService.
 */
export interface SproutEvent {
  type: string;
  data?: unknown;
  [key: string]: unknown;
}

/** Callback invoked for each incoming event */
export type SproutEventCallback = (event: SproutEvent) => void;

// ── Event Data Types ────────────────────────────────────────────────

export interface ConnectionStatusData {
  connected: boolean;
  session_id?: string;
  client_id?: string;
  reconnected?: boolean;
  reconnecting?: boolean;
  restored?: boolean;
  message_count?: number;
  queuedMessageCount?: number;
  /** Session ID for reattachment after disconnect. */
  reattach?: string | null;
}

export interface QueryStartedData {
  query: string;
  provider?: string;
  model?: string;
  chat_id?: string;
}

export interface QueryProgressData {
  message?: string;
  iteration?: number;
  tokens_used?: number;
  chat_id?: string;
}

export interface QueryCompletedData {
  query: string;
  response?: string;
  tokens_used?: number;
  cost?: number;
  duration_ms?: number;
  chat_id?: string;
}

export interface StreamChunkData {
  chunk: string;
  content_type?: string;
  chat_id?: string;
}

export interface ErrorData {
  message: string;
  error?: string;
  code?: string;
  chat_id?: string;
}

export interface ToolStartData {
  tool_name: string;
  tool_call_id?: string;
  arguments?: string;
  display_name?: string;
  persona?: string;
  is_subagent?: boolean;
  subagent_type?: string;
  tool_index?: number;
  chat_id?: string;
}

export interface ToolEndData {
  tool_call_id?: string;
  tool_name?: string;
  status?: string;
  result?: string;
  error?: string;
  duration_ms?: number;
  result_truncated?: boolean;
  result_length?: number;
  /** display_name/arguments may appear in legacy or enriched payloads but are not sent by Go ToolEndEvent. */
  display_name?: string;
  arguments?: string;
  chat_id?: string;
}

export interface SubagentActivityData {
  tool_call_id?: string;
  tool_name?: string;
  phase?: string;
  message?: string;
  task_id?: string;
  persona?: string;
  is_parallel?: boolean;
  provider?: string;
  model?: string;
  task_count?: number;
  failures?: number;
  status?: string;
  chat_id?: string;
}

export interface AgentMessageData {
  category: string;
  message: string;
  action?: string;
  target?: string;
  chat_id?: string;
}

export interface TodoUpdateData {
  todos: unknown;
  chat_id?: string;
}

export interface FileChangedData {
  /** file_path is the canonical field sent by Go. path is a legacy alias. */
  file_path?: string;
  path?: string;
  action?: string;
  operation?: string;
  content?: string;
  lines_added?: number;
  lines_deleted?: number;
  chat_id?: string;
}

export interface FileContentChangedData {
  file_path: string;
  mod_time?: number;
  size?: number;
}

export interface MetricsUpdateData {
  total_tokens?: number;
  context_tokens?: number;
  max_context_tokens?: number;
  iteration?: number;
  total_cost?: number;
  provider?: string;
  model?: string;
  persona?: string;
  chat_id?: string;
}

export interface WorkspaceChangedData {
  daemon_root?: string;
  workspace_root?: string;
  previous_workspace_root?: string;
  client_id?: string;
  source?: string;
}

export interface SecurityApprovalRequestData {
  request_id: string;
  tool_name: string;
  risk_level: string;
  reasoning: string;
  command?: string;
  risk_type?: string;
  target?: string;
  status?: string;
}

export interface SecurityPromptRequestData {
  request_id: string;
  prompt: string;
  file_path?: string;
  concern?: string;
  status?: string;
  default_response?: boolean;
}

export interface AskUserRequestData {
  request_id: string;
  question: string;
  client_id?: string;
  status?: string;
}

// ── Terminal Session Data Types ─────────────────────────────────────

export interface TerminalSessionReadyData {
  session_id?: string;
  pseudo_command?: string;
}

export interface TerminalOutputData {
  chunk?: string;
}

export interface TerminalPtyExitData {
  exit_code?: number;
  reason?: string;
}

export interface DriftDetectedData {
  similarity: number;
  threshold: number;
  sessionId?: string;
  timestamp?: string;
  options?: string[];
}

// ── Discriminated Union ────────────────────────────────────────────────

export type WsEvent =
  | { type: 'connection_status'; data?: ConnectionStatusData; id?: string; timestamp?: string }
  | { type: 'query_started'; data?: QueryStartedData; id?: string; timestamp?: string }
  | { type: 'query_progress'; data?: QueryProgressData; id?: string; timestamp?: string }
  | { type: 'query_completed'; data?: QueryCompletedData; id?: string; timestamp?: string }
  | { type: 'stream_chunk'; data?: StreamChunkData; id?: string; timestamp?: string }
  | { type: 'error'; data?: ErrorData; id?: string; timestamp?: string }
  | { type: 'tool_start'; data?: ToolStartData; id?: string; timestamp?: string }
  | { type: 'tool_end'; data?: ToolEndData; id?: string; timestamp?: string }
  | { type: 'tool_execution'; data?: Record<string, unknown>; id?: string; timestamp?: string }
  | { type: 'subagent_activity'; data?: SubagentActivityData; id?: string; timestamp?: string }
  | { type: 'agent_message'; data?: AgentMessageData; id?: string; timestamp?: string }
  | { type: 'todo_update'; data?: TodoUpdateData; id?: string; timestamp?: string }
  | { type: 'file_changed'; data?: FileChangedData; id?: string; timestamp?: string }
  | { type: 'file_content_changed'; data?: FileContentChangedData; id?: string; timestamp?: string }
  | { type: 'metrics_update'; data?: MetricsUpdateData; id?: string; timestamp?: string }
  | { type: 'workspace_changed'; data?: WorkspaceChangedData; id?: string; timestamp?: string }
  | { type: 'security_approval_request'; data?: SecurityApprovalRequestData; id?: string; timestamp?: string }
  | { type: 'security_prompt_request'; data?: SecurityPromptRequestData; id?: string; timestamp?: string }
  | { type: 'ask_user_request'; data?: AskUserRequestData; id?: string; timestamp?: string }
  | { type: 'validation'; data?: Record<string, unknown>; id?: string; timestamp?: string }
  | { type: 'terminal_output'; data?: Record<string, unknown>; id?: string; timestamp?: string }
  | { type: 'session_terminated'; data?: Record<string, unknown>; id?: string; timestamp?: string }
  | { type: 'session_ready'; data?: TerminalSessionReadyData; id?: string; timestamp?: string }
  | { type: 'session_restored'; data?: TerminalSessionReadyData; id?: string; timestamp?: string }
  | { type: 'output'; data?: TerminalOutputData; id?: string; timestamp?: string }
  | { type: 'error_output'; data?: TerminalOutputData; id?: string; timestamp?: string }
  | { type: 'pty_exit'; data?: TerminalPtyExitData; id?: string; timestamp?: string }
  | { type: 'drift_detected'; data?: DriftDetectedData; id?: string; timestamp?: string }
  // Catch-all: must be last. Provides SproutEvent compatibility and handles
  // dev-server noise (liveReload, hot, ping, etc.). Note: this prevents
  // automatic narrowing in switch/case — use typed casts in handlers instead.
  | { type: string; data?: unknown; id?: string; timestamp?: string; [key: string]: unknown };

/**
 * EventsProvider — abstraction over the real-time event transport.
 *
 * In local mode this wraps a WebSocket connection to the Go backend.
 * In cloud mode this could wrap Server-Sent Events, a cloud WebSocket,
 * or any other streaming transport.
 *
 * Components consume this via the `useEvents()` hook from EventsContext.
 */
export interface EventsProvider {
  /** Establish the underlying connection. Idempotent if already connected. */
  connect(): void;

  /** Gracefully tear down the connection and clear any outbound queue. */
  disconnect(): void;

  /** Register a callback for incoming events. No-op if already registered. */
  onEvent(callback: SproutEventCallback): void;

  /** Remove a previously registered callback. */
  removeEvent(callback: SproutEventCallback): void;

  /** Send an outbound event to the server. Implementations may queue if disconnected. */
  sendEvent(event: SproutEvent): void;

  /** Whether the underlying transport is currently open. */
  isConnected(): boolean;

  /** Register a one-shot callback that fires on the next successful reconnect (not initial connect). Pass null to unregister. */
  onReconnect(callback: (() => void) | null): void;

  /** Proactively disconnect before tab freeze. Should preserve outbound message queue for replay after resume(). */
  freeze(): void;

  /** Resume after tab freeze/unfreeze. Should trigger immediate reconnection. */
  resume(): void;

  /** Force a clean reconnection, resetting backoff state. */
  resetAndReconnect(): void;

  /** Number of outbound messages currently queued awaiting connection. */
  getQueuedMessageCount(): number;

  /** Manually flush all queued messages if connected. Returns count flushed, or 0 if not connected. */
  flushQueuedMessages(): number;
}
