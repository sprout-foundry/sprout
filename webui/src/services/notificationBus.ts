/**
 * The web UI's notification entry point.
 *
 * Historically this was a straight re-export of `@sprout/ui`'s singleton bus.
 * It is now host-aware (host.7): when an active host is set, a posted
 * notification is routed through that host's `notifications.post` sink, so the
 * host owns where notifications surface. When no host is active (pure service
 * contexts, tests, and the code path before the entry sets the host) it falls
 * back to the underlying in-app bus — today's behavior.
 *
 * The local host's sink IS this in-app bus (see host/localHost.ts), so for local
 * builds a post still reaches the toast stack + NotificationCenter unchanged:
 * there is no double-post, because routing to `localHost.notifications.post`
 * forwards once to the same bus the fallback would have used.
 *
 * Every other bus method (`onNotification`, `markAllRead`, `getNotificationHistory`,
 * the control-event channel, the test reset) is delegated verbatim to the raw
 * bus, so subscribers and existing call sites keep working.
 */

import { notificationBus as rawBus } from '@sprout/ui';
import type { NotificationEvent, NotificationType } from '@sprout/ui';
import { getActiveHost } from '../host/accessor';

export type { NotificationEvent, NotificationType } from '@sprout/ui';

/**
 * A request to post a notification, as the in-app bus accepts it. The host
 * sink carries the same fields (level + title + message + optional
 * duration/action), so routing is a faithful translation.
 */
type NotifyArgs = [
  type: NotificationType,
  title: string,
  message: string,
  duration?: number,
  action?: { label: string; onClick: () => void; keepOpen?: boolean },
];

/**
 * The host-aware notification bus. `notify` is the only method that consults
 * the host; the rest are the raw bus's own methods bound to it, so identity
 * and behavior are unchanged for subscribers.
 */
export const notificationBus: {
  notify(...args: NotifyArgs): string;
  markAllRead(): void;
  onNotification(listener: (event: NotificationEvent) => void): () => void;
  removeNotificationListener(listener: (event: NotificationEvent) => void): void;
  onControlEvent(listener: (event: { kind: string }) => void): () => void;
  removeControlListener(listener: (event: { kind: string }) => void): void;
  getNotificationHistory(): NotificationEvent[];
  _resetForTesting(): void;
} = {
  notify(type, title, message, duration, action) {
    const host = getActiveHost();
    if (host) {
      // The host sink owns notification delivery. The local host forwards to
      // the in-app bus, so local toasts/center are preserved; a host with its
      // own sink (platform, recording test host) receives it instead.
      host.notifications.post({ level: type, title, message, duration, action });
      return '';
    }
    return rawBus.notify(type, title, message, duration, action);
  },
  markAllRead: () => rawBus.markAllRead(),
  onNotification: (listener) => rawBus.onNotification(listener),
  removeNotificationListener: (listener) => rawBus.removeNotificationListener(listener),
  onControlEvent: (listener) => rawBus.onControlEvent(listener),
  removeControlListener: (listener) => rawBus.removeControlListener(listener),
  getNotificationHistory: () => rawBus.getNotificationHistory(),
  _resetForTesting: () => rawBus._resetForTesting(),
};

export type Listener = (event: NotificationEvent) => void;
