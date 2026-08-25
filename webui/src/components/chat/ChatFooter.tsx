import { SkeletonText, type TodoItem } from '@sprout/ui';
import { Zap, AlertTriangle, ListTodo } from 'lucide-react';
import type { QueryProgress } from '../../types/app';
import type { ToolExecution } from './types';

interface ChatFooterProps {
  queryProgress: QueryProgress | null;
  isProcessing: boolean;
  filteredToolExecutions: ToolExecution[];
  lastError: string | null;
  showExpiredSessionRecovery: boolean;
  handleReloadWithoutSSHPath: () => void;
  currentTodos?: TodoItem[];
}

export function ChatFooter({
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

  if (queryProgress) {
    elements.push(
      <div key="progress" className="query-progress" data-testid="chat-query-progress">
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

  if (isProcessing && !hasToolsToShow && !queryProgress) {
    elements.push(
      <div
        key="processing"
        className="processing-indicator"
        role="status"
        aria-label="Processing request"
        data-testid="chat-processing"
      >
        <div className="processing-content">
          {activeTodoLabel ? (
            <div className="processing-active-task" aria-live="polite">
              <ListTodo size={12} className="processing-active-task-icon" />
              <span className="processing-active-task-text">{activeTodoLabel}</span>
            </div>
          ) : null}
          <SkeletonText lines={2} gap="6px" lineHeight="14px" lastLineWidth="40%" />
          <span className="sr-only">
            {activeTodoLabel ? `Processing: ${activeTodoLabel}` : 'Processing your request...'}
          </span>
        </div>
      </div>,
    );
  }

  if (lastError) {
    elements.push(
      <div key="error" className="error-indicator" data-testid="chat-error">
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
