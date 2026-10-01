import { useNotifications } from '../contexts/NotificationContext';

/** Unread Activity entries, for badges on the places that open Activity. */
export function useUnreadNotificationCount(): number {
  return useNotifications().notifications.filter((n) => !n.read).length;
}
