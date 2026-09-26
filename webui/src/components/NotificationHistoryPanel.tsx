import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNotifications, type Notification } from '../contexts/NotificationContext';
import './NotificationHistoryPanel.css';

interface NotificationHistoryPanelProps {
  anchorRef: React.RefObject<HTMLElement>;
  onClose: () => void;
}

export function formatNotificationAge(createdAt: number, now: number = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - createdAt) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return new Date(createdAt).toLocaleDateString();
}

/**
 * Popover listing the session's notification history, opened from the
 * status-bar bell. Toasts disappear after a few seconds; this is where a
 * missed one can still be read. Opening it marks everything read.
 */
function NotificationHistoryPanel({ anchorRef, onClose }: NotificationHistoryPanelProps): JSX.Element {
  const { notifications, removeNotification, clearNotifications, markAllRead } = useNotifications();
  const panelRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<{ right: number; bottom: number } | null>(null);

  useEffect(() => {
    markAllRead();
  }, [markAllRead]);

  // The status bar clips overflow, so the panel is portalled and placed
  // above the bell with fixed coordinates.
  useLayoutEffect(() => {
    const place = () => {
      const rect = anchorRef.current?.getBoundingClientRect();
      if (!rect) return;
      setPosition({
        right: Math.max(8, window.innerWidth - rect.right),
        bottom: window.innerHeight - rect.top + 4,
      });
    };
    place();
    window.addEventListener('resize', place);
    return () => window.removeEventListener('resize', place);
  }, [anchorRef]);

  useEffect(() => {
    panelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        anchorRef.current?.focus();
      }
    };
    const onPointer = (e: PointerEvent) => {
      const target = e.target as Node;
      if (panelRef.current?.contains(target) || anchorRef.current?.contains(target)) return;
      onClose();
    };
    document.addEventListener('keydown', onKey, true);
    document.addEventListener('pointerdown', onPointer, true);
    return () => {
      document.removeEventListener('keydown', onKey, true);
      document.removeEventListener('pointerdown', onPointer, true);
    };
  }, [anchorRef, onClose]);

  const items: Notification[] = [...notifications].reverse();

  return createPortal(
    <div
      ref={panelRef}
      className="notification-history"
      role="dialog"
      aria-label="Notifications"
      tabIndex={-1}
      style={position ? { right: position.right, bottom: position.bottom } : { visibility: 'hidden' }}
      data-testid="notification-history"
    >
      <div className="notification-history-header">
        <span className="notification-history-title">Notifications</span>
        {items.length > 0 && (
          <button type="button" className="notification-history-clear" onClick={clearNotifications}>
            Clear all
          </button>
        )}
      </div>
      {items.length === 0 ? (
        <p className="notification-history-empty">No notifications yet.</p>
      ) : (
        <ul className="notification-history-list">
          {items.map((n) => (
            <li key={n.id} className={`notification-history-item notification-history-item--${n.type}`}>
              <div className="notification-history-item-body">
                <div className="notification-history-item-title">{n.title}</div>
                {n.message && <div className="notification-history-item-message">{n.message}</div>}
                <div className="notification-history-item-time">{formatNotificationAge(n.createdAt)}</div>
              </div>
              <button
                type="button"
                className="notification-history-item-dismiss"
                onClick={() => removeNotification(n.id)}
                aria-label={`Dismiss ${n.title}`}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>,
    document.body,
  );
}

export default NotificationHistoryPanel;
