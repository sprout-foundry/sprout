import { SkeletonText } from '@sprout/ui';
import { Zap, AlertTriangle } from 'lucide-react';
import type { QueryProgress } from '../../types/app';
import { SubagentActivityFeed } from './SubagentActivityFeed';
import { ToolTimelineBar } from './ToolTimelineBar';
import { DelegateActivityTree } from './DelegateActivityTree';
import './ToolTimelineBar.css';
import type { ToolExecution, SubagentActivity, DelegateActivity } from './types';

interface ChatFooterProps {
  hasSubagentActivity: boolean;
  subagentActivities: SubagentActivity[];
  delegateActivities?: DelegateActivity[];
  queryProgress: QueryProgress | null;
  isProcessing: boolean;
  filteredToolExecutions: ToolExecution[];
  lastError: string | null;
  showExpiredSessionRecovery: boolean;
  handleReloadWithoutSSHPath: () => void;
}

export function ChatFooter({
  hasSubagentActivity,
  subagentActivities,
  delegateActivities = [],
  queryProgress,
  isProcessing,
  filteredToolExecutions,
  lastError,
  showExpiredSessionRecovery,
  handleReloadWithoutSSHPath,
}: ChatFooterProps): JSX.Element {
  const elements: JSX.Element[] = [];

  // SP-053-2b: live tool timeline above subagent feed / query progress.
  // Shown whenever there are tool executions to surface (running, or
  // recently-completed within the fade window); ToolTimelineBar itself
  // returns null when there's nothing visible, so no extra guard needed.
  const hasToolsToShow = filteredToolExecutions.length > 0;
  if (hasToolsToShow) {
    elements.push(<ToolTimelineBar key="tool-timeline" toolExecutions={filteredToolExecutions} />);
  }

  if (hasSubagentActivity) {
    elements.push(<SubagentActivityFeed key="subagent" activities={subagentActivities} />);
  }

  if (delegateActivities.length > 0) {
    elements.push(
      <div key="delegate-activities" className="delegate-activities-container">
        {delegateActivities.map((da) => (
          <DelegateActivityTree key={da.delegateId} activity={da} />
        ))}
      </div>,
    );
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
          <SkeletonText lines={2} gap="6px" lineHeight="14px" lastLineWidth="40%" />
          <span className="sr-only">Processing your request...</span>
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
