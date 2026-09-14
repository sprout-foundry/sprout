/**
 * Shared chat-related types for @sprout/ui.
 *
 * These types define the core data structures used across the chat UI:
 * messages, tool executions, subagent activities, log entries, todos,
 * and file edits. They are the single canonical source — consumers in
 * both `packages/ui` and `webui` should import from here (via `@sprout/ui`).
 */

// ── Core data types ────────────────────────────────────────────────

export interface ToolRef {
  toolId: string;
  toolName: string;
  label: string;
  parallel?: boolean;
  toolIndex?: number;
}

export interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string; // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: ToolRef[];
  /**
   * SP-053-1b: persona ID (e.g. "coder", "tester") when this message
   * originated from a subagent. Drives the colored persona badge in
   * MessageBubble. Absent for primary-agent messages.
   */
  persona?: string;
  /**
   * SP-053-1b: nesting depth — 0=primary agent, 1=orchestrator,
   * 2=specialist subagent. Drives the left-margin indent in MessageBubble
   * so a delegation chain reads as a visible hierarchy. Absent or 0 means
   * primary agent (no indent).
   */
  subagentDepth?: number;
  /**
   * SP-053-perTurnCost: tokens consumed for this turn (input + output).
   * Populated from query_completed event. Only shown for assistant messages.
   */
  tokensUsed?: number;
  /**
   * SP-053-perTurnCost: cost in dollars for this turn.
   * Populated from query_completed event. Only shown for assistant messages.
   */
  cost?: number;
  /**
   * SP-053-perTurnCost: model used for this turn (e.g. "gpt-4o").
   * Populated from query_completed or metrics_update event.
   */
  model?: string;
  /**
   * Inline subagent run marker — when true, this message represents
   * a subagent activity rendered inline in the chat flow as a
   * collapsible section (rather than the old footer feed). The
   * subagent's streaming output lines accumulate in the `reasoning`
   * field and render via a Collapsible in MessageItem.
   */
  isSubagentRun?: boolean;
  /** Whether the inline subagent run has completed. */
  subagentRunComplete?: boolean;
  /** Persona name for the inline subagent run (e.g. "coder", "tester"). */
  subagentPersona?: string;
}

export interface ToolExecution {
  id: string;
  tool: string;
  status: 'started' | 'running' | 'completed' | 'error';
  message?: string;
  startTime: Date;
  endTime?: Date;
  details?: unknown;
  arguments?: string;
  result?: string;
  persona?: string;
  subagentType?: 'single' | 'parallel';
  queryId?: number;
  /** Index of tool within its query's tool list */
  toolIndex?: number;
  /** Nesting depth: 0=primary, 1=orchestrator, 2=specialist */
  depth?: number;
}

export interface SubagentActivity {
  id: string;
  toolCallId: string;
  toolName: string;
  phase: 'spawn' | 'output' | 'complete' | 'step';
  message: string;
  timestamp: Date;
  taskId?: string;
  persona?: string;
  isParallel?: boolean;
  provider?: string;
  model?: string;
  taskCount?: number;
  failures?: number;
  tool?: string;
  /** Lifecycle status: "queued", "started", "completed", "cancelled" */
  status?: 'queued' | 'started' | 'completed' | 'cancelled';
  /** Reason for cancellation (e.g. "budget exceeded") */
  reason?: string;
  /** Tokens consumed by this subagent task */
  tokensUsed?: number;
  /** Cost in dollars for this subagent task */
  cost?: number;
  /** Duration in milliseconds */
  elapsedMs?: number;
  /** Nesting depth: 0=primary, 1=orchestrator, 2=specialist */
  depth?: number;
}

export interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: unknown;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

export type TodoStatus = 'pending' | 'in_progress' | 'completed' | 'cancelled';
export type TodoPriority = 'high' | 'medium' | 'low';

export interface TodoItem {
  id: string;
  content: string;
  status: TodoStatus;
  /** Present-continuous phrasing surfaced while status === 'in_progress' (e.g. content "Implement X" → activeForm "Implementing X"). */
  activeForm?: string;
  /** Visual hint only; drives the priority indicator color. */
  priority?: TodoPriority;
}

