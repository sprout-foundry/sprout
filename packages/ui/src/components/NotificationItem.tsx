import { useEffect, useRef, useState, useCallback } from 'react';
import type { NotificationType } from '../contexts/NotificationContext';

export interface NotificationItemProps {
  id: string;
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  onClose: (id: string) => void;
}

const ICONS: Record<NotificationType, string> = {
  info: 'ℹ',
  success: '✓',
  warning: '⚠',
  error: '✕',
};

const DEFAULT_DURATION = 5000; // 5 seconds
const EXIT_ANIMATION_DURATION = 200; // 200ms to match CSS

function NotificationItem({
  id,
  type,
  title,
  message,
  duration = DEFAULT_DURATION,
  onClose,
}: NotificationItemProps): JSX.Element {
  const autoDismissTimerRef = useRef<number | null>(null);
  const [isExiting, setIsExiting] = useState(false);
  const isClosingRef = useRef(false);

  const handleClose = useCallback(() => {
    // Prevent multiple calls
    if (isClosingRef.current) {
      return;
    }
    isClosingRef.current = true;

    // Clear auto-dismiss timer to prevent double-callback
    if (autoDismissTimerRef.current) {
      clearTimeout(autoDismissTimerRef.current);
      autoDismissTimerRef.current = null;
    }

    // Trigger exit animation via React state, then close after animation
    setIsExiting(true);
    setTimeout(() => onClose(id), EXIT_ANIMATION_DURATION);
  }, [id, onClose]);

  // Auto-dismiss after duration
  useEffect(() => {
    if (duration <= 0) return; // No auto-dismiss

    autoDismissTimerRef.current = window.setTimeout(handleClose, duration);

    return () => {
      if (autoDismissTimerRef.current) {
        clearTimeout(autoDismissTimerRef.current);
      }
    };
  }, [duration, handleClose]);

  // Cleanup timeouts on unmount
  useEffect(() => {
    return () => {
      if (autoDismissTimerRef.current) {
        clearTimeout(autoDismissTimerRef.current);
      }
    };
  }, []);

  return (
    <div
      className={`notification-item type-${type}${isExiting ? ' notification-item-exit' : ''}`}
      role="status"
      aria-live="polite"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Escape' || e.key === 'Enter') {
          handleClose();
        }
      }}
    >
      <span className="notification-icon" aria-hidden="true">
        {ICONS[type]}
      </span>
      <div className="notification-content">
        {title && <h4 className="notification-title">{title}</h4>}
        <p className="notification-message">{message}</p>
      </div>
      <button className="notification-dismiss" onClick={handleClose} aria-label="Dismiss notification" type="button">
        ×
      </button>
    </div>
  );
}

export default NotificationItem;
