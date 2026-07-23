import type { NotificationAction, NotificationType } from '../types/notification';

export type { NotificationType, NotificationAction };

export interface NotificationEvent {
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  id: string;
  action?: NotificationAction;
}

/**
 * Bus-level control events. These are separate from user-facing
 * notifications so subscribers can react without rendering a toast.
 * `kind: 'mark_all_read'` is the canonical event for "user wants to
 * acknowledge every visible notification now"; consumers like
 * NotificationCenter translate it into dismissing their toast stack.
 */
export type BusControlEvent =
  | { kind: 'mark_all_read' };

export type Listener = (event: NotificationEvent) => void;
export type ControlListener = (event: BusControlEvent) => void;

class NotificationBus {
  private listeners: Listener[] = [];
  private controlListeners: ControlListener[] = [];
  private history: NotificationEvent[] = [];
  private nextId = 0;

  notify(
    type: NotificationType,
    title: string,
    message: string,
    duration?: number,
    action?: NotificationAction,
  ): string {
    const id = `notify_${this.nextId++}_${Date.now()}`;
    const event: NotificationEvent = { type, title, message, duration, id, action };

    // Log to console based on type
    switch (type) {
      case 'error':
        // eslint-disable-next-line no-console
        console.error(`[Notification] ${title}: ${message}`);
        break;
      case 'warning':
        // eslint-disable-next-line no-console
        console.warn(`[Notification] ${title}: ${message}`);
        break;
      case 'info':
        // eslint-disable-next-line no-console
        console.info(`[Notification] ${title}: ${message}`);
        break;
      case 'success':
        // eslint-disable-next-line no-console
        console.log(`[Notification] ${title}: ${message}`);
        break;
    }

    // Emit to listeners
    this.listeners.forEach((listener) => listener(event));

    // Store in history
    this.history.push(event);
    if (this.history.length > 100) {
      this.history.shift();
    }

    return id;
  }

  /**
   * Broadcast a "mark all read" signal. Subscribers that manage their
   * own notification UI (e.g. the NotificationCenter toast stack) use
   * this to dismiss every visible notification in one shot.
   */
  markAllRead(): void {
    this.controlListeners.forEach((listener) => listener({ kind: 'mark_all_read' }));
  }

  onNotification(listener: Listener): () => void {
    this.listeners.push(listener);
    return () => this.removeNotificationListener(listener);
  }

  removeNotificationListener(listener: Listener): void {
    this.listeners = this.listeners.filter((l) => l !== listener);
  }

  onControlEvent(listener: ControlListener): () => void {
    this.controlListeners.push(listener);
    return () => this.removeControlListener(listener);
  }

  removeControlListener(listener: ControlListener): void {
    this.controlListeners = this.controlListeners.filter((l) => l !== listener);
  }

  getNotificationHistory(): NotificationEvent[] {
    return [...this.history];
  }

  /** @internal Resets all state. Only for use in tests. */
  _resetForTesting(): void {
    this.listeners = [];
    this.controlListeners = [];
    this.history = [];
    this.nextId = 0;
  }
}

// Singleton instance
export const notificationBus = new NotificationBus();
