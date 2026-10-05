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
  /** User-facing bubble text. Falls back to query when absent. */
  display?: string;
  /** Query source (e.g. 'auto-resume') — wakeup turns render differently. */
  source?: string;
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
  /** "interrupted" when the user stopped the run before it finished. */
  status?: string;
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
  /** True when arguments exceeded the server's event-size cap and were truncated. */
  arguments_truncated?: boolean;
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
  /** Lifecycle status for subagent events: "queued", "started", "completed", "cancelled" */
  status?: string;
  chat_id?: string;
  /** Reason for cancellation (e.g. "budget exceeded") */
  reason?: string;
  /** Tokens consumed by this subagent task */
  tokens_used?: number;
  /** Duration in milliseconds */
  elapsed_ms?: number;
}

export interface AgentMessageData {
  category: string;
  message: string;
  action?: string;
  target?: string;
  chat_id?: string;
}

export interface DelegateClarificationRequestedData {
  subagent_id: string;
  request_id: string;
  question: string;
  timestamp: string;
  chat_id?: string;
}

export interface DelegateClarificationRespondedData {
  subagent_id: string;
  request_id: string;
  response: string;
  timestamp: string;
  chat_id?: string;
}

export interface SessionChangedData {
  change: string;
  summary: Record<string, unknown>;
  chat_id?: string;
}

/** Payload for rate_limited — the approval broker's backoff notice. */
export interface RateLimitedData {
  provider: string;
  attempt: number;
  max_attempts: number;
  retry_after_ms: number;
  message: string;
  session_id?: string;
}

/** Payload for compact_started (seed structural compaction / manual /compact). */
export interface CompactStartedData {
  source: string;
  message_count: number;
  checkpoint_count: number;
  timestamp: string;
  chat_id?: string;
}

/** Payload for compact_completed. success=false carries the failure reason. */
export interface CompactCompletedData {
  source: string;
  before_message_count: number;
  after_message_count: number;
  summary_chars: number;
  success: boolean;
  error?: string;
  timestamp: string;
  chat_id?: string;
}

export interface ProviderNoCredentialData {
  provider: string;
  message: string;
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
  /** @deprecated No longer transmitted — the editor refetches file bytes on
   *  demand. Use `size` for the byte count. */
  content?: string;
  /** Byte length of the changed content (whole-file content is not sent). */
  size?: number;
  lines_added?: number;
  lines_deleted?: number;
  chat_id?: string;
}

export interface FileContentChangedData {
  file_path: string;
  mod_time?: number;
  size?: number;
}

/** File-change notification payload from WorkspacePatchEvent (no content — see WorkspacePatchEvent). */
export interface WorkspacePatchData {
  file_path: string;
  /**
   * @deprecated No longer transmitted — the event never carried content the
   * UI rendered, and shipping whole files made every tab JSON.parse them.
   * Use `size` for the byte count; fetch content via /api/files.
   */
  content?: string;
  /** Byte length of the changed content (whole-file content is not sent). */
  size: number;
  action: string;
  seq: number;
  conflict?: boolean;
  theirs_path?: string;
  chat_id?: string;
}

/** Streamed output from a safe slash command. */
export interface CommandOutputData {
  command: string;
  chunk: string;
  is_final: boolean;
  seq: number;
  chat_id?: string;
}

/** Backpressure warning emitted when streamed command output is discarded. */
export interface CommandOutputDroppedData {
  command: string;
  dropped_bytes: number;
  chat_id?: string;
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

/** Per-iteration context-management diagnostic emitted by the backend agent
 *  loop (see pkg/events.ContextManagementDiagnosticEvent). The fields mirror
 *  the Go payload 1:1 — `cached_tokens`, `prompt_tokens`, and
 *  `cache_write_tokens` are cumulative session counters (not per-iteration),
 *  and `cache_hit_rate` is the backend-computed `cached/prompt` ratio. The
 *  frontend treats this as telemetry: the typed payload lets the dedicated
 *  handler in useWebSocketEventHandler render a compact summary instead of
 *  letting the event fall through to the generic "unknown event" branch and
 *  show up as raw JSON in the Logs pane. */
export interface ContextManagementDiagnosticData {
  current_tokens?: number;
  max_tokens?: number;
  native_max_tokens?: number;
  effective_max?: number;
  trigger_fraction?: number;
  reserved_response?: number;
  reserved_thinking?: number;
  reserved_tool_io?: number;
  iteration?: number;
  message_count?: number;
  cached_tokens?: number;
  prompt_tokens?: number;
  cache_write_tokens?: number;
  cache_hit_rate?: number;
  chat_id?: string;
}

/**
 * Payload for a language_guard_replacement event (SP-152 §152c, item 152.7).
 *
 * Published when a streamed reply that was RELEASED by the streaming
 * hold-back (its start passed the language check) is re-checked at completion
 * and found to have switched language mid-stream. The reply was already
 * streamed to the client and cannot be un-streamed, so the WebUI replaces the
 * already-streamed assistant message's content with `replacement` and keeps
 * `original` (the full switched content) available for "view original".
 *
 * Go: pkg/events.events_filter.go::LanguageGuardReplacementEvent
 */
export interface LanguageGuardReplacementData {
  /** The localized §152b-style notice the user should see instead of the reply. */
  replacement: string;
  /** The full switched content — the "view original" payload. */
  original: string;
  /** What triggered the replacement (e.g. "mid_stream_switch"). */
  reason: string;
  chat_id?: string;
}

// ── SP-151 progress events (SP-151 §151a, item 151.1) ─────────────────────
//
// Structured run-progress signals emitted by the runtime, not parsed from
// model text. Each payload carries the stable correlation IDs (run, plan
// revision, scope item) so consumers can de-duplicate and correlate. Field
// names mirror the Go json tags 1:1 (snake_case).

/**
 * Payload for a progress_milestone event: a plan scope item (SP-148)
 * started or finished, with the files-touched count and the scope item's
 * elapsed wall time.
 *
 * Go: pkg/events/progress_events.go::ProgressMilestoneData
 */
export interface ProgressMilestoneData {
  /** Stable run identifier correlating every event of one run. */
  run_id: string;
  /** Revision of the SP-148 plan (0 when the run has no active plan). */
  plan_revision: number;
  /** Scope item id (plancontract.ScopeItem.ID); absent without plan scope. */
  scope_id?: string;
  /** Scope item's title, for display. */
  scope_title?: string;
  /** "started" | "finished" — which milestone phase this reports. */
  phase: string;
  /** How many files the scope item changed. */
  files_touched?: number;
  /** Scope item's elapsed wall time in milliseconds. */
  elapsed_ms: number;
  /**
   * Present only when the stream coalesced a run of milestone events
   * (SP-151 §151b); each entry is a flat ProgressMilestoneData. A single
   * (non-coalesced) milestone event omits this field.
   */
  milestones?: ProgressMilestoneData[];
}

/**
 * Payload for a progress_question event: the agent needs a decision.
 * Carries the question, options if any, and why it matters — complementing
 * `ask_user_request` with plan context.
 *
 * Go: pkg/events/progress_events.go::ProgressQuestionData
 */
export interface ProgressQuestionData {
  run_id: string;
  plan_revision: number;
  /** Scope item the question belongs to; absent when not scoped. */
  scope_id?: string;
  /** The decision being requested. */
  question: string;
  /** Short categorizing label rendered above the question. */
  header?: string;
  /** Selectable choices; absent for freeform questions. */
  options?: AskUserRequestOption[];
  /** Why the decision matters. */
  why_it_matters?: string;
}

/**
 * Compact evidence a single verification check carries (SP-149). Mirrors
 * the consumer-facing fields of verify.Check; the full result (routes,
 * screenshots, steps, duration) stays server-side.
 *
 * Go: pkg/events/progress_events.go::ProgressVerificationCheck
 */
export interface ProgressVerificationCheck {
  /** plancontract check kind ("build", "test", ...). */
  kind: string;
  /** ids of the plan acceptance items this check covers. */
  items?: string[];
  /** Trusted command that ran (absent when skipped). */
  command?: string;
  /** Check did not run (no trusted command, or the run was cancelled). */
  skipped?: boolean;
  /** Whether the check passed (false for a skipped check). */
  passed?: boolean;
  /** Explains a skipped or abnormal check (timeout, cancellation). */
  reason?: string;
  /** Bounded excerpt of the check's output (evidence). */
  excerpt?: string;
}

/**
 * Payload for a progress_verification event: the SP-149 verification
 * result — the checks, pass/fail, and evidence references.
 *
 * Go: pkg/events/progress_events.go::ProgressVerificationData
 */
export interface ProgressVerificationData {
  run_id: string;
  plan_revision: number;
  /** Run happened without an active SP-148 plan (SP-149 baseline mode). */
  baseline?: boolean;
  /** Nothing failed and at least one check actually ran (SP-149 §149d). */
  passed?: boolean;
  /** Individual check outcomes, in run order. */
  checks: ProgressVerificationCheck[];
  /** Run-level findings that prevented some commands from resolving. */
  errors?: string[];
}

/**
 * Payload for a progress_complete event: the run finished. `verified` is
 * true only when a passing verification result exists (SP-149 §149d /
 * SP-151 §151c). `not_verified_reason` says why only when verification is
 * enabled and the turn was not verified (e.g. "no code changes this
 * turn"); when SP-149 is disabled (the CLI default) both `verification`
 * and `not_verified_reason` are absent — the payload carries just
 * `run_id`, so the default UI is unchanged.
 *
 * Go: pkg/events/progress_events.go::ProgressCompleteData
 */
export interface ProgressCompleteData {
  run_id: string;
  plan_revision: number;
  /** True only when a passing verification result exists. */
  verified?: boolean;
  /** Final verification result; absent when SP-149 is disabled or was not run. */
  verification?: ProgressVerificationData;
  /** Why the run is not verified (e.g. "no code changes this turn"); absent when verification is disabled. */
  not_verified_reason?: string;
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
  /** SP-058: "true" when the server opts the dialog into the 4-option
   *  layout (Approve once / Deny / Always approve / Always ask / Elevate)
   *  instead of the legacy Allow / Block pair. Only shell_command sends
   *  this today. The backend sends it as a string in `extras`. */
  allow_options?: string;
  /** Filesystem approval dialog mode ("fs_external" | "fs_sensitive").
   *  Sent by the backend (extras["kind"]) for out-of-workspace file
   *  accesses. */
  kind?: string;
  /** Folder proposed for the session allowlist (fs_external only).
   *  Backend extras["folder"]. */
  folder?: string;
  /** The exact path being accessed (filesystem dialog).
   *  Backend extras["path"]; falls back to `target`. */
  path?: string;
  /** LLM-generated analysis attached by the backend (SP-124-2). The Go broker
   *  JSON-marshals `pkg/agent.SecurityAnalysis` into a string and shoves it
   *  into `extras["security_analysis"]`, which then lands here verbatim —
   *  a JSON-encoded string, not an object. Consumers (the WebUI handler)
   *  parse it on receive. We expose both shapes: `security_analysis` is the
   *  raw wire value (string for true CSP-safe transport), and the typed
   *  `SecurityAnalysisData` interface documents the parsed shape. */
  security_analysis?: string;
}

/** LLM-generated analysis of a shell command. The full struct lives in
 *  `pkg/agent.SecurityAnalysis` (Go) and serializes to JSON over the
 *  wire — callers receive it as the SecurityAnalysisData shape, not a
 *  string. SP-124-2. */
export interface SecurityAnalysisData {
  summary: string;
  modifies: string;
  risk_assessment: "low" | "moderate" | "high";
  recommendation: "approve" | "review" | "reject";
}

export interface SecurityPromptRequestData {
  request_id: string;
  prompt: string;
  file_path?: string;
  concern?: string;
  status?: string;
  default_response?: boolean;
}

export interface AskUserRequestOption {
  /** Display label rendered in the option list. Required. */
  label: string;
  /** Machine-friendly value returned on selection. Falls back to `label` when omitted. */
  value?: string;
  /** Optional explanatory text shown next to the label. */
  description?: string;
}

export interface AskUserRequestData {
  request_id: string;
  question: string;
  /** Short categorizing label rendered above the question (e.g. "Auth method"). */
  header?: string;
  /** Selectable choices. When present, the dialog renders buttons / checkboxes instead of a freeform textarea. */
  options?: AskUserRequestOption[];
  /** When true, the user may pick multiple options. Response is a comma-joined list of values. */
  multi_select?: boolean;
  /** Default value (option `value` / `label`, or freeform string) pre-selected when the dialog opens. */
  default?: string;
  /**
   * Credential request: the response is diverted to the credential store
   * (never returned to the model). The dialog renders a masked input.
   */
  sensitive?: boolean;
  /** Where the response will be stored (e.g. "mcp/figma/FIGMA_TOKEN"). Not a secret. */
  credential_key?: string;
  client_id?: string;
  status?: string;
}

export interface InputRequiredData {
  reason: string;
  request_id?: string;
  timestamp: string;
  chat_id?: string;
}

/** Payload for a password_request event. Password responses are never included. */
export interface PasswordRequestData {
  request_id: string;
  command: string;
  prompt: string;
  timestamp: string;
  status?: string;
  chat_id?: string;
}

/** A single line in a diff hunk with its change type. */
export interface EditHunkLine {
  type: "context" | "add" | "remove";
  content: string;
}

/** A discrete change region in a unified diff for edit approval. */
export interface EditHunk {
  id: string;
  old_start: number;
  old_lines: number;
  new_start: number;
  new_lines: number;
  lines: EditHunkLine[];
  add_count: number;
  del_count: number;
}

/** Payload for an edit_approval_request event (SP-072-3). */
export interface EditApprovalRequestData {
  request_id: string;
  file_path: string;
  unified_diff?: string;
  hunks: EditHunk[];
  timestamp?: string;
  /** "responded" suppresses the dialog (echo from the decision POST). */
  status?: string;
}

/** A single part of a shell command in a shell_approval_request event (SP-093-3). */
export interface ShellApprovalPartData {
  id: string;
  text: string;
  kind: string;
  semantic: string;
  risk: string;
}

/** Payload for a shell_approval_request event (SP-093-3). */
export interface ShellApprovalRequestData {
  request_id: string;
  command: string;
  parts: ShellApprovalPartData[];
  unified_view: string;
  risk_level: string;
  timestamp?: string;
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

// ── Discriminated Union ────────────────────────────────────────────────

export type WsEvent =
  | {
      type: "connection_status";
      data?: ConnectionStatusData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "query_started";
      data?: QueryStartedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "query_progress";
      data?: QueryProgressData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "query_completed";
      data?: QueryCompletedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "stream_chunk";
      data?: StreamChunkData;
      id?: string;
      timestamp?: string;
    }
  | { type: "error"; data?: ErrorData; id?: string; timestamp?: string }
  | {
      type: "tool_start";
      data?: ToolStartData;
      id?: string;
      timestamp?: string;
    }
  | { type: "tool_end"; data?: ToolEndData; id?: string; timestamp?: string }
  | {
      type: "tool_execution";
      data?: Record<string, unknown>;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "command_output";
      data?: CommandOutputData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "command_output_dropped";
      data?: CommandOutputDroppedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "subagent_activity";
      data?: SubagentActivityData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "delegate_clarification_requested";
      data?: DelegateClarificationRequestedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "delegate_clarification_responded";
      data?: DelegateClarificationRespondedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "agent_message";
      data?: AgentMessageData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "provider_no_credential";
      data?: ProviderNoCredentialData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "todo_update";
      data?: TodoUpdateData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "file_changed";
      data?: FileChangedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "workspace_patch";
      data?: WorkspacePatchData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "file_content_changed";
      data?: FileContentChangedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "metrics_update";
      data?: MetricsUpdateData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "workspace_changed";
      data?: WorkspaceChangedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "session_changed";
      data?: SessionChangedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "rate_limited";
      data?: RateLimitedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "compact_started";
      data?: CompactStartedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "compact_completed";
      data?: CompactCompletedData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "context_management_diagnostic";
      data?: ContextManagementDiagnosticData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "language_guard_replacement";
      data?: LanguageGuardReplacementData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "progress_milestone";
      data?: ProgressMilestoneData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "progress_question";
      data?: ProgressQuestionData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "progress_verification";
      data?: ProgressVerificationData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "progress_complete";
      data?: ProgressCompleteData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "security_approval_request";
      data?: SecurityApprovalRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "security_prompt_request";
      data?: SecurityPromptRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "ask_user_request";
      data?: AskUserRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "input_required";
      data?: InputRequiredData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "edit_approval_request";
      data?: EditApprovalRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "shell_approval_request";
      data?: ShellApprovalRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "password_request";
      data?: PasswordRequestData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "validation";
      data?: Record<string, unknown>;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "terminal_output";
      data?: Record<string, unknown>;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "session_terminated";
      data?: Record<string, unknown>;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "session_ready";
      data?: TerminalSessionReadyData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "session_restored";
      data?: TerminalSessionReadyData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "output";
      data?: TerminalOutputData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "error_output";
      data?: TerminalOutputData;
      id?: string;
      timestamp?: string;
    }
  | {
      type: "pty_exit";
      data?: TerminalPtyExitData;
      id?: string;
      timestamp?: string;
    }
  // Fallback for transport/dev-server events not represented above. Keep the
  // shape closed so excess-property checking still catches misspelled fields.
  | { type: string; data?: unknown; id?: string; timestamp?: string };

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
