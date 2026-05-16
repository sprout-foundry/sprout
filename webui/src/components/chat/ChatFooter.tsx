import { Zap, AlertTriangle } from 'lucide-react';
import { SubagentActivityFeed } from './SubagentActivityFeed';
import { SkeletonText } from '@sprout/ui';
import type { ToolExecution, SubagentActivity } from './types';
import type { QueryProgress } from '../../types/app';

interface ChatFooterProps {
  hasSubagentActivity: boolean;
  subagentActivities: SubagentActivity[];
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
  queryProgress,
  isProcessing,
  filteredToolExecutions,
  lastError,
  showExpiredSessionRecovery,
  handleReloadWithoutSSHPath,
}: ChatFooterProps): JSX.Element {
  const elements: JSX.Element[] = [];

  if (hasSubagentActivity) {
    elements.push(<SubagentActivityFeed key="subagent" activities={subagentActivities} />);
  }

  if (queryProgress) {
    elements.push(
      <div key="progress" className="query-progress">
        <div className="progress-header">
          <span className="progress-icon">
            <Zap size={14} />
          </span>
          <span className="progress-text">
            {queryProgress.message || 'Processing...'}
          </span>
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

  if (isProcessing && filteredToolExecutions.length === 0 && !queryProgress && !hasSubagentActivity) {
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
