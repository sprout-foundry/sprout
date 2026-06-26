/**
 * Desktop notification service using the browser Notification API.
 * Notifications only fire when the tab is backgrounded (document.hidden)
 * to avoid spamming the user when they are already looking at the app.
 */

type NotificationPermission = 'default' | 'granted' | 'denied';

// Internal state
let isEnabled = true; // controlled by settings UI toggle

/**
 * Get the current notification permission status.
 */
export function getPermission(): NotificationPermission {
  if (typeof Notification === 'undefined') return 'denied';
  return Notification.permission;
}

/**
 * Request notification permission from the user.
 * Returns a promise that resolves with the resulting permission.
 */
export async function requestPermission(): Promise<NotificationPermission> {
  if (typeof Notification === 'undefined') return 'denied';
  if (Notification.permission === 'granted') return 'granted';
  if (Notification.permission === 'denied') return 'denied';

  const perm = await Notification.requestPermission();
  return perm;
}

/**
 * Enable or disable notifications (controlled by settings toggle).
 */
export function setEnabled(value: boolean): void {
  isEnabled = value;
}

/**
 * Check if notifications are enabled.
 * Named isEnabled_ to avoid collision with the parameter name pattern.
 */
export function isEnabled_(): boolean {
  return isEnabled;
}

/**
 * Fire a browser notification.
 * Only fires if permission is 'granted' and notifications are enabled.
 * Clicking the notification focuses the window.
 */
export function notify(title: string, body?: string): void {
  if (!isEnabled) return;
  if (typeof Notification === 'undefined') return;
  if (Notification.permission !== 'granted') return;

  const notification = new Notification(title, {
    body: body || undefined,
    icon: '/favicon.ico',
    tag: 'sprout-notification',
  });

  notification.onclick = () => {
    window.focus();
  };
}

/**
 * Fire a notification ONLY when the tab is backgrounded (document.hidden is true).
 * This is the primary method used by the event handler — it avoids spamming
 * the user when they're already looking at the app.
 */
export function notifyIfHidden(title: string, body?: string): void {
  if (!isEnabled) return;
  if (!document.hidden) return;
  notify(title, body);
}
