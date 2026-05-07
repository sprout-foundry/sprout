/**
 * Shared application types extracted from App.tsx
 *
 * These types define the core state shape and data structures used
 * throughout the application.
 */

import type { ChatSession } from '../services/chatSessions';
import type { OnboardingEnvironment, OnboardingProviderOption } from '../services/api';

// Import canonical types from @sprout/ui
import type {
  Message,
  ToolExecution,
  SubagentActivity,
  LogEntry,
  TodoStatus,
  TodoItem,
  FileEdit,
  ToolRef,
} from '@sprout/ui';

// Re-export for downstream consumers
export type {
  Message,
  ToolExecution,
  SubagentActivity,
  LogEntry,
  TodoStatus,
  TodoItem,
  FileEdit,
  ToolRef,
};

// ── WebUI-specific Types ─────────────────────────────────────────────

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
  currentTodos: TodoItem[];
  queryProgress: unknown;
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
  queryProgress: unknown;
  stats: Record<string, unknown>; // Enhanced stats from API
  currentTodos: TodoItem[];
  fileEdits: FileEdit[];
  subagentActivities: SubagentActivity[];
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
