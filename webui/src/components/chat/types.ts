import type {
  Message,
  ToolExecution,
  SubagentActivity,
  TodoItem,
  SubagentRun,
  LiveLogLine,
  ChatProps,
} from '@sprout/ui';
import { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES } from '@sprout/ui';
import type { VirtuosoHandle } from 'react-virtuoso';

// Re-export shared types for convenience
export type { Message, ToolExecution, SubagentActivity, TodoItem, SubagentRun, LiveLogLine, ChatProps };

// Re-export constants for convenience
export { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES };

// ── WebUI-specific Types ─────────────────────────────────────────────

// ChatProps is now imported from @sprout/ui and re-exported above for convenience

// ── Chat Internal State Types ────────────────────────────────────────

export interface ChatRefs {
  chatShellRef: { current: HTMLDivElement | null };
  chatContainerRef: { current: HTMLDivElement | null };
  virtuosoRef: { current: VirtuosoHandle | null };
  inputContainerRef: { current: HTMLDivElement | null };
}
