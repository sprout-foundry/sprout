import type {
  ToolExecution,
  LogEntry,
  SubagentActivity,
  TodoItem,
  FileEdit,
  LiveLogLine,
  RevisionFile,
  Revision,
  RevisionDetailFile,
} from '@sprout/ui';
import type { ReactNode, CSSProperties } from 'react';
import type { SessionEntry } from '../../services/api/types';
import type { QueryProgress } from '../../types/app';

// Re-export shared types from @sprout/ui for convenience
export type {
  ToolExecution,
  LogEntry,
  SubagentActivity,
  TodoItem,
  FileEdit,
  LiveLogLine,
  RevisionFile,
  Revision,
  RevisionDetailFile,
};

// Re-export SessionEntry from services/api/types for convenience
export type { SessionEntry };

// ── WebUI-specific Core data interfaces ─────────────────────────────

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

// ── Derived types for subagent runs (webui view-model, not @sprout/ui) ──

export interface ContextSubagentActivityItem {
  id: string;
  timestamp: Date;
  taskId?: string;
  label: string;
  isSpawn: boolean;
}

export interface ContextSubagentTaskGroup {
  taskId: string | null;
  items: ContextSubagentActivityItem[];
  latest: ContextSubagentActivityItem;
}

export interface ContextSubagentRun {
  tool: ToolExecution;
  prompt?: string;
  latestActivity?: ContextSubagentActivityItem;
  activities: ContextSubagentActivityItem[];
  orderedTaskGroups: ContextSubagentTaskGroup[];
}

export interface ContextNormalizedActivity {
  taskId?: string;
  label: string;
  isSpawn: boolean;
}

/** Aggregated lifecycle counts per status bucket (derived from SubagentActivity.status). */
export interface SubagentLifecycleCounts {
  queued: number;
  active: number;
  completed: number;
  cancelled: number;
}

export interface SubagentResourceCounts {
  active: number;
  queued: number;
  completed: number;
  failed: number;
  cancelled: number;
}

export interface SubagentRunResult {
  subagentRuns: ContextSubagentRun[];
  resourceCounts: SubagentResourceCounts;
}

// ── Props ───────────────────────────────────────────────────────────

export interface ContextPanelBaseProps {
  className?: string;
  style?: CSSProperties;
  isMobileLayout?: boolean;
  isTabletLayout?: boolean;
  panelWidth?: number;
  onPanelWidthChange?: (width: number) => void;
  onMobileOpenChange?: (open: boolean) => void;
  onCollapsedChange?: (collapsed: boolean) => void;
}

export interface ChatContextPanelProps extends ContextPanelBaseProps {
  context: 'chat';
  toolExecutions: ToolExecution[];
  fileEdits: FileEdit[];
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  currentTodos: TodoItem[];
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: QueryProgress | null;
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
  onRestoreSession: (sessionId: string) => Promise<{ messages: unknown[] }>;
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
/** Width of the side-rail-only collapsed context panel (px). Must match .context-panel.collapsed width in ContextPanel.css. */
export const PANEL_COLLAPSED_WIDTH = 52;
export const MOBILE_LAYOUT_MAX_WIDTH = 768;

export type ChatTabId = 'subagents' | 'tools' | 'changes' | 'tasks' | 'status' | 'sessions';

export interface PanelTab {
  id: ChatTabId;
  label: string;
  icon: ReactNode;
  count: string;
}
