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
}

// ── Constants ──────────────────────────────────────────────────────────

export const MAX_ACTIVE_LINES = 50;
export const MAX_COMPLETED_SUMMARIES = 3;
