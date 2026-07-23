import {
  notificationBus,
  NotificationStack,
  type NotificationEvent,
  type NotificationData,
  type NotificationType,
} from '@sprout/ui';
import { useState, useEffect, useRef, useCallback } from 'react';
import './NotificationCenter.css';

/**
 * @deprecated Props kept for backward compatibility with StatusBar.tsx.
 * The NotificationCenter is now a self-contained toast stack that
 * subscribes to notificationBus directly; these props are ignored.
 */
interface NotificationCenterLegacyProps {
  isOpen?: boolean;
  onClose?: () => void;
  positionRef?: React.RefObject<HTMLDivElement>;
}

/**
 * NotificationCenter — top-right toast stack.
 *
 * Subscribes to the singleton notificationBus and renders NotificationStack
 * from @sprout/ui. Auto-dismisses notifications after 5 s when no explicit
 * duration is provided. Positioned top-right via App.css override.
 *
 * Exposes a "Mark all read" affordance that:
 *   1. Calls `notificationBus.markAllRead()` to broadcast the intent.
 *   2. Locally clears every visible toast (with its pending auto-dismiss
 *      timer) so the stack empties immediately.
 */
function NotificationCenter(_props: NotificationCenterLegacyProps = {}): JSX.Element | null {
  const [notifications, setNotifications] = useState<NotificationData[]>([]);
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const removeNotification = useCallback((id: string) => {
    setNotifications((prev) => prev.filter((n) => n.id !== id));
    const timer = timersRef.current.get(id);
    if (timer) {
      clearTimeout(timer);
      timersRef.current.delete(id);
    }
  }, []);

  const clearAll = useCallback(() => {
    setNotifications([]);
    timersRef.current.forEach((timer) => clearTimeout(timer));
    timersRef.current.clear();
  }, []);

  useEffect(() => {
    const handleNotification = (event: NotificationEvent) => {
      const notification: NotificationData = {
        id: event.id,
        type: event.type,
        title: event.title,
        message: event.message,
        duration: event.duration,
        createdAt: Date.now(),
        read: false,
        action: event.action,
      };

      setNotifications((prev) => [...prev, notification]);

      // Auto-dismiss after 5 s only when the bus did NOT provide an explicit duration.
      // Toasts carrying an action with keepOpen=true stay until the user acts or
      // clicks the dismiss button — the auto-dismiss timer is intentionally
      // suppressed for those.
      const suppressAutoDismiss = !!event.action?.keepOpen;
      if (!suppressAutoDismiss && (!event.duration || event.duration === 0)) {
        const timer = setTimeout(() => {
          setNotifications((prev) => prev.filter((n) => n.id !== event.id));
          timersRef.current.delete(event.id);
        }, 5000);
        timersRef.current.set(event.id, timer);
      }
    };

    const handleControl = (event: { kind: string }) => {
      if (event.kind === 'mark_all_read') {
        clearAll();
      }
    };

    const unsubscribe = notificationBus.onNotification(handleNotification);
    const unsubscribeControl = notificationBus.onControlEvent(handleControl);

    return () => {
      unsubscribe();
      unsubscribeControl();
      timersRef.current.forEach((timer) => clearTimeout(timer));
      timersRef.current.clear();
    };
  }, [clearAll]);

  const handleMarkAllRead = useCallback(() => {
    notificationBus.markAllRead();
    clearAll();
  }, [clearAll]);

  if (notifications.length === 0) {
    return null;
  }

  return (
    <div className="notification-center-wrapper" data-testid="notification-center">
      <div className="notification-center-header">
        <button
          type="button"
          className="notification-center-mark-all-read"
          onClick={handleMarkAllRead}
          aria-label="Mark all notifications read"
          data-testid="notification-center-mark-all-read"
        >
          Mark all read
        </button>
      </div>
      <NotificationStack notifications={notifications} onDismiss={removeNotification} />
    </div>
  );
}

/**
 * Publish a system notification using semantic categories.
 *
 * Maps internal event categories to notification types:
 *   rate_limit           → warning
 *   auth_failure         → error
 *   permission_required  → warning
 *   agent_blocked        → error
 *   (unrecognized)       → info
 */
export function publishSystemNotification(category: string, title: string, message: string): void {
  const typeMap: Record<string, NotificationType> = {
    rate_limit: 'warning',
    auth_failure: 'error',
    permission_required: 'warning',
    agent_blocked: 'error',
  };
  const type = typeMap[category] ?? 'info';
  notificationBus.notify(type, title, message);
}

export default NotificationCenter;
