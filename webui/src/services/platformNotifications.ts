/**
 * The account's notifications — a task finished or failed, usage crossed a
 * threshold, a payment failed — shown in the hosted editor. The platform app
 * lists them under its own bell, which isn't on screen while you work here,
 * so the editor raises each new one once (toast and Activity) and marks it
 * read on the platform.
 */

import { platformHref } from '../utils/platformUrl';
import { openPlatformPage } from './homeView';
import { notificationBus, type NotificationType } from './notificationBus';

const POLL_MS = 60_000;

interface PlatformNotification {
  id: string;
  type: string;
  title: string;
  message: string;
  severity: string;
  action_url?: string;
  action_label?: string;
  read_at?: string | null;
  resolved_at?: string | null;
}

function busType(n: PlatformNotification): NotificationType {
  if (n.severity === 'error') return 'error';
  if (n.severity === 'warning') return 'warning';
  return n.type === 'task_completed' ? 'success' : 'info';
}

function openAction(url: string): void {
  if (/^https?:\/\//.test(url)) {
    window.open(url, '_blank', 'noopener');
    return;
  }
  if (!openPlatformPage(url)) window.location.href = platformHref(`/#${url}`);
}

export async function pollPlatformNotifications(raised: Set<string>): Promise<void> {
  const res = await fetch(platformHref('/notifications'), { credentials: 'include' });
  if (!res.ok) return;
  const body = (await res.json()) as { notifications?: PlatformNotification[] };
  for (const n of body.notifications ?? []) {
    // The platform keeps one row per kind and refreshes it for each new
    // event (a second finished task reuses the row), so the text is part of
    // what makes it new.
    const key = `${n.id}\n${n.title}\n${n.message}`;
    if (n.read_at || n.resolved_at || raised.has(key)) continue;
    raised.add(key);
    const url = n.action_url;
    notificationBus.notify(
      busType(n),
      n.title,
      n.message,
      undefined,
      url ? { label: n.action_label || 'View', onClick: () => openAction(url) } : undefined,
    );
    void fetch(platformHref(`/notifications/${encodeURIComponent(n.id)}/read`), {
      method: 'POST',
      credentials: 'include',
    }).catch(() => undefined);
  }
}

/** Polls while the tab is visible, and on focus. Returns a stop function. */
export function startPlatformNotifications(): () => void {
  const raised = new Set<string>();
  const poll = () => {
    if (document.visibilityState === 'visible') void pollPlatformNotifications(raised).catch(() => undefined);
  };
  poll();
  const timer = window.setInterval(poll, POLL_MS);
  window.addEventListener('focus', poll);
  return () => {
    window.clearInterval(timer);
    window.removeEventListener('focus', poll);
  };
}
