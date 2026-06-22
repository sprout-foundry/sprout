import { SkeletonText, type TodoItem } from '@sprout/ui';
import { Zap, AlertTriangle, Bot, ListTodo } from 'lucide-react';
import type { QueryProgress } from '../../types/app';
import { SubagentActivityFeed } from './SubagentActivityFeed';
import { ToolTimelineBar } from './ToolTimelineBar';
import './ToolTimelineBar.css';
import type { ToolExecution, SubagentActivity } from './types';

// SP-059 Phase 1c: derive whether a subagent is *currently running* (not
// just present in the recent feed). The activity stream carries lifecycle
// `status` and a `phase` field — a subagent is live if any recent activity
// is `started`/`running` without a matching `completed`/`cancelled`.
function hasLiveSubagent(activities: SubagentActivity[]): boolean {
  if (activities.length === 0) return false;
  // Walk recent activities and track per-tool-call liveness. The feed is
  // small (capped at 500), so the linear scan is fine here.
  const live = new Map<string, boolean>();
  for (const a of activities) {
    const key = a.toolCallId || a.taskId || 'unknown';
    if (a.phase === 'spawn' || a.status === 'queued' || a.status === 'started') {
      live.set(key, true);
    } else if (a.phase === 'complete' || a.status === 'completed' || a.status === 'cancelled') {
      live.set(key, false);
    }
  }
  for (const v of live.values()) {
    if (v) return true;
  }
  return false;
}

interface ChatFooterProps {
  hasSubagentActivity: boolean;
  subagentActivities: SubagentActivity[];
  queryProgress: QueryProgress | null;
  isProcessing: boolean;
  filteredToolExecutions: ToolExecution[];
  lastError: string | null;
  showExpiredSessionRecovery: boolean;
  handleReloadWithoutSSHPath: () => void;
  currentTodos?: TodoItem[];
}

export function ChatFooter({
  hasSubagentActivity,
  subagentActivities,
  queryProgress,
  isProcessing,
  filteredToolExecutions,
  lastError,
  showExpiredSessionRecovery,
  handleReloadWithoutSSHPath,
  currentTodos,
}: ChatFooterProps): JSX.Element {
  const activeTodo = isProcessing && currentTodos?.find((t) => t.status === 'in_progress');
  const activeTodoLabel = activeTodo ? activeTodo.activeForm || activeTodo.content : null;
  // ToolTimelineBar is always mounted (it owns its own visibility), but the
  // processing-indicator guard below still needs to know whether any tool
  // execution is present so it doesn't render a skeleton alongside the bar.
  const hasToolsToShow = filteredToolExecutions.length > 0;
  const elements: JSX.Element[] = [];

  // SP-053-2b: live tool timeline above subagent feed / query progress.
  // Always mount; ToolTimelineBar owns its own visibility (including a
  // hide-grace window) so rapid back-to-back tools don't churn the DOM.
  // An outer length-gate here would unmount the bar between consecutive
  // tools whose lifetimes don't quite overlap, which the user perceives
  // as the live bar flickering on and off.
  elements.push(<ToolTimelineBar key="tool-timeline" toolExecutions={filteredToolExecutions} />);

  if (hasSubagentActivity) {
    // SP-059 Phase 1c: when a subagent is currently running, show a pill
    // above the activity feed so the user knows the next thing they type
    // will steer the subagent (not queue against the primary).
    const subagentLive = hasLiveSubagent(subagentActivities);
    if (subagentLive) {
      elements.push(
        <div key="subagent-routing" className="subagent-routing-pill" role="status" aria-live="polite">
          <Bot size={12} className="subagent-routing-icon" />
          <span>Subagent running — your next message will steer it</span>
        </div>,
      );
    }
    elements.push(<SubagentActivityFeed key="subagent" activities={subagentActivities} />);
  }

  if (queryProgress) {
    elements.push(
      <div key="progress" className="query-progress">
        <div className="progress-header">
          <span className="progress-icon">
            <Zap size={14} />
          </span>
          <span className="progress-text">{queryProgress.message || 'Processing...'}</span>
        </div>
        {queryProgress.details != null && (
          <div className="progress-details">
            {typeof queryProgress.details === 'string' || typeof queryProgress.details === 'number'
              ? String(queryProgress.details)
              : null}
          </div>
        )}
      </div>,
    );
  }

  if (isProcessing && !hasToolsToShow && !queryProgress && !hasSubagentActivity) {
    elements.push(
      <div key="processing" className="processing-indicator" role="status" aria-label="Processing request">
        <div className="processing-content">
          {activeTodoLabel ? (
            <div className="processing-active-task" aria-live="polite">
              <ListTodo size={12} className="processing-active-task-icon" />
              <span className="processing-active-task-text">{activeTodoLabel}</span>
            </div>
          ) : null}
          <SkeletonText lines={2} gap="6px" lineHeight="14px" lastLineWidth="40%" />
          <span className="sr-only">{activeTodoLabel ? `Processing: ${activeTodoLabel}` : 'Processing your request...'}</span>
        </div>
      </div>,
    );
  }

  if (lastError) {
    elements.push(
      <div key="error" className="error-indicator">
        <div className="error-content">
          <div className="error-icon">
            <AlertTriangle size={14} />
          </div>
          <div className="error-text">{lastError}</div>
          {showExpiredSessionRecovery ? (
            <div className="error-actions">
              <button type="button" className="error-recovery-btn" onClick={handleReloadWithoutSSHPath}>
                Reload Without SSH Path
              </button>
            </div>
          ) : null}
        </div>
      </div>,
    );
  }

  return elements.length === 1 ? elements[0] : <>{elements}</>;
}
