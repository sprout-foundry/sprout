/**
 * NotificationCenter.test.tsx — Unit tests for the bus-driven toast stack.
 *
 * Covers:
 * - Renders nothing when bus is empty
 * - Subscribes to notificationBus and renders notifications via NotificationStack
 * - Auto-dismiss after 5s (when no explicit duration)
 * - Respects explicit duration when set
 * - Dismiss callback removes the notification
 * - Cleans up timers + subscriptions on unmount
 * - publishSystemNotification category → type mapping
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach, beforeAll } from 'vitest';
import NotificationCenter, { publishSystemNotification } from './NotificationCenter';
import { notificationBus, type NotificationEvent } from '@sprout/ui';

// ---------------------------------------------------------------------------
// Mock @sprout/ui NotificationStack so we don't need the whole shared stack
// ---------------------------------------------------------------------------
const mockOnDismiss = vi.fn();
let lastReceivedNotifications: any[] = [];
vi.mock('@sprout/ui', async () => {
  const actual = await vi.importActual<any>('@sprout/ui');
  return {
    ...actual,
    NotificationStack: (props: any) => {
      lastReceivedNotifications = props.notifications;
      mockOnDismiss.mockImplementation(props.onDismiss);
      return (
        <div data-testid="mock-notification-stack">
          {props.notifications.map((n: any) => (
            <article
              key={n.id}
              data-testid={`toast-${n.id}`}
              data-type={n.type}
            >
              <h3 data-testid={`toast-title-${n.id}`}>{n.title}</h3>
              <p data-testid={`toast-message-${n.id}`}>{n.message}</p>
              <button
                type="button"
                data-testid={`dismiss-${n.id}`}
                onClick={() => props.onDismiss(n.id)}
              >
                ×
              </button>
            </article>
          ))}
        </div>
      );
    },
  };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  vi.useRealTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  lastReceivedNotifications = [];
  mockOnDismiss.mockReset();
  // Reset the singleton bus between tests so listener counts don't bleed.
  notificationBus._resetForTesting();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  vi.useRealTimers();
  notificationBus._resetForTesting();
});

function renderCenter() {
  act(() => {
    root.render(createElement(NotificationCenter));
  });
}

/** Count rendered toasts (excludes title/message children). */
function toastCount(container: HTMLElement): number {
  return container.querySelectorAll('[data-testid^="toast-"]:not([data-testid^="toast-title-"]):not([data-testid^="toast-message-"])').length;
}

// ---------------------------------------------------------------------------
// Tests: Rendering
// ---------------------------------------------------------------------------

describe('NotificationCenter rendering', () => {
  it('renders nothing when bus is empty', () => {
    renderCenter();
    expect(container.innerHTML).toBe('');
  });

  it('renders the NotificationStack after a bus notification arrives', () => {
    renderCenter();
    act(() => {
      notificationBus.notify('warning', 'Rate limit hit', 'Try again in 60s');
    });
    expect(container.querySelector('[data-testid="mock-notification-stack"]')).not.toBeNull();
    expect(toastCount(container)).toBe(1);
  });

  it('passes the title and message through', () => {
    renderCenter();
    let eventId = '';
    act(() => {
      eventId = notificationBus.notify('error', 'Auth failed', 'Invalid token');
    });
    const titleEl = container.querySelector(`[data-testid="toast-title-${eventId}"]`);
    const msgEl = container.querySelector(`[data-testid="toast-message-${eventId}"]`);
    expect(titleEl?.textContent).toBe('Auth failed');
    expect(msgEl?.textContent).toBe('Invalid token');
  });

  it('passes the notification type through', () => {
    renderCenter();
    let eventId = '';
    act(() => {
      eventId = notificationBus.notify('error', 'Auth failed', 'Invalid token');
    });
    const toast = container.querySelector(`[data-testid="toast-${eventId}"]`);
    expect(toast?.getAttribute('data-type')).toBe('error');
  });
});

// ---------------------------------------------------------------------------
// Tests: Auto-dismiss after 5s
// ---------------------------------------------------------------------------

