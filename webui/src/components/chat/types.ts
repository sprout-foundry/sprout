import type { VirtuosoHandle } from 'react-virtuoso';

// ── Types ────────────────────────────────────────────────────────────

export interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  reasoning?: string;
  toolRefs?: Array<{ toolId: string; toolName: string; label: string; parallel?: boolean; toolIndex?: number }>;
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
  toolIndex?: number;
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
}

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
  currentTodos?: Array<{
    id: string;
    content: string;
    status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
  }>;
  subagentActivities?: SubagentActivity[];
  onToolPillClick?: (toolId: string) => void;
  onStopProcessing?: () => void;
  chatId?: string;
  worktreePath?: string;
  workspaceRoot?: string;
  onWorktreeChange?: (worktreePath: string) => void;
  providerAvailable?: boolean;
  onRequestProviderSetup?: () => void;
  stats?: Record<string, unknown>;
  isConnected?: boolean;
  backendReachable?: boolean;
  onRetryConnection?: () => void;
}

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
}

// ── Constants ────────────────────────────────────────────────────────

export const MAX_ACTIVE_LINES = 50;
export const MAX_COMPLETED_SUMMARIES = 3;

// ── Chat Internal State Types ────────────────────────────────────────

export interface ChatRefs {
  chatShellRef: { current: HTMLDivElement | null };
  chatContainerRef: { current: HTMLDivElement | null };
  virtuosoRef: { current: VirtuosoHandle | null };
  inputContainerRef: { current: HTMLDivElement | null };
}
