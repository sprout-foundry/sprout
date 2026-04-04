import type { FC } from 'react';
import { useNotifications } from '../contexts/NotificationContext';
import NotificationItem from './NotificationItem';
import './Notification.css';

/**
 * Notification container component that renders all active notifications.
 *
 * Subscribes to the NotificationContext and renders each notification
 * as a NotificationItem component. Notifications are stacked in a fixed
 * position at the bottom-right of the viewport.
 */
const Notification: FC = () => {
  const { notifications, removeNotification } = useNotifications();

  if (notifications.length === 0) {
    return null;
  }

  return (
    <div className="notification-container" role="region" aria-label="Notifications">
      {notifications.map((notification) => (
        <NotificationItem
          key={notification.id}
          id={notification.id}
          type={notification.type}
          title={notification.title}
          message={notification.message}
          duration={notification.duration}
          onClose={removeNotification}
        />
      ))}
    </div>
  );
};

export default Notification;