describe('NotificationCenter auto-dismiss', () => {
  it('auto-dismisses a notification after 5s when no duration was set', () => {
    vi.useFakeTimers();
    try {
      renderCenter();
      act(() => {
        notificationBus.notify('warning', 'Rate limit', 'Wait 60s');
      });
      expect(toastCount(container)).toBe(1);

      // Advance just under 5s — still present
      act(() => {
        vi.advanceTimersByTime(4999);
      });
      expect(toastCount(container)).toBe(1);

      // Advance past 5s — gone
      act(() => {
        vi.advanceTimersByTime(2);
      });
      expect(toastCount(container)).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('respects an explicit duration from the bus (defers to NotificationItem)', () => {
    vi.useFakeTimers();
    try {
      renderCenter();
      // When an explicit duration is set, NotificationCenter must NOT set its
      // own 5s auto-dismiss timer — the NotificationItem will handle dismissal
      // on its own. So after 1s of the NotificationCenter's clock the toast
      // must still be in the list (the item's internal timer hasn't fired
      // because the mock doesn't call onDismiss).
      act(() => {
        notificationBus.notify('info', 'Quick', 'Bye', 1000);
      });
      expect(toastCount(container)).toBe(1);

      // NotificationCenter's own 5s timer would fire at 5000ms. Advance
      // past that — the toast must STILL be there because the parent
      // deferred to NotificationItem (and the mock doesn't auto-dismiss).
      act(() => {
        vi.advanceTimersByTime(6000);
      });
      expect(toastCount(container)).toBe(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not stack multiple auto-dismiss timers when many notifications arrive', () => {
    vi.useFakeTimers();
    try {
      renderCenter();
      act(() => {
        notificationBus.notify('info', 'A', 'msgA');
        notificationBus.notify('info', 'B', 'msgB');
        notificationBus.notify('info', 'C', 'msgC');
      });
      expect(toastCount(container)).toBe(3);

      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(toastCount(container)).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });
});

// ---------------------------------------------------------------------------
// Tests: Manual dismiss via the close button
// ---------------------------------------------------------------------------

describe('NotificationCenter manual dismiss', () => {
  it('removes a notification when the dismiss button is clicked', () => {
    renderCenter();
    let eventId = '';
    act(() => {
      eventId = notificationBus.notify('warning', 'Rate limit', 'Wait 60s');
    });
    expect(toastCount(container)).toBe(1);

    const dismissBtn = container.querySelector(`[data-testid="dismiss-${eventId}"]`) as HTMLElement;
    act(() => {
      dismissBtn.click();
    });
    expect(toastCount(container)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// Tests: Cleanup on unmount
// ---------------------------------------------------------------------------

describe('NotificationCenter cleanup', () => {
  it('clears all pending auto-dismiss timers on unmount (no late setState warnings)', () => {
    vi.useFakeTimers();
    try {
      renderCenter();
      act(() => {
        notificationBus.notify('info', 'A', 'msgA');
      });
      act(() => {
        root.unmount();
      });

      // Should not throw when the stale timer fires
      expect(() => vi.advanceTimersByTime(10000)).not.toThrow();
      expect(toastCount(container)).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('no longer receives notifications after unmount', () => {
    renderCenter();
    act(() => {
      root.unmount();
    });
    // After unmount, the listener is removed. A new notify should not crash.
    expect(() => {
      act(() => {
        notificationBus.notify('warning', 'late', 'msg');
      });
    }).not.toThrow();
    // Container was removed in afterEach, so we just assert the call didn't throw.
  });
});

// ---------------------------------------------------------------------------
// Tests: publishSystemNotification helper
// ---------------------------------------------------------------------------

describe('publishSystemNotification helper', () => {
  it('maps rate_limit → warning', () => {
    const spy = vi.spyOn(notificationBus, 'notify');
    publishSystemNotification('rate_limit', 'Rate limit', 'Wait');
    expect(spy).toHaveBeenCalledWith('warning', 'Rate limit', 'Wait');
    spy.mockRestore();
  });

  it('maps auth_failure → error', () => {
    const spy = vi.spyOn(notificationBus, 'notify');
    publishSystemNotification('auth_failure', 'Auth failed', 'Token expired');
    expect(spy).toHaveBeenCalledWith('error', 'Auth failed', 'Token expired');
    spy.mockRestore();
  });

  it('maps permission_required → warning', () => {
    const spy = vi.spyOn(notificationBus, 'notify');
    publishSystemNotification('permission_required', 'Allow?', 'Tool needs access');
    expect(spy).toHaveBeenCalledWith('warning', 'Allow?', 'Tool needs access');
    spy.mockRestore();
  });

  it('maps agent_blocked → error', () => {
    const spy = vi.spyOn(notificationBus, 'notify');
    publishSystemNotification('agent_blocked', 'Blocked', 'Permission denied');
    expect(spy).toHaveBeenCalledWith('error', 'Blocked', 'Permission denied');
    spy.mockRestore();
  });

  it('maps unknown categories to info (graceful default)', () => {
    const spy = vi.spyOn(notificationBus, 'notify');
    publishSystemNotification('something_weird', 'Hello', 'World');
    expect(spy).toHaveBeenCalledWith('info', 'Hello', 'World');
    spy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Tests: NotificationStack contract
// ---------------------------------------------------------------------------

describe('NotificationStack contract', () => {
  it('passes a NotificationData[] with the bus event fields', () => {
    renderCenter();
    act(() => {
      notificationBus.notify('error', 'Auth failed', 'Token expired');
    });
    expect(lastReceivedNotifications.length).toBe(1);
    const n = lastReceivedNotifications[0];
    expect(n.id).toBeTruthy();
    expect(n.type).toBe('error');
    expect(n.title).toBe('Auth failed');
    expect(n.message).toBe('Token expired');
    expect(typeof n.createdAt).toBe('number');
    expect(n.read).toBe(false);
  });

  it('queues multiple notifications in arrival order', () => {
    renderCenter();
    act(() => {
      notificationBus.notify('info', 'A', 'msgA');
      notificationBus.notify('info', 'B', 'msgB');
    });
    expect(lastReceivedNotifications.length).toBe(2);
    expect(lastReceivedNotifications[0].title).toBe('A');
    expect(lastReceivedNotifications[1].title).toBe('B');
  });
});
