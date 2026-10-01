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
const PANEL_WIDTH = 360;
const NARROW_SCREEN_MAX = 600;

function NotificationHistoryPanel({ anchorRef, onClose }: NotificationHistoryPanelProps): JSX.Element {
  const { notifications, removeNotification, clearNotifications, markAllRead } = useNotifications();
  const panelRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<
    { right: number; bottom: number } | { left: number; top: number } | { left: number; bottom: number } | null
  >(null);

  useEffect(() => {
    markAllRead();
  }, [markAllRead]);

  // The status bar clips overflow, so the panel is portalled and placed
  // above the bell with fixed coordinates.
  useLayoutEffect(() => {
    const place = () => {
      const rect = anchorRef.current?.getBoundingClientRect();
      if (!rect) return;
      // A phone has no room beside the anchor (the rail's bell sits in a
      // drawer that closes on open): drop the panel under the top bar.
      if (window.innerWidth <= NARROW_SCREEN_MAX) {
        setPosition({ left: 8, top: 56 });
        return;
      }
      // A rail anchor (left edge) opens beside it — below from the top half,
      // upward from the bottom — kept on screen; the status bar bell opens
      // above.
      if (rect.left < window.innerWidth / 4 && rect.top >= window.innerHeight / 2) {
        const width = Math.min(PANEL_WIDTH, window.innerWidth - 16);
        setPosition({
          left: Math.max(8, Math.min(rect.right + 8, window.innerWidth - width - 8)),
          bottom: Math.max(8, window.innerHeight - rect.bottom),
        });
        return;
      }
      if (rect.top < window.innerHeight / 2) {
        const width = Math.min(PANEL_WIDTH, window.innerWidth - 16);
        setPosition({ left: Math.max(8, Math.min(rect.right + 8, window.innerWidth - width - 8)), top: rect.top });
      } else {
        setPosition({
          right: Math.max(8, window.innerWidth - rect.right),
          bottom: window.innerHeight - rect.top + 4,
        });
      }
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
      style={position ?? { visibility: 'hidden' }}
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
                {n.action && (
                  <button
                    type="button"
                    className="notification-history-item-action"
                    onClick={() => {
                      n.action?.onClick();
                      onClose();
                    }}
                  >
                    {n.action.label}
                  </button>
                )}
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
