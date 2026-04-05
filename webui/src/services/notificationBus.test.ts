import { notificationBus } from './notificationBus';
import type { NotificationEvent } from './notificationBus';
import type { NotificationType } from '../contexts/NotificationContext';

// ---------------------------------------------------------------------------
// Setup / Teardown
// ---------------------------------------------------------------------------

beforeAll(() => {
  jest.spyOn(console, 'error').mockImplementation(() => {});
  jest.spyOn(console, 'warn').mockImplementation(() => {});
  jest.spyOn(console, 'info').mockImplementation(() => {});
  jest.spyOn(console, 'log').mockImplementation(() => {});
});

afterAll(() => {
  jest.restoreAllMocks();
});

beforeEach(() => {
  notificationBus._resetForTesting();
  jest.clearAllMocks();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('NotificationBus', () => {
  // =========================================================================
  // notify() — console logging
  // =========================================================================
  describe('notify() — console logging', () => {
    it('calls console.error for type "error"', () => {
      notificationBus.notify('error', 'Oops', 'Something went wrong');
      expect(console.error).toHaveBeenCalledWith('[Notification] Oops: Something went wrong');
    });

    it('calls console.warn for type "warning"', () => {
      notificationBus.notify('warning', 'Heads up', 'Capacity at 90%');
      expect(console.warn).toHaveBeenCalledWith('[Notification] Heads up: Capacity at 90%');
    });

    it('calls console.info for type "info"', () => {
      notificationBus.notify('info', 'FYI', 'Build completed');
      expect(console.info).toHaveBeenCalledWith('[Notification] FYI: Build completed');
    });

    it('calls console.log for type "success"', () => {
      notificationBus.notify('success', 'Done', 'File saved');
      expect(console.log).toHaveBeenCalledWith('[Notification] Done: File saved');
    });

    it('does not call unrelated console methods when emitting an error', () => {
      notificationBus.notify('error', 'E', 'msg');
      expect(console.warn).not.toHaveBeenCalled();
      expect(console.info).not.toHaveBeenCalled();
    });

    it('all four NotificationType values produce correct console calls', () => {
      const mapping: Array<[NotificationType, keyof typeof console]> = [
        ['error', 'error'],
        ['warning', 'warn'],
        ['info', 'info'],
        ['success', 'log'],
      ] as const;

      for (const [type, consoleMethod] of mapping) {
        notificationBus._resetForTesting();
        jest.clearAllMocks();

        notificationBus.notify(type, 'T', 'M');
        expect(console[consoleMethod]).toHaveBeenCalledWith('[Notification] T: M');
      }
    });
  });

  // =========================================================================
  // notify() — listener emission
  // =========================================================================
  describe('notify() — listener emission', () => {
    it('emits an event to a single subscriber', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'Title', 'Hello');

      expect(handler).toHaveBeenCalledTimes(1);
      const event = handler.mock.calls[0][0] as NotificationEvent;
      expect(event.type).toBe('info');
      expect(event.title).toBe('Title');
      expect(event.message).toBe('Hello');
      expect(event.duration).toBeUndefined();
    });

    it('emits to multiple subscribers', () => {
      const handler1 = jest.fn();
      const handler2 = jest.fn();
      notificationBus.onNotification(handler1);
      notificationBus.onNotification(handler2);

      notificationBus.notify('success', 'Win', 'Test passed');

      expect(handler1).toHaveBeenCalledTimes(1);
      expect(handler2).toHaveBeenCalledTimes(1);
    });

    it('passes duration when provided', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('warning', 'T', 'M', 5000);

      const event = handler.mock.calls[0][0] as NotificationEvent;
      expect(event.duration).toBe(5000);
    });

    it('does not emit to a listener that was added after notify()', () => {
      const handler1 = jest.fn();
      const handler2 = jest.fn();

      notificationBus.onNotification(handler1);
      notificationBus.notify('info', 'First', 'Only handler1');
      notificationBus.onNotification(handler2);
      notificationBus.notify('info', 'Second', 'Both handlers');

      expect(handler1).toHaveBeenCalledTimes(2);
      expect(handler2).toHaveBeenCalledTimes(1);
    });
  });

  // =========================================================================
  // Event IDs
  // =========================================================================
  describe('event IDs', () => {
    it('generates an id for each notification', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'T', 'M');
      const event = handler.mock.calls[0][0] as NotificationEvent;

      expect(event.id).toBeDefined();
      expect(typeof event.id).toBe('string');
    });

    it('generates unique IDs across notifications', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'A', 'a');
      notificationBus.notify('info', 'B', 'b');

      const id1 = (handler.mock.calls[0][0] as NotificationEvent).id;
      const id2 = (handler.mock.calls[1][0] as NotificationEvent).id;

      expect(id1).not.toBe(id2);
    });

    it('IDs follow the pattern notify_{counter}_{timestamp}', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'T', 'M');
      const event = handler.mock.calls[0][0] as NotificationEvent;

      expect(event.id).toMatch(/^notify_\d+_\d+$/);
    });

    it('increments the counter for each notification', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'A', 'a');
      notificationBus.notify('info', 'B', 'b');

      const id1 = (handler.mock.calls[0][0] as NotificationEvent).id;
      const id2 = (handler.mock.calls[1][0] as NotificationEvent).id;

      // Extract counter (the number between "notify_" and the timestamp)
      const counter1 = parseInt(id1.split('_')[1], 10);
      const counter2 = parseInt(id2.split('_')[1], 10);

      expect(counter2).toBe(counter1 + 1);
    });
  });

  // =========================================================================
  // onNotification() — unsubscribe via returned function
  // =========================================================================
  describe('onNotification() — unsubscribe', () => {
    it('returned function unsubscribes the listener', () => {
      const handler = jest.fn();
      const unsubscribe = notificationBus.onNotification(handler);

      notificationBus.notify('info', 'Before', 'visible');
      unsubscribe();
      notificationBus.notify('info', 'After', 'unsubscribed');

      expect(handler).toHaveBeenCalledTimes(1); // Only "Before"
    });

    it('calling unsubscribe twice is safe (no-op)', () => {
      const handler = jest.fn();
      const unsubscribe = notificationBus.onNotification(handler);

      unsubscribe();
      unsubscribe(); // Should not throw or cause issues

      notificationBus.notify('info', 'T', 'M');
      expect(handler).not.toHaveBeenCalled();
    });

    it('unsubscribing one listener does not affect others', () => {
      const handler1 = jest.fn();
      const handler2 = jest.fn();

      notificationBus.onNotification(handler1);
      const unsub2 = notificationBus.onNotification(handler2);

      unsub2();

      notificationBus.notify('info', 'T', 'M');

      expect(handler1).toHaveBeenCalledTimes(1);
      expect(handler2).not.toHaveBeenCalled();
    });

    it('same listener registered twice receives event twice', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'T', 'M');

      expect(handler).toHaveBeenCalledTimes(2);
    });

    it('removeNotificationListener removes ALL registrations of the same reference', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);
      notificationBus.onNotification(handler);

      notificationBus.removeNotificationListener(handler);

      notificationBus.notify('info', 'T', 'M');

      // Both registrations were removed (filter-based removal)
      expect(handler).toHaveBeenCalledTimes(0);
    });

    it('unsubscribing one of two identical listeners removes all registrations (same reference)', () => {
      // NOTE: removeNotificationListener uses reference equality and removes ALL
      // entries matching the listener reference. This is a known behavior of the
      // filter-based implementation.
      const handler = jest.fn();
      const unsub1 = notificationBus.onNotification(handler);
      notificationBus.onNotification(handler);

      unsub1(); // This removes ALL registrations of handler

      notificationBus.notify('info', 'T', 'M');

      expect(handler).toHaveBeenCalledTimes(0); // Both registrations removed
    });
  });

  // =========================================================================
  // removeNotificationListener()
  // =========================================================================
  describe('removeNotificationListener()', () => {
    it('removes a specific listener', () => {
      const handler1 = jest.fn();
      const handler2 = jest.fn();

      notificationBus.onNotification(handler1);
      notificationBus.onNotification(handler2);

      notificationBus.removeNotificationListener(handler1);

      notificationBus.notify('info', 'T', 'M');

      expect(handler1).not.toHaveBeenCalled();
      expect(handler2).toHaveBeenCalledTimes(1);
    });

    it('is a no-op for a listener that was never added', () => {
      const handler = jest.fn();
      // Should not throw
      notificationBus.removeNotificationListener(handler);

      notificationBus.onNotification(handler);
      notificationBus.notify('info', 'T', 'M');

      expect(handler).toHaveBeenCalledTimes(1);
    });

    it('works the same as the unsubscribe function returned by onNotification', () => {
      const handler = jest.fn();
      const unsubscribe = notificationBus.onNotification(handler);

      // Both approaches should remove the listener equally
      notificationBus.removeNotificationListener(handler);

      notificationBus.notify('info', 'T', 'M');
      expect(handler).not.toHaveBeenCalled();

      // Calling the returned function afterwards should also be safe
      unsubscribe();
      notificationBus.notify('info', 'T2', 'M2');
      expect(handler).not.toHaveBeenCalled();
    });
  });

  // =========================================================================
  // getNotificationHistory()
  // =========================================================================
  describe('getNotificationHistory()', () => {
    it('returns an empty array initially', () => {
      expect(notificationBus.getNotificationHistory()).toEqual([]);
    });

    it('stores each notification in history', () => {
      notificationBus.notify('info', 'First', 'one');
      notificationBus.notify('warning', 'Second', 'two');

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(2);
      expect(history[0].title).toBe('First');
      expect(history[1].title).toBe('Second');
    });

    it('returns a copy of the history (not a reference)', () => {
      notificationBus.notify('info', 'T', 'M');

      const history1 = notificationBus.getNotificationHistory();
      const history2 = notificationBus.getNotificationHistory();

      expect(history1).not.toBe(history2); // Different references
      expect(history1).toEqual(history2); // Same contents
    });

    it('caps history at 100 events', () => {
      // Add 105 notifications
      for (let i = 0; i < 105; i++) {
        notificationBus.notify('info', `Title ${i}`, `Message ${i}`);
      }

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(100);

      // The first 5 should have been evicted; oldest is Title 5
      expect(history[0].title).toBe('Title 5');
      expect(history[99].title).toBe('Title 104');
    });

    it('events include all fields from notify() call', () => {
      notificationBus.notify('error', 'Big Error', 'Stack overflow', 10000);

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(1);

      const event = history[0];
      expect(event.type).toBe('error');
      expect(event.title).toBe('Big Error');
      expect(event.message).toBe('Stack overflow');
      expect(event.duration).toBe(10000);
      expect(event.id).toBeDefined();
    });

    it('history includes events even when no listeners are attached', () => {
      notificationBus.notify('success', 'No Listener', 'Still recorded');

      const history = notificationBus.getNotificationHistory();
      expect(history).toHaveLength(1);
      expect(history[0].title).toBe('No Listener');
    });
  });

  // =========================================================================
  // _resetForTesting()
  // =========================================================================
  describe('_resetForTesting()', () => {
    it('clears all listeners', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus._resetForTesting();
      notificationBus.notify('info', 'T', 'M');

      expect(handler).not.toHaveBeenCalled();
    });

    it('clears notification history', () => {
      notificationBus.notify('info', 'T', 'M');

      notificationBus._resetForTesting();

      expect(notificationBus.getNotificationHistory()).toEqual([]);
    });

    it('resets the ID counter', () => {
      const handler = jest.fn();
      notificationBus.onNotification(handler);

      notificationBus.notify('info', 'Before', 'reset');
      const idBefore = (handler.mock.calls[0][0] as NotificationEvent).id;
      const counterBefore = parseInt(idBefore.split('_')[1], 10);

      notificationBus._resetForTesting();
      notificationBus.onNotification(handler);
      notificationBus.notify('info', 'After', 'reset');
      const idAfter = (handler.mock.calls[1][0] as NotificationEvent).id;
      const counterAfter = parseInt(idAfter.split('_')[1], 10);

      expect(counterBefore).toBeGreaterThanOrEqual(0);
      expect(counterAfter).toBe(0); // Counter restarted
    });
  });

  // =========================================================================
  // Integration-style scenarios
  // =========================================================================
  describe('integration scenarios', () => {
    it('full lifecycle: subscribe → notify → assert → unsubscribe → notify → assert', () => {
      const handler = jest.fn();

      // Subscribe
      const unsub = notificationBus.onNotification(handler);

      // First notification
      notificationBus.notify('info', 'Step 1', 'Subscribed');
      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler.mock.calls[0][0].title).toBe('Step 1');

      // Unsubscribe
      unsub();

      // Second notification — handler should not fire
      notificationBus.notify('error', 'Step 2', 'Unsubscribed');
      expect(handler).toHaveBeenCalledTimes(1); // No additional calls

      // History should have both events
      expect(notificationBus.getNotificationHistory()).toHaveLength(2);
    });

    it('multiple independent listeners with different lifetimes', () => {
      const handlerA = jest.fn();
      const handlerB = jest.fn();
      const handlerC = jest.fn();

      notificationBus.onNotification(handlerA);
      const unsubB = notificationBus.onNotification(handlerB);
      notificationBus.onNotification(handlerC);

      notificationBus.notify('info', 'Round 1', 'All three');
      expect(handlerA).toHaveBeenCalledTimes(1);
      expect(handlerB).toHaveBeenCalledTimes(1);
      expect(handlerC).toHaveBeenCalledTimes(1);

      unsubB();

      notificationBus.notify('info', 'Round 2', 'A and C only');
      expect(handlerA).toHaveBeenCalledTimes(2);
      expect(handlerB).toHaveBeenCalledTimes(1); // Unsubscribed
      expect(handlerC).toHaveBeenCalledTimes(2);

      notificationBus.removeNotificationListener(handlerA);

      notificationBus.notify('info', 'Round 3', 'Only C');

      // handlerA was completely removed
      expect(handlerA).toHaveBeenCalledTimes(2); // No new calls in round 3
      expect(handlerC).toHaveBeenCalledTimes(3); // C still active
    });
  });
});
