import type { NotificationType } from '../contexts/NotificationContext';

export interface NotificationEvent {
  type: NotificationType;
  title: string;
  message: string;
  duration?: number;
  id: string;
}

type Listener = (event: NotificationEvent) => void;

class NotificationBus {
  private listeners: Listener[] = [];
  private history: NotificationEvent[] = [];
  private nextId = 0;

  notify(type: NotificationType, title: string, message: string, duration?: number): void {
    const id = `notify_${this.nextId++}_${Date.now()}`;
    const event: NotificationEvent = { type, title, message, duration, id };

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
  }

  onNotification(listener: Listener): () => void {
    this.listeners.push(listener);
    return () => this.removeNotificationListener(listener);
  }

  removeNotificationListener(listener: Listener): void {
    this.listeners = this.listeners.filter((l) => l !== listener);
  }

  getNotificationHistory(): NotificationEvent[] {
    return [...this.history];
  }

  /** @internal Resets all state. Only for use in tests. */
  _resetForTesting(): void {
    this.listeners = [];
    this.history = [];
    this.nextId = 0;
  }
}

// Singleton instance
export const notificationBus = new NotificationBus();
