import { useEffect, useRef, useCallback } from 'react';
import type { NotificationType } from '../contexts/NotificationContext';
import type { NotificationAction } from '../types/notification';

export interface NotificationItemProps {
  id: string;
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  action?: NotificationAction;
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
  action,
  onClose,
}: NotificationItemProps): JSX.Element {
  const exitAnimationRef = useRef<number | null>(null);
  const autoDismissTimerRef = useRef<number | null>(null);
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

    // Clear any existing exit animation timeout
    if (exitAnimationRef.current) {
      clearTimeout(exitAnimationRef.current);
    }

    const element = document.getElementById(`notification-${id}`);
    if (element && !element.classList.contains('notification-item-exit')) {
      element.classList.add('notification-item-exit');
      exitAnimationRef.current = window.setTimeout(() => {
        onClose(id);
        exitAnimationRef.current = null;
      }, EXIT_ANIMATION_DURATION);
    } else {
      onClose(id);
    }
  }, [id, onClose]);

  // Auto-dismiss after duration. When the notification carries an action
  // with keepOpen=true, suppress auto-dismiss entirely (the toast stays
  // until the user clicks the action or the dismiss button). With keepOpen,
  // an explicit duration still acts as a hard timeout so the toast never
  // gets stuck, but the 5 s default is not applied.
  useEffect(() => {
    if (duration <= 0) return; // No auto-dismiss

    if (action?.keepOpen) {
      // keepOpen + no explicit duration → toast sticks around until the
      // user acts. The parent NotificationCenter also suppresses its own
      // 5 s default timer when keepOpen is set, so this is the only
      // timer path left, and it skips when duration falls back to default.
      if (duration === DEFAULT_DURATION) return;
    }

    autoDismissTimerRef.current = window.setTimeout(handleClose, duration);

    return () => {
      if (autoDismissTimerRef.current) {
        clearTimeout(autoDismissTimerRef.current);
      }
    };
  }, [duration, handleClose, action?.keepOpen]);
  useEffect(() => {
    return () => {
      if (exitAnimationRef.current) {
        clearTimeout(exitAnimationRef.current);
      }
      if (autoDismissTimerRef.current) {
        clearTimeout(autoDismissTimerRef.current);
      }
    };
  }, []);

  return (
    <div
      id={`notification-${id}`}
      className={`notification-item type-${type}`}
      role="alert"
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
        {action && (
          <button
            type="button"
            className="notification-action"
            onClick={(e) => {
              e.stopPropagation();
              action.onClick();
              if (!action.keepOpen) {
                handleClose();
              }
            }}
          >
            {action.label}
          </button>
        )}
      </div>
      <button className="notification-dismiss" onClick={handleClose} aria-label="Dismiss notification" type="button">
        ×
      </button>
    </div>
  );
}

export default NotificationItem;
