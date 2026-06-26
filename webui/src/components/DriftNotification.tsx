import { useCallback } from 'react';
import './DriftNotification.css';

export interface DriftNotificationProps {
  similarity: number;
  threshold: number;
  sessionId: string;
  options: string[];
  onContinue: () => void;
  onNewChat: () => void;
}

/**
 * DriftNotification — A dialog that appears when the backend detects
 * conversation drift and offers the user options to continue or start
 * a new chat.
 */
function DriftNotification({ similarity, threshold, onContinue, onNewChat }: DriftNotificationProps): JSX.Element {
  const simPercent = Math.round(similarity * 100);
  const threshPercent = Math.round(threshold * 100);

  const handleContinue = useCallback(() => {
    onContinue();
  }, [onContinue]);

  const handleNewChat = useCallback(() => {
    onNewChat();
  }, [onNewChat]);

  const hasContinue = true;
  const hasNewChat = true;

  return (
    <div className="drift-overlay" role="dialog" aria-modal="true" aria-label="Conversation drift detected">
      <div className="drift-card" onClick={(e) => e.stopPropagation()}>
        {/* Accent bar - warning color */}
        <div className="drift-accent-bar" />

        {/* Header */}
        <div className="drift-header">
          <span className="drift-icon">⚠</span>
          <h2 className="drift-title">Conversation Drift Detected</h2>
        </div>

        {/* Body */}
        <div className="drift-body">
          <p className="drift-message">The conversation has drifted from the original topic.</p>
          <div className="drift-stats">
            <div className="drift-stat">
              <span className="drift-stat-label">Similarity</span>
              <span className="drift-stat-value">{simPercent}%</span>
            </div>
            <div className="drift-stat">
              <span className="drift-stat-label">Threshold</span>
              <span className="drift-stat-value">{threshPercent}%</span>
            </div>
          </div>
          <p className="drift-hint">Would you like to continue this conversation or start a new one?</p>
        </div>

        {/* Footer */}
        <div className="drift-footer">
          {hasContinue && (
            <button type="button" className="drift-btn drift-btn--continue" onClick={handleContinue}>
              Continue
            </button>
          )}
          {hasNewChat && (
            <button type="button" className="drift-btn drift-btn--new-chat" onClick={handleNewChat}>
              New Chat
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

export default DriftNotification;
