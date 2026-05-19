import React, { useEffect, useCallback } from 'react';
import './DriftNotification.css';

export interface DriftNotificationProps {
  similarity: number;
  threshold: number;
  onDismiss: () => void;
  onStartNewChat: () => void;
}

function DriftNotification({
  similarity,
  threshold,
  onDismiss,
  onStartNewChat,
}: DriftNotificationProps): JSX.Element {
  // Auto-dismiss after 30 seconds
  useEffect(() => {
    const timer = setTimeout(() => {
      onDismiss();
    }, 30000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  const handleDismiss = useCallback(() => {
    onDismiss();
  }, [onDismiss]);

  const handleNewChat = useCallback(() => {
    onStartNewChat();
  }, [onStartNewChat]);

  const similarityPct = Math.round(similarity * 100);
  const thresholdPct = Math.round(threshold * 100);

  return (
    <div className="drift-notification" role="alert" aria-live="polite">
      <div className="drift-notification-content">
        <div className="drift-notification-icon">⚠️</div>
        <div className="drift-notification-text">
          <strong>Conversation drift detected</strong>
          <span className="drift-notification-detail">
            Topic similarity: {similarityPct}% (threshold: {thresholdPct}%)
          </span>
        </div>
        <div className="drift-notification-actions">
          <button
            className="drift-notification-btn drift-notification-btn-primary"
            onClick={handleDismiss}
            autoFocus
          >
            Continue here
          </button>
          <button
            className="drift-notification-btn drift-notification-btn-secondary"
            onClick={handleNewChat}
          >
            Start new chat
          </button>
        </div>
      </div>
    </div>
  );
}

export default DriftNotification;