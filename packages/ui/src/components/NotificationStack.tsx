import type { ReactNode } from 'react';
import type { NotificationType } from '../contexts/NotificationContext';
import NotificationItem from './NotificationItem';
import './NotificationStack.css';

import type { NotificationData } from '../types/notification';

export interface NotificationStackProps {
  notifications: NotificationData[];
  onDismiss: (id: string) => void;
  className?: string;
}

/**
 * Notification container component that renders all active notifications.
 *
 * Can be used in two ways:
 * 1. Props-based: Pass notifications array and onDismiss callback
 * 2. Context-based: Use NotificationProvider and useNotifications hook (imports NotificationStack internally)
 *
 * Notifications are stacked in a fixed position at the bottom-right of the viewport.
 */
function NotificationStack({
  notifications,
  onDismiss,
  className,
}: NotificationStackProps): JSX.Element | null {
  if (notifications.length === 0) {
    return null;
  }

  const containerClassName = className ? `notification-container ${className}` : 'notification-container';

  return (
    <div className={containerClassName} role="region" aria-label="Notifications">
      {notifications.map((notification) => (
        <NotificationItem
          key={notification.id}
          id={notification.id}
          type={notification.type}
          title={notification.title}
          message={notification.message}
          duration={notification.duration}
          action={notification.action}
          onClose={onDismiss}
        />
      ))}
    </div>
  );
}

export default NotificationStack;
