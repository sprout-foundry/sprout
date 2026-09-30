import { useNotifications, type Notification } from '../contexts/NotificationContext';

/** Unread Activity entries, for badges on the places that open Activity. */
export function useUnreadNotificationCount(): number {
  let notifications: Notification[] = [];
  try {
    notifications = useNotifications().notifications;
  } catch {
    // Rendered without the provider (isolated component tests): no badge.
  }
  return notifications.filter((n) => !n.read).length;
}