export interface FileEdit {
  path: string;
  action: string;
  timestamp: Date;
  linesAdded?: number;
  linesDeleted?: number;
  /** Server-side RFC3339 timestamp from the file_changed event, when the
   * backend provides one. Revert-since floors must use server time. */
  serverTs?: string;
  /** The queryCount of the turn that produced this edit (same correlation
   * pattern as ToolExecution.queryId). */
  queryId?: number;
}

// ── Live Log Types ─────────────────────────────────────────────────────

export interface LiveLogLine {
  id: string;
  text: string;
  timestamp: Date;
  taskId?: string;
}

// ── Subagent Activity Types ─────────────────────────────────────────────

export interface SubagentRun {
  toolCallId: string;
  persona: string;
  isParallel: boolean;
  isComplete: boolean;
  completionMessage: string;
  completionTimestamp: Date | null;
  activities: SubagentActivity[];
  spawnActivity: SubagentActivity | null;
  completeActivity: SubagentActivity | null;
  outputLines: Array<{ id: string; text: string; timestamp: Date; taskId?: string }>;
  /** Nesting depth: 0=primary, 1=orchestrator, 2=specialist */
  depth: number;
  /** Sum of tokens used across all activities in this run */
  tokensUsed: number;
  /** Sum of costs across all activities in this run */
  cost: number;
}

// ── Chat Component Props ───────────────────────────────────────────────
/**
 * Shared props interface for chat components.
 * This is the canonical definition used across ChatPanel implementations.
 */
export interface ChatProps {
  messages: Message[];
  onSendMessage: (message: string) => void;
  onQueueMessage: (message: string) => void;
  queuedMessagesCount: number;
  queuedMessages?: string[];
  onQueueMessageRemove?: (index: number) => void;
  onQueueMessageEdit?: (index: number, newText: string) => void;
  onQueueReorder?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  isProcessing?: boolean;
  lastError?: string | null;
  toolExecutions?: ToolExecution[];
  queryProgress?: unknown;
  currentTodos?: TodoItem[];
  subagentActivities?: SubagentActivity[];
  onToolPillClick?: (toolId: string) => void;
  onStopProcessing?: () => void;
  onRetractSteer?: () => boolean | Promise<boolean>;
  /**
   * Optimistic clear for the New Session (/clear) action. Called the moment
   * the user invokes clear so the transcript empties instantly; the backend
   * confirms via session_changed("clear") afterwards. Optional — without it
   * the UI waits for the backend round trip.
   */
  onChatCleared?: () => void;
  /**
   * Per-turn agent-change summaries (SP-139 Phase 2). When provided, each
   * completed turn with edits renders a collapsed "N files changed" strip
   * with Review / Revert actions. Empty array disables the feature.
   */
  fileEdits?: FileEdit[];
  /**
   * Opens a review surface for one changed file, given the agent-session
   * diff already fetched by the strip.
   */
  onReviewChange?: (path: string, diff: { stats?: string; diff?: string }) => void;
  /**
   * Restores a saved conversation into this chat (SP-139 Phase 3 header
   * history switcher). Receives the chat's own id so restores stay scoped
   * to the pane that triggered them.
   */
  onRestoreSession?: (sessionId: string, chatId?: string) => void | Promise<void>;
  /**
   * Client-side turn counter (AppState.queryCount) — the SAME counter
   * ToolExecution.queryId and FileEdit.queryId are stamped with. Consumers
   * that group by turn must use this, NOT stats.queryCount (a server
   * global that diverges after restores / multi-tab / multi-chat).
   */
  queryCount?: number;
  // Worktree support
  chatId?: string;
  worktreePath?: string;
  workspaceRoot?: string;
  onWorktreeChange?: (worktreePath: string) => void;
  // Provider availability
  providerAvailable?: boolean;
  onRequestProviderSetup?: () => void;
  // Status bar
  stats?: Record<string, unknown>;
  isConnected?: boolean;
  // Backend reachability (cloud mode)
  backendReachable?: boolean;
  onRetryConnection?: () => void;
  // SP-076: display verbosity for inter-tool narration filtering
  outputVerbosity?: 'compact' | 'default' | 'verbose';
  // Fork support: callback when user clicks fork icon on a user message
  onForkAtBreakpoint?: (breakpointIndex: number) => void;
  // Fork support: true while a fork operation is in-flight (disables button)
  isForking?: boolean;
}

// ── Constants ──────────────────────────────────────────────────────────

export const MAX_ACTIVE_LINES = 50;
export const MAX_COMPLETED_SUMMARIES = 3;
