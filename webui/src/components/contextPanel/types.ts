import type { ReactNode, CSSProperties } from 'react';

// ── Core data interfaces ───────────────────────────────────────────

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
}

export interface RevisionFile {
  file_revision_hash?: string;
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
}

export interface Revision {
  revision_id: string;
  timestamp: string;
  files: RevisionFile[];
  description: string;
}

export interface RevisionDetailFile extends RevisionFile {
  original_code: string;
  new_code: string;
  diff: string;
}

export interface SessionEntry {
  session_id: string;
  name: string;
  working_directory: string;
  last_updated: string;
  message_count: number;
  total_tokens: number;
}

export interface StatusMetrics {
  userMsgs: number;
  assistantMsgs: number;
  totalMsgs: number;
  completedTools: number;
  failedTools: number;
  activeTools: number;
  totalTools: number;
  totalAdditions: number;
  totalDeletions: number;
  filesTouched: number;
  topTools: Array<[string, number]>;
  maxToolCount: number;
  duration: number;
}

// ── Derived types for subagent runs ────────────────────────────────

export interface SubagentActivityItem {
  id: string;
  timestamp: Date;
  taskId?: string;
  label: string;
  isSpawn: boolean;
}

export interface SubagentTaskGroup {
  taskId: string | null;
  items: SubagentActivityItem[];
  latest: SubagentActivityItem;
}

export interface SubagentRun {
  tool: ToolExecution;
  prompt?: string;
  latestActivity?: SubagentActivityItem;
  activities: SubagentActivityItem[];
  orderedTaskGroups: SubagentTaskGroup[];
}

export interface NormalizedActivity {
  taskId?: string;
  label: string;
  isSpawn: boolean;
}

export interface LiveLogLine {
  id: string;
  text: string;
  timestamp: Date;
  taskId?: string;
}

// ── Props ───────────────────────────────────────────────────────────

export interface ContextPanelBaseProps {
  className?: string;
  style?: CSSProperties;
  isMobileLayout?: boolean;
  panelWidth?: number;
  onPanelWidthChange?: (width: number) => void;
  onMobileOpenChange?: (open: boolean) => void;
  onCollapsedChange?: (collapsed: boolean) => void;
}

export interface ChatContextPanelProps extends ContextPanelBaseProps {
  context: 'chat';
  toolExecutions: ToolExecution[];
  fileEdits: Array<{
    path: string;
    action: string;
    timestamp: Date;
    linesAdded?: number;
    linesDeleted?: number;
  }>;
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  currentTodos: Array<{
    id: string;
    content: string;
    status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
  }>;
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: unknown;
  stats?: {
    provider?: string;
    model?: string;
    total_tokens?: number;
    prompt_tokens?: number;
    completion_tokens?: number;
    cached_tokens?: number;
    current_context_tokens?: number;
    max_context_tokens?: number;
    context_usage_percent?: number;
    cache_efficiency?: number;
    total_cost?: number;
    cached_cost_savings?: number;
    last_tps?: number;
    current_iteration?: number;
    max_iterations?: number;
    streaming_enabled?: boolean;
    debug_mode?: boolean;
    context_warning_issued?: boolean;
    uptime?: string;
    query_count?: number;
  };
  onHandleToolPillClick?: (toolId: string) => void;
  onOpenRevisionDiff?: (options: { path: string; diff: string; title: string }) => void;
  onLoadRevisionHistory: () => Promise<{ revisions: Revision[] }>;
  onLoadSessions: () => Promise<{ sessions: SessionEntry[]; current_session_id: string }>;
  onRestoreSession: (sessionId: string) => Promise<{ messages: any[] }>;
  onLoadRevisionDetails: (revisionId: string) => Promise<{ revision?: { files: RevisionDetailFile[] } }>;
}

export type ContextPanelProps = ChatContextPanelProps;

// ── Public API via ref ─────────────────────────────────────────────

export interface ContextPanelHandle {
  openTab: (tab: string) => void;
  highlightTool: (toolId: string) => void;
  closePanel: () => void;
}

// ── Constants ──────────────────────────────────────────────────────

export const PANEL_COLLAPSED_KEY = 'sprout.contextPanel.collapsed';
export const PANEL_TAB_KEY = 'sprout.contextPanel.tab';
export const PANEL_MIN = 280;
export const PANEL_MAX = 760;
export const MOBILE_LAYOUT_MAX_WIDTH = 768;

export type ChatTabId = 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';

export interface PanelTab {
  id: ChatTabId;
  label: string;
  icon: ReactNode;
  count: string;
}
