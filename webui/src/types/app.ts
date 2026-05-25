/**
 * Shared application types extracted from App.tsx
 *
 * These types define the core state shape and data structures used
 * throughout the application.
 */

import type {
  Message,
  ToolExecution,
  SubagentActivity,
  DelegateActivity,
  LogEntry,
  TodoStatus,
  TodoItem,
  FileEdit,
  ToolRef,
} from '@sprout/ui';
import type { OnboardingEnvironment, OnboardingProviderOption } from '../services/api';
import type { ChatSession } from '../services/chatSessions';

// Import canonical types from @sprout/ui

// Re-export for downstream consumers
export type { Message, ToolExecution, SubagentActivity, DelegateActivity, LogEntry, TodoStatus, TodoItem, FileEdit, ToolRef };

// ── WebUI-specific Types ─────────────────────────────────────────────

/** Typed shape of websocket query_progress events. */
export interface QueryProgress {
  message: string;
  details?: unknown;
}

/** Defensively construct a QueryProgress from raw websocket event data. */
export function toQueryProgress(raw: Record<string, unknown>): QueryProgress {
  return {
    message: typeof raw.message === 'string' ? raw.message : 'Processing...',
    details: 'details' in raw ? raw.details : undefined,
  };
}

export interface WorktreeInfo {
  path: string;
  branch: string;
  is_main: boolean;
  is_current: boolean;
  parent_path?: string;
  parent_branch?: string;
}

export interface PerChatState {
  messages: Message[];
  toolExecutions: ToolExecution[];
  fileEdits: FileEdit[];
  subagentActivities: SubagentActivity[];
  delegateActivities: DelegateActivity[];
  currentTodos: TodoItem[];
  queryProgress: QueryProgress | null;
  lastError: string | null;
  isProcessing: boolean;
  provider: string;
  model: string;
  worktreePath?: string;
  queryCount: number;
}

export interface AppState {
  isConnected: boolean;
  provider: string;
  model: string;
  sessionId: string | null;
  queryCount: number;
  messages: Message[];
  logs: LogEntry[];
  isProcessing: boolean;
  lastError: string | null;
  currentView: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team';
  toolExecutions: ToolExecution[];
  queryProgress: QueryProgress | null;
  stats: Record<string, unknown>; // Enhanced stats from API
  currentTodos: TodoItem[];
  fileEdits: FileEdit[];
  subagentActivities: SubagentActivity[];
  delegateActivities: DelegateActivity[];
  activeChatId: string | null;
  chatSessions: ChatSession[];
  // Snapshot of per-chat state, saved on switch-away and restored on switch-back
  perChatCache: Record<string, PerChatState>;
  securityApprovalRequest: {
    requestId: string;
    toolName: string;
    riskLevel: string;
    reasoning: string;
    command?: string;
    riskType?: string;
    target?: string;
  } | null;
  securityPromptRequest: {
    requestId: string;
    prompt: string;
    filePath?: string;
    concern?: string;
  } | null;
  askUserRequest: {
    requestId: string;
    question: string;
  } | null;
  modelSelectionRequest: {
    provider: string;
    /**
     * Why the modal opened. `unavailable` = backend told us the configured
     * model isn't available and we need a replacement (warning treatment);
     * `switch` = user clicked the model name in the status bar to
     * proactively change (neutral treatment). Defaults to `unavailable`
     * when omitted so legacy callers keep their existing UX.
     */
    reason?: 'unavailable' | 'switch';
  } | null;
  driftNotification: {
    similarity: number;
    threshold: number;
    sessionId: string;
    options: string[];
  } | null;
}

export interface OnboardingState {
  checking: boolean;
  open: boolean;
  reason: string;
  providers: OnboardingProviderOption[];
  environment: OnboardingEnvironment | null;
  provider: string;
  model: string;
  apiKey: string;
  showAllProviders: boolean;
  submitting: boolean;
  platformActionMessage: string | null;
  error: string | null;
  initialModelSet: boolean;
  /** True when API key validation succeeded (set briefly before dialog closes). */
  validationSuccess: boolean;
  /** Number of models reported by the provider on successful validation. */
  validationModelCount: number;
  /** True when the current error is specifically an API key validation failure. */
  keyError: boolean;
  /** True when the dialog was opened for re-onboarding (from settings), not first-run. */
  isReonboarding: boolean;
}
