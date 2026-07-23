import { vi } from 'vitest';
import { notificationBus } from './notificationBus';

describe('notificationBus', () => {
  let listeners: Array<ReturnType<typeof vi.fn>>;

  beforeEach(() => {
    listeners = [];
    // Reset bus state for testing
    (notificationBus as any)._resetForTesting();
  });

  afterEach(() => {
    (notificationBus as any)._resetForTesting();
  });

  const createListener = () => {
    const listener = vi.fn();
    listeners.push(listener);
    return listener;
  };

  describe('notify', () => {
    it('emits event to all registered listeners', () => {
      const listener1 = createListener();
      const listener2 = createListener();

      notificationBus.onNotification(listener1);
      notificationBus.onNotification(listener2);

      notificationBus.notify('info', 'Title', 'Message');

      expect(listener1).toHaveBeenCalledTimes(1);
      expect(listener2).toHaveBeenCalledTimes(1);
    });

    it('emits event with correct data', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('error', 'Error Title', 'Error message', 5000);

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'error',
          title: 'Error Title',
          message: 'Error message',
          duration: 5000,
        })
      );
    });

    it('generates unique IDs for each notification', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title 1', 'Message 1');
      notificationBus.notify('info', 'Title 2', 'Message 2');

      const calls = listener.mock.calls;
      const id1 = calls[0][0].id;
      const id2 = calls[1][0].id;

      expect(id1).not.toBe(id2);
    });

    it('defaults duration when not provided', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          duration: undefined,
        })
      );
    });

    it('logs to console based on type', () => {
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
      const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const consoleInfoSpy = vi.spyOn(console, 'info').mockImplementation(() => {});
      const consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      notificationBus.notify('error', 'Error', 'Error message');
      expect(consoleErrorSpy).toHaveBeenCalledWith('[Notification] Error: Error message');

      notificationBus.notify('warning', 'Warning', 'Warning message');
      expect(consoleWarnSpy).toHaveBeenCalledWith('[Notification] Warning: Warning message');

      notificationBus.notify('info', 'Info', 'Info message');
      expect(consoleInfoSpy).toHaveBeenCalledWith('[Notification] Info: Info message');

      notificationBus.notify('success', 'Success', 'Success message');
      expect(consoleLogSpy).toHaveBeenCalledWith('[Notification] Success: Success message');

      consoleErrorSpy.mockRestore();
      consoleWarnSpy.mockRestore();
      consoleInfoSpy.mockRestore();
      consoleLogSpy.mockRestore();
    });
  });

  describe('onNotification', () => {
    it('returns unsubscribe function', () => {
      const listener = createListener();
      const unsubscribe = notificationBus.onNotification(listener);

      expect(typeof unsubscribe).toBe('function');
    });

    it('stops calling listener after unsubscribe', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message 1');
      expect(listener).toHaveBeenCalledTimes(1);

      const unsubscribe = notificationBus.onNotification(listener);
      unsubscribe();

      notificationBus.notify('info', 'Title', 'Message 2');
      expect(listener).toHaveBeenCalledTimes(1); // Still 1, not 2
    });

    it('does not affect other listeners when one unsubscribes', () => {
      const listener1 = createListener();
      const listener2 = createListener();

      const unsubscribe1 = notificationBus.onNotification(listener1);
      notificationBus.onNotification(listener2);

      unsubscribe1();

      notificationBus.notify('info', 'Title', 'Message');

      expect(listener1).not.toHaveBeenCalled();
      expect(listener2).toHaveBeenCalledTimes(1);
    });
  });

  describe('removeNotificationListener', () => {
    it('removes specific listener', () => {
      const listener1 = createListener();
      const listener2 = createListener();

      notificationBus.onNotification(listener1);
      notificationBus.onNotification(listener2);

      notificationBus.removeNotificationListener(listener1);

      notificationBus.notify('info', 'Title', 'Message');

      expect(listener1).not.toHaveBeenCalled();
      expect(listener2).toHaveBeenCalledTimes(1);
    });

    it('does not throw when removing non-existent listener', () => {
      const listener = createListener();

      expect(() => {
        notificationBus.removeNotificationListener(listener);
      }).not.toThrow();
    });
  });

  describe('getNotificationHistory', () => {
    it('returns empty array initially', () => {
      const history = notificationBus.getNotificationHistory();
      expect(history).toEqual([]);
    });

    it('stores notifications in history', () => {
      notificationBus.notify('info', 'Title 1', 'Message 1');
      notificationBus.notify('error', 'Title 2', 'Message 2');

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(2);
    });

    it('stores notifications in order', () => {
      notificationBus.notify('info', 'Title 1', 'Message 1');
      notificationBus.notify('error', 'Title 2', 'Message 2');

      const history = notificationBus.getNotificationHistory();
      expect(history[0].title).toBe('Title 1');
      expect(history[1].title).toBe('Title 2');
    });

    it('returns a copy of history (not reference)', () => {
      notificationBus.notify('info', 'Title', 'Message');

      const history1 = notificationBus.getNotificationHistory();
      const history2 = notificationBus.getNotificationHistory();

      expect(history1).not.toBe(history2);
      expect(history1).toEqual(history2);
    });
  });

  describe('history limit', () => {
    it('limits history to 100 notifications', () => {
      for (let i = 0; i < 150; i++) {
        notificationBus.notify('info', `Title ${i}`, `Message ${i}`);
      }

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(100);
    });

    it('keeps most recent notifications', () => {
      for (let i = 0; i < 150; i++) {
        notificationBus.notify('info', `Title ${i}`, `Message ${i}`);
      }

      const history = notificationBus.getNotificationHistory();
      // Should keep last 100 (50-149)
      expect(history[0].title).toBe('Title 50');
      expect(history[99].title).toBe('Title 149');
    });
  });

  describe('action threading', () => {
    it('forwards an action to listeners when provided as the 5th argument', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      const onClick = vi.fn();
      notificationBus.notify('warning', 'Configure', 'Provider missing', 5000, {
        label: 'Open settings',
        onClick,
      });

      expect(listener).toHaveBeenCalledTimes(1);
      const event = listener.mock.calls[0][0];
      expect(event.action).toBeDefined();
      expect(event.action?.label).toBe('Open settings');
      expect(event.action?.onClick).toBe(onClick);
      expect(event.action?.keepOpen).toBeUndefined();
    });

    it('preserves the keepOpen flag on the action', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Important', 'Read this', undefined, {
        label: 'Acknowledge',
        onClick: vi.fn(),
        keepOpen: true,
      });

      const event = listener.mock.calls[0][0];
      expect(event.action?.keepOpen).toBe(true);
    });

    it('leaves action undefined when no action is provided (backwards compat)', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message');

      const event = listener.mock.calls[0][0];
      expect(event.action).toBeUndefined();
    });

    it('supports the legacy 4-argument signature without an action', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      // Old call sites pass only type/title/message/duration. The bus
      // must keep that working — action defaults to undefined.
      notificationBus.notify('error', 'Error', 'msg', 3000);

      const event = listener.mock.calls[0][0];
      expect(event.action).toBeUndefined();
      expect(event.duration).toBe(3000);
    });

    it('stores the action in the notification history', () => {
      notificationBus.notify('info', 'Important', 'Read this', undefined, {
        label: 'Acknowledge',
        onClick: vi.fn(),
      });

      const history = notificationBus.getNotificationHistory();
      expect(history[0].action?.label).toBe('Acknowledge');
    });

    it('invokes the action callback exactly when the listener decides to', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      const onClick = vi.fn();
      notificationBus.notify('info', 'T', 'M', undefined, { label: 'X', onClick });

      // The bus does NOT auto-invoke onClick — listeners (and ultimately
      // the toast item) decide when to fire it. This proves the bus is
      // purely a transport.
      expect(onClick).not.toHaveBeenCalled();

      const event = listener.mock.calls[0][0];
      event.action?.onClick();
      expect(onClick).toHaveBeenCalledTimes(1);
    });
  });

  describe('multiple listeners', () => {
    it('all listeners receive events', () => {
      const listeners: Array<ReturnType<typeof vi.fn>> = [];
      for (let i = 0; i < 10; i++) {
        const listener = createListener();
        listeners.push(listener);
        notificationBus.onNotification(listener);
      }

      notificationBus.notify('info', 'Title', 'Message');

      listeners.forEach(listener => {
        expect(listener).toHaveBeenCalledTimes(1);
      });
    });

    it('listeners receive same event object', () => {
      const listener1 = createListener();
      const listener2 = createListener();

      notificationBus.onNotification(listener1);
      notificationBus.onNotification(listener2);

      notificationBus.notify('info', 'Title', 'Message');

      const event1 = listener1.mock.calls[0][0];
      const event2 = listener2.mock.calls[0][0];

      expect(event1.id).toBe(event2.id);
      expect(event1.type).toBe(event2.type);
      expect(event1.title).toBe(event2.title);
      expect(event1.message).toBe(event2.message);
    });
  });

  describe('notification types', () => {
    it('supports info type', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'info' })
      );
    });

    it('supports success type', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('success', 'Title', 'Message');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'success' })
      );
    });

    it('supports warning type', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('warning', 'Title', 'Message');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'warning' })
      );
    });

    it('supports error type', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('error', 'Title', 'Message');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'error' })
      );
    });
  });

  describe('event data structure', () => {
    it('includes required fields', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message', 5000);

      const event = listener.mock.calls[0][0];

      expect(event).toHaveProperty('type');
      expect(event).toHaveProperty('title');
      expect(event).toHaveProperty('message');
      expect(event).toHaveProperty('id');
      expect(event).toHaveProperty('duration');
    });

    it('has string id', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message');

      const event = listener.mock.calls[0][0];
      expect(typeof event.id).toBe('string');
    });

    it('id follows pattern', () => {
      const listener = createListener();
      notificationBus.onNotification(listener);

      notificationBus.notify('info', 'Title', 'Message');

      const event = listener.mock.calls[0][0];
      expect(event.id).toMatch(/^notify_\d+_\d+$/);
    });
  });

  describe('markAllRead', () => {
    it('emits mark_all_read to all control listeners', () => {
      const listener1 = vi.fn();
      const listener2 = vi.fn();

      notificationBus.onControlEvent(listener1);
      notificationBus.onControlEvent(listener2);

      notificationBus.markAllRead();

      expect(listener1).toHaveBeenCalledWith({ kind: 'mark_all_read' });
      expect(listener2).toHaveBeenCalledWith({ kind: 'mark_all_read' });
    });

    it('does not emit mark_all_read to notification listeners', () => {
      const notificationListener = vi.fn();
      notificationBus.onNotification(notificationListener);

      notificationBus.markAllRead();

      expect(notificationListener).not.toHaveBeenCalled();
    });

    it('returns unsubscribe from onControlEvent', () => {
      const listener = vi.fn();
      const unsubscribe = notificationBus.onControlEvent(listener);

      expect(typeof unsubscribe).toBe('function');

      notificationBus.markAllRead();
      expect(listener).toHaveBeenCalledTimes(1);

      unsubscribe();
      notificationBus.markAllRead();
      expect(listener).toHaveBeenCalledTimes(1);
    });
  });
});
