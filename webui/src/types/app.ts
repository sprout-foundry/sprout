/**
 * Shared application types extracted from App.tsx
 *
 * These types define the core state shape and data structures used
 * throughout the application.
 */

import type { ChatSession } from '../services/chatSessions';
import type { OnboardingEnvironment, OnboardingProviderOption } from '../services/api';

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
}

export interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string; // Chain-of-thought content from content_type: "reasoning"
  toolRefs?: Array<{ toolId: string; toolName: string; label: string; parallel?: boolean }>;
}

export interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: unknown;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

export interface SubagentActivity {
  id: string;
  toolCallId: string;
  toolName: string;
  phase: 'spawn' | 'output' | 'complete';
  message: string;
  timestamp: Date;
  taskId?: string;
  persona?: string;
  isParallel?: boolean;
  provider?: string;
  model?: string;
  taskCount?: number;
  failures?: number;
}

export type TodoStatus = 'pending' | 'in_progress' | 'completed' | 'cancelled';

export interface TodoItem {
  id: string;
  content: string;
  status: TodoStatus;
}

export interface FileEdit {
  path: string;
  action: string;
  timestamp: Date;
  linesAdded?: number;
  linesDeleted?: number;
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
  currentView: 'chat' | 'editor' | 'git';
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
}
