import { useEffect, useCallback, useRef } from 'react';
import { X } from 'lucide-react';
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
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Auto-dismiss after 30 seconds
  useEffect(() => {
    const timer = setTimeout(() => {
      onDismiss();
    }, 30000);
    timerRef.current = timer;
    return () => clearTimeout(timer);
  }, [onDismiss]);

  // Handle Escape key to dismiss
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        // Clear the auto-dismiss timer when user dismisses via Escape
        if (timerRef.current) {
          clearTimeout(timerRef.current);
          timerRef.current = null;
        }
        onDismiss();
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onDismiss]);

  const handleDismiss = useCallback(() => {
    // Clear the auto-dismiss timer when user manually dismisses
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    onDismiss();
  }, [onDismiss]);

  const handleNewChat = useCallback(() => {
    // Clear the auto-dismiss timer when user starts new chat
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    onStartNewChat();
  }, [onStartNewChat]);

  const similarityPct = Math.round(similarity * 100);
  const thresholdPct = Math.round(threshold * 100);

  return (
    <div
      className="drift-notification"
      role="alert"
      aria-live="polite"
    >
      <div className="drift-notification-content">
        <button
          type="button"
          className="drift-notification-dismiss"
          onClick={handleDismiss}
          aria-label="Dismiss notification"
          title="Dismiss"
        >
          <X size={16} />
        </button>
        <div className="drift-notification-icon">⚠️</div>
        <div className="drift-notification-text">
          <strong>Conversation drift detected</strong>
          <span className="drift-notification-detail">
            Topic similarity: {similarityPct}% (threshold: {thresholdPct}%)
          </span>
        </div>
        <div className="drift-notification-actions">
          <button
            type="button"
            className="drift-notification-btn drift-notification-btn-primary"
            onClick={handleDismiss}
            autoFocus
          >
            Continue here
          </button>
          <button
            type="button"
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