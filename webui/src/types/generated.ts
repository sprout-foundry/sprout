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
  /** Workspace-mode lane ("design"); absent = code/legacy (SP-142). */
  mode?: string;
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
  | 'password_request'
  | 'agent_message'
  | 'workspace_changed'
  | 'session_terminated'
  | 'session_changed'
  | 'delegate_clarification_requested'
  | 'delegate_clarification_responded'
  | 'language_guard_replacement'
  | 'progress_milestone'
  | 'progress_question'
  | 'progress_verification'
  | 'progress_complete'
  // The agent's register_preview_port tool
  // publishes this on the shared event bus after a successful platform
  // registration. In hosted workspaces the agent runs in-process in the
  // webui server, which subscribes to it to record the hosted preview URL
  // (the pane embeds it). It is an in-process signal, NOT forwarded to the
  // browser over the WebSocket, so it is intentionally absent from the
  // WS outbound registry (websocket_outbound_registry.go).
  | 'preview_port_registered';

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

/**
 * Payload of the `409 workspace_busy` rejection (SP-142 §3): a query was
 * submitted from a chat while another chat in the same client context
 * (workspace) had one running. The composer turns this into an inline
 * busy notice with a send-anyway (local queue) affordance.
 *
 * Go: pkg/webui/api_query_shared.go::runChatQuery (workspace gate)
 */
export interface WorkspaceBusyData {
  error: string;
  code: 'workspace_busy';
  running_chat_id: string;
  running_chat_name: string;
}

// ── Progress event payloads ──────────────────────────────────────────
//
// The four progress event types are structured run-progress
// signals emitted by the runtime, not parsed from model text. Each carries
// the stable correlation IDs (run, plan revision, scope item) so consumers
// can de-duplicate and correlate. Field names mirror the Go json tags 1:1
// (snake_case); `@sprout/events` (packages/events/src/types.ts) carries
// the canonical shared versions of these payloads.

/**
 * A single selectable option in a `progress_question` payload. Mirrors the
 * ask_user option shape.
 *
 * Go: pkg/events/events_types.go::AskUserRequestOption
 */
export interface AskUserRequestOption {
  /** Display label rendered in the option list. Required. */
  label: string;
  /** Machine-friendly value returned on selection. Falls back to `label`. */
  value?: string;
  /** Optional explanatory text shown next to the label. */
  description?: string;
}

/**
 * Payload of a `progress_milestone` event: a plan scope
 * item started or finished, with the files-touched count and the
 * scope item's elapsed wall time.
 *
 * This one interface models BOTH wire shapes the event type carries:
 * a flat single-milestone payload (mirrors the Go struct below) and the
 * coalesced batch envelope, which is built by the webui stream coalescer
 * (pkg/webui/stream_coalesce.go::mergeMilestones), not by the Go struct.
 * The batch carries only run_id, the milestones array, and the route keys
 * at the top level — plan_revision, phase, and elapsed_ms exist only on
 * the per-entry flat payloads — which is why those fields are optional.
 *
 * Go: pkg/events/progress_events.go::ProgressMilestoneData
 */
export interface ProgressMilestoneData {
  /** Stable run identifier correlating every event of one run. Present on flat and batched payloads alike. */
  run_id: string;
  /**
   * Revision of the active plan (0 when the run has no active plan).
   * Optional: a coalesced batch envelope has no top-level plan_revision —
   * only the per-milestone entries inside `milestones` carry it.
   */
  plan_revision?: number;
  /** Scope item id (plancontract.ScopeItem.ID); absent without plan scope. */
  scope_id?: string;
  /** Scope item's title, for display. */
  scope_title?: string;
  /**
   * "started" | "finished" — which milestone phase this reports. Optional:
   * a coalesced batch envelope has no top-level phase — each flat entry in
   * `milestones` carries its own.
   */
  phase?: string;
  /** How many files the scope item changed. */
  files_touched?: number;
  /**
   * Scope item's elapsed wall time in milliseconds. Optional: the coalesced
   * batch envelope has no top-level elapsed_ms, and a flat "started"
   * milestone omits it on the wire (only "finished" emits it).
   */
  elapsed_ms?: number;
  /**
   * Present only when the stream coalesced a run of milestone events;
   * each entry is a flat ProgressMilestoneData. A single
   * (non-coalesced) milestone event omits this field.
   */
  milestones?: ProgressMilestoneData[];
  /**
   * Route keys. Stamped onto every flat payload by the event metadata
   * decorator, and copied onto a batch envelope so the coalesced event
   * still reaches its destination. Consumers scope a milestone to a chat
   * by the TOP-LEVEL chat_id — it is present on both flat and batched
   * payloads, so one check covers both shapes.
   */
  client_id?: string;
  chat_id?: string;
  user_id?: string;
}

/**
 * Payload of a `progress_question` event: the agent needs a
 * decision. Carries the question, options if any, and why it matters —
 * complementing `ask_user_request` with plan context.
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
 * Compact evidence a single verification check carries. Mirrors
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
 * Payload of a `progress_verification` event: the verification
 * result — the checks, pass/fail, and evidence references.
 *
 * Go: pkg/events/progress_events.go::ProgressVerificationData
 */
export interface ProgressVerificationData {
  run_id: string;
  plan_revision: number;
  /** Run happened without an active plan (baseline mode). */
  baseline?: boolean;
  /** Nothing failed and at least one check actually ran. */
  passed?: boolean;
  /** Individual check outcomes, in run order. */
  checks: ProgressVerificationCheck[];
  /** Run-level findings that prevented some commands from resolving. */
  errors?: string[];
}

/**
 * Payload of a `progress_complete` event: the run
 * finished. `verified` is true only when a passing verification result
 * exists. `not_verified_reason` says why
 * only when verification is enabled and the turn was not verified (e.g.
 * "no code changes this turn"); when verification is disabled (the CLI
 * default) both `verification` and `not_verified_reason` are absent — the
 * payload carries just `run_id`, so the default UI is unchanged.
 *
 * Go: pkg/events/progress_events.go::ProgressCompleteData
 */
export interface ProgressCompleteData {
  run_id: string;
  plan_revision: number;
  /** True only when a passing verification result exists. */
  verified?: boolean;
  /** Final verification result; absent when verification is disabled or was not run. */
  verification?: ProgressVerificationData;
  /** Why the run is not verified (e.g. "no code changes this turn"); absent when verification is disabled. */
  not_verified_reason?: string;
}
