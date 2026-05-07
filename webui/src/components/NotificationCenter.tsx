import { useState, useEffect, useRef, useCallback } from 'react';
import { useNotifications } from '../contexts/NotificationContext';
import './NotificationCenter.css';

interface NotificationCenterProps {
  isOpen: boolean;
  onClose: () => void;
  positionRef?: React.RefObject<HTMLDivElement>;
}

const NOTIFICATION_ICONS: Record<string, string> = {
  info: 'ℹ',
  success: '✓',
  warning: '⚠',
  error: '✕',
};

function getNotificationIcon(type: string): string {
  return NOTIFICATION_ICONS[type] ?? 'ℹ';
}

/**
 * NotificationCenter component that displays notification history in a panel.
 *
 * Features:
 * - Shows all notifications with type icon, title, message, and timestamp
 * - Actions per notification: dismiss, copy message
 * - "Dismiss All" button to clear all notifications
 * - Relative timestamps (e.g., "2m ago", "1h ago")
 * - Close on Escape key and outside click
 * - Copied feedback state for copy button
 */
function NotificationCenter({ isOpen, onClose, positionRef }: NotificationCenterProps): JSX.Element | null {
  const { notifications, removeNotification, clearNotifications } = useNotifications();
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Handle copy message to clipboard
  const handleCopyMessage = useCallback(async (id: string, message: string) => {
    try {
      await navigator.clipboard.writeText(message);
      setCopiedId(id);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setCopiedId(null), 2000);
    } catch (error) {
      console.error('Failed to copy notification message:', error);
    }
  }, []);

  // Handle dismiss individual notification
  const handleDismiss = useCallback((id: string) => {
    removeNotification(id);
  }, [removeNotification]);

  // Handle dismiss all notifications
  const handleDismissAll = useCallback(() => {
    clearNotifications();
  }, [clearNotifications]);

  // Cleanup copy timer on unmount
  useEffect(() => {
    return () => {
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
    };
  }, []);

  // Format relative time — guard against negative (future) timestamps
  const formatRelativeTime = useCallback((timestamp: number): string => {
    const now = Date.now();
    const diff = Math.max(0, now - timestamp);
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (seconds < 10) return 'just now';
    if (seconds < 60) return `${seconds}s ago`;
    if (minutes < 60) return `${minutes}m ago`;
    if (hours < 24) return `${hours}h ago`;
    return `${days}d ago`;
  }, []);

  // Close on Escape key
  useEffect(() => {
    if (!isOpen) return;

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };

    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, onClose]);

  // Close on outside click
  useEffect(() => {
    if (!isOpen) return;

    const handleClickOutside = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        // Check if clicked on the bell icon (if positionRef is provided)
        if (positionRef?.current?.contains(e.target as Node)) {
          return;
        }
        onClose();
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen, onClose, positionRef]);

  if (!isOpen) {
    return null;
  }

  return (
    <div
      ref={panelRef}
      className="notification-center"
      role="dialog"
      aria-label="Notification center"
      aria-modal="false"
    >
      {/* Header */}
      <div className="notification-center-header">
        <h2 className="notification-center-title">Notifications</h2>
        {notifications.length > 0 && (
          <button
            className="notification-center-dismiss-all"
            onClick={handleDismissAll}
            type="button"
          >
            Dismiss All
          </button>
        )}
      </div>

      {/* Content */}
      <div className="notification-center-content">
        {notifications.length === 0 ? (
          <div className="notification-center-empty">
            <div className="notification-center-empty-icon" aria-hidden="true">🔔</div>
            <p className="notification-center-empty-text">No notifications</p>
          </div>
        ) : (
          <ul className="notification-center-list" role="list">
            {[...notifications].reverse().map((notification) => (
              <li
                key={notification.id}
                className={`notification-center-item type-${notification.type}`}
              >
                {/* Type icon */}
                <span className="notification-center-item-icon" aria-hidden="true">
                  {getNotificationIcon(notification.type)}
                </span>

                {/* Content */}
                <div className="notification-center-item-content">
                  <div className="notification-center-item-header">
                    <h3 className="notification-center-item-title">{notification.title}</h3>
                    <span className="notification-center-item-time">
                      {formatRelativeTime(notification.createdAt)}
                    </span>
                  </div>
                  <p className="notification-center-item-message">{notification.message}</p>
                </div>

                {/* Actions */}
                <div className="notification-center-item-actions">
                  <button
                    className="notification-center-item-copy"
                    onClick={() => handleCopyMessage(notification.id, notification.message)}
                    type="button"
                    aria-label="Copy message"
                    title="Copy message"
                  >
                    {copiedId === notification.id ? 'Copied!' : '📋'}
                  </button>
                  <button
                    className="notification-center-item-dismiss"
                    onClick={() => handleDismiss(notification.id)}
                    type="button"
                    aria-label="Dismiss notification"
                    title="Dismiss"
                  >
                    ×
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

export default NotificationCenter;
