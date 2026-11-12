import type {
  ToolExecution,
  LogEntry,
  SubagentActivity,
  LiveLogLine,
  RevisionFile,
  Revision,
  RevisionDetailFile,
} from '@sprout/ui';
import type { QueryProgress } from '../../types/app';

// Re-export shared types from @sprout/ui for convenience
export type { ToolExecution, LogEntry, SubagentActivity, LiveLogLine, RevisionFile, Revision, RevisionDetailFile };

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

/** The chat activity a subagent-run view is built from. */
export interface ChatContextPanelProps {
  context: 'chat';
  toolExecutions: ToolExecution[];
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: QueryProgress | null;
}
