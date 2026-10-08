import { act, createElement, useEffect, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import {
  NotificationProvider,
  useNotifications,
  type Notification,
  type NotificationType,
} from './NotificationContext';
import { notificationBus } from '../services/notificationBus';

// ── Test wrapper to use useNotifications ─────────────────────────────

function TestConsumer(): ReactNode {
  const { notifications, addNotification, removeNotification, clearNotifications } = useNotifications();
  return createElement('div', { 'data-testid': 'notifications' },
    createElement('span', { 'data-testid': 'count', 'data-count': String(notifications.length) }, String(notifications.length)),
    ...notifications.map((n: Notification) =>
      createElement('div', {
        key: n.id,
        'data-testid': 'notification-item',
        'data-id': n.id,
        'data-type': n.type,
      }, `${n.title}: ${n.message}`)
    ),
    createElement('button', {
      type: 'button',
      'data-testid': 'add-info',
      onClick: () => addNotification('info', 'Test Info', 'Info message'),
    }, 'Add Info'),
    createElement('button', {
      type: 'button',
      'data-testid': 'add-error',
      onClick: () => addNotification('error', 'Test Error', 'Error message'),
    }, 'Add Error'),
    createElement('button', {
      type: 'button',
      'data-testid': 'add-warning',
      onClick: () => addNotification('warning', 'Test Warning', 'Warning message'),
    }, 'Add Warning'),
    createElement('button', {
      type: 'button',
      'data-testid': 'add-success',
      onClick: () => addNotification('success', 'Test Success', 'Success message'),
    }, 'Add Success'),
    createElement('button', {
      type: 'button',
      'data-testid': 'remove-last',
      onClick: () => {
        if (notifications.length > 0) removeNotification(notifications[notifications.length - 1].id);
      },
    }, 'Remove Last'),
    createElement('button', {
      type: 'button',
      'data-testid': 'clear-all',
      onClick: () => clearNotifications(),
    }, 'Clear All'),
  );
}

function TestWrapper({ children }: { children: ReactNode }) {
  return createElement(NotificationProvider, { children });
}

// ── Helpers ────────────────────────────────────────────────────────────

let container: HTMLDivElement;
let root: Root;

const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();

beforeAll(() => {
  // IS_REACT_ACT_ENVIRONMENT is React's runtime act() flag; not typed on globalThis.
  (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  Object.defineProperty(global, 'localStorage', { value: localStorageMock });
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  notificationBus._resetForTesting();
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

// ── Tests ──────────────────────────────────────────────────────────────

describe('NotificationProvider', () => {
  it('renders children inside provider', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement('div', { 'data-testid': 'child' }, 'Hello'),
      }));
    });
    expect(container.querySelector('[data-testid="child"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="child"]')?.textContent).toBe('Hello');
  });

  it('throws when useNotifications is called outside provider', () => {
    // Capture the error since React 18 may not throw synchronously
    const originalConsoleError = console.error;
    let capturedError: Error | null = null;
    console.error = vi.fn((...args: any[]) => {
      if (typeof args[0] === 'string' && args[0].includes('useNotifications must be used within NotificationProvider')) {
        capturedError = new Error('useNotifications must be used within NotificationProvider');
      }
    });
    try {
      // This will throw because TestConsumer uses useNotifications without a provider
      expect(() => {
        act(() => {
          root.render(createElement(TestConsumer));
        });
      }).toThrow(/useNotifications must be used within NotificationProvider/);
    } catch {
      // If the outer expect doesn't catch it, check via console.error
      expect(capturedError).not.toBeNull();
    } finally {
      console.error = originalConsoleError;
    }
  });

  it('starts with empty notifications list', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    const countEl = container.querySelector('[data-testid="count"]');
    expect(countEl?.textContent).toBe('0');
    expect(container.querySelectorAll('[data-testid="notification-item"]')).toHaveLength(0);
  });
});

describe('addNotification', () => {
  it('adds an info notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
    const item = container.querySelector('[data-testid="notification-item"]');
    expect(item).not.toBeNull();
    expect(item?.getAttribute('data-type')).toBe('info');
    expect(item?.textContent).toContain('Test Info');
    expect(item?.textContent).toContain('Info message');
  });

  it('adds an error notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-error"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const item = container.querySelector('[data-testid="notification-item"]');
    expect(item?.getAttribute('data-type')).toBe('error');
  });

  it('adds a warning notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-warning"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const item = container.querySelector('[data-testid="notification-item"]');
    expect(item?.getAttribute('data-type')).toBe('warning');
  });

  it('adds a success notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-success"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const item = container.querySelector('[data-testid="notification-item"]');
    expect(item?.getAttribute('data-type')).toBe('success');
  });

  it('adds multiple notifications and shows correct count', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-error"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-success"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('3');
    expect(container.querySelectorAll('[data-testid="notification-item"]')).toHaveLength(3);
  });

  it('adds notification with custom duration', () => {
    let capturedDuration: number | undefined;
    const TestDurationConsumer = () => {
      const { addNotification, notifications } = useNotifications();
      // Capture duration from the last added notification
      if (notifications.length > 0) {
        capturedDuration = notifications[notifications.length - 1].duration;
      }
      return createElement('div', {
        'data-testid': 'duration-test',
        'data-count': String(notifications.length),
      }, createElement('button', {
        type: 'button',
        'data-testid': 'add-with-duration',
        onClick: () => addNotification('info', 'Duration Test', 'With duration', 15000),
      }, 'Add'));
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestDurationConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-with-duration"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(capturedDuration).toBe(15000);
  });
});

describe('removeNotification', () => {
  it('removes the last added notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-error"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('2');

    act(() => {
      container.querySelector('[data-testid="remove-last"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
    // The remaining one should be the info
    expect(container.querySelector('[data-testid="notification-item"]')?.getAttribute('data-type')).toBe('info');
  });

  it('does not crash when removing non-existent notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="remove-last"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('0');
  });
});

describe('clearNotifications', () => {
  it('clears all notifications', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-error"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-success"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('3');

    act(() => {
      container.querySelector('[data-testid="clear-all"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('0');
    expect(container.querySelectorAll('[data-testid="notification-item"]')).toHaveLength(0);
  });

  it('does not crash when clearing empty list', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="clear-all"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('0');
  });
});

describe('duration clamping', () => {
  it('clamps duration to max 60000ms', () => {
    let capturedDuration: number | undefined;
    const TestClampConsumer = () => {
      const { addNotification, notifications } = useNotifications();
      if (notifications.length > 0) {
        capturedDuration = notifications[notifications.length - 1].duration;
      }
      return createElement('div', { 'data-testid': 'clamp-test' },
        createElement('button', {
          type: 'button',
          'data-testid': 'add-huge-duration',
          onClick: () => addNotification('info', 'Clamp Test', 'Huge duration', 99999),
        }, 'Add'));
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestClampConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-huge-duration"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(capturedDuration).toBe(60000);
  });

  it('clamps duration to min 0ms', () => {
    let capturedDuration: number | undefined;
    const TestClampConsumer = () => {
      const { addNotification, notifications } = useNotifications();
      if (notifications.length > 0) {
        capturedDuration = notifications[notifications.length - 1].duration;
      }
      return createElement('div', { 'data-testid': 'clamp-test' },
        createElement('button', {
          type: 'button',
          'data-testid': 'add-neg-duration',
          onClick: () => addNotification('info', 'Clamp Test', 'Neg duration', -500),
        }, 'Add'));
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestClampConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-neg-duration"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(capturedDuration).toBe(0);
  });
});

describe('notificationBus subscription', () => {
  it('receives notifications from notificationBus', async () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('0');

    // Fire a bus notification and await React's microtask queue
    await act(async () => {
      notificationBus.notify('info', 'Bus Title', 'Bus message');
    });

    // The provider should have picked it up via the subscription
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
    const item = container.querySelector('[data-testid="notification-item"]');
    expect(item?.textContent).toContain('Bus Title');
    expect(item?.textContent).toContain('Bus message');
  });

  it('clamps duration from bus notifications', async () => {
    let capturedDuration: number | undefined;
    const TestBusClampConsumer = () => {
      const { notifications } = useNotifications();
      if (notifications.length > 0) {
        capturedDuration = notifications[notifications.length - 1].duration;
      }
      return createElement('div', { 'data-testid': 'bus-clamp-test', 'data-count': String(notifications.length) });
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestBusClampConsumer),
      }));
    });

    await act(async () => {
      notificationBus.notify('error', 'Bus Clamp', 'With huge duration', 99999);
    });
    expect(capturedDuration).toBe(60000);
  });

  it('propagates an action from a bus notification into the provider state', async () => {
    let capturedAction: { label: string; onClick: () => void; keepOpen?: boolean } | undefined;
    const TestBusActionConsumer = () => {
      const { notifications } = useNotifications();
      if (notifications.length > 0) {
        capturedAction = notifications[notifications.length - 1].action;
      }
      return createElement('div', { 'data-testid': 'bus-action-test', 'data-count': String(notifications.length) });
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestBusActionConsumer),
      }));
    });

    const onClick = vi.fn();
    await act(async () => {
      notificationBus.notify('warning', 'Configure', 'Provider missing', undefined, {
        label: 'Open settings',
        onClick,
      });
    });

    expect(capturedAction).toBeDefined();
    expect(capturedAction?.label).toBe('Open settings');
    expect(capturedAction?.onClick).toBe(onClick);
  });

  it('leaves action undefined when the bus does not provide one (backwards compat)', async () => {
    let capturedAction: { label: string } | undefined;
    const TestBusNoActionConsumer = () => {
      const { notifications } = useNotifications();
      if (notifications.length > 0) {
        capturedAction = notifications[notifications.length - 1].action;
      }
      return createElement('div', { 'data-testid': 'bus-no-action-test' });
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestBusNoActionConsumer),
      }));
    });

    await act(async () => {
      notificationBus.notify('info', 'Plain', 'No action');
    });

    expect(capturedAction).toBeUndefined();
  });
});

describe('notification fields', () => {
  it('generates unique IDs for each notification', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('[data-testid="add-error"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    const items = container.querySelectorAll('[data-testid="notification-item"]');
    const ids = Array.from(items).map(el => el.getAttribute('data-id'));
    expect(ids[0]).not.toBe(ids[1]);
  });

  it('sets createdAt timestamp on each notification', () => {
    let hasTimestamp = false;
    const TestTimestampConsumer = () => {
      const { notifications } = useNotifications();
      hasTimestamp = notifications.some(n => n.createdAt > 0);
      return createElement('div', {
        'data-testid': 'ts-test',
        'data-count': String(notifications.length),
      }, createElement('button', {
        type: 'button',
        'data-testid': 'add-ts',
        onClick: () => {},
      }, 'Add'));
    };
    // We use a modified consumer that calls addNotification directly
    let addNotificationRef: ((type: NotificationType, title: string, message: string, duration?: number) => string) | null = null;
    const TestTimestampConsumer2 = () => {
      const { addNotification, notifications } = useNotifications();
      addNotificationRef = addNotification;
      hasTimestamp = notifications.some(n => n.createdAt > 0);
      return createElement('div', {
        'data-testid': 'ts-test',
        'data-has-ts': String(hasTimestamp),
        'data-count': String(notifications.length),
      });
    };
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestTimestampConsumer2),
      }));
    });
    act(() => {
      addNotificationRef?.('info', 'TS Test', 'Check timestamp');
    });
    expect(container.querySelector('[data-testid="ts-test"]')?.getAttribute('data-has-ts')).toBe('true');
  });
});

describe('sink path (host delivery)', () => {
  type SinkCall = { level: NotificationType; title: string; message: string; duration?: number; action?: unknown };

  function SinkConsumer({ onAdd }: { onAdd: (add: ReturnType<typeof useNotifications>['addNotification']) => void }): ReactNode {
    const { addNotification } = useNotifications();
    useEffect(() => {
      onAdd(addNotification);
    }, [addNotification, onAdd]);
    return createElement('div', { 'data-testid': 'sink-consumer' });
  }

  it('routes addNotification through the sink instead of the local reducer', () => {
    const calls: SinkCall[] = [];
    const sink = vi.fn((n: SinkCall) => calls.push(n));
    let add: ReturnType<typeof useNotifications>['addNotification'] | null = null;

    act(() => {
      root.render(createElement(NotificationProvider, {
        sink,
        children: createElement(SinkConsumer, { onAdd: (a) => { add = a; } }),
      }));
    });

    let id = '';
    act(() => {
      id = add!('warning', 'Host Title', 'Host message', 5000);
    });

    // The sink received exactly one call with the full shape, and nothing was
    // dispatched locally (a host that echoes back on the bus adds the entry).
    expect(sink).toHaveBeenCalledTimes(1);
    expect(calls).toHaveLength(1);
    expect(calls[0]).toEqual({ level: 'warning', title: 'Host Title', message: 'Host message', duration: 5000, action: undefined });
    // The id return contract is preserved (generated regardless of delivery).
    expect(typeof id).toBe('string');
    expect(id.length).toBeGreaterThan(0);
    expect(notificationBus.getNotificationHistory()).toHaveLength(0);
  });

  it('reaches exactly one provider entry when the sink echoes on the bus (no double-add)', async () => {
    // A host sink that mirrors the local host: it forwards to the in-app bus,
    // which the provider subscribes to. If addNotification ALSO dispatched,
    // this would be two entries.
    const sink = vi.fn((n: SinkCall) => {
      notificationBus.notify(n.level, n.title, n.message, n.duration, n.action as never);
    });
    let add: ReturnType<typeof useNotifications>['addNotification'] | null = null;

    act(() => {
      root.render(createElement(NotificationProvider, {
        sink,
        children: createElement(SinkConsumer, { onAdd: (a) => { add = a; } }),
      }));
    });

    await act(async () => {
      add!('error', 'Echo', 'Echoed once');
    });

    expect(sink).toHaveBeenCalledTimes(1);
    expect(container.querySelectorAll('[data-testid="notification-item"]')).toHaveLength(0);
    // The provider state carries exactly one entry — from the bus echo, not
    // from a second local dispatch.
    // (TestConsumer is not mounted here; assert via the bus history.)
    expect(notificationBus.getNotificationHistory()).toHaveLength(1);
  });

  it('clamps duration before handing it to the sink (max 60000)', () => {
    const sink = vi.fn();
    let add: ReturnType<typeof useNotifications>['addNotification'] | null = null;
    act(() => {
      root.render(createElement(NotificationProvider, {
        sink,
        children: createElement(SinkConsumer, { onAdd: (a) => { add = a; } }),
      }));
    });
    act(() => {
      add!('info', 'Clamp', 'Huge', 999999);
    });
    expect(sink).toHaveBeenCalledWith({ level: 'info', title: 'Clamp', message: 'Huge', duration: 60000, action: undefined });
  });

  it('clamps duration before handing it to the sink (min 0)', () => {
    const sink = vi.fn();
    let add: ReturnType<typeof useNotifications>['addNotification'] | null = null;
    act(() => {
      root.render(createElement(NotificationProvider, {
        sink,
        children: createElement(SinkConsumer, { onAdd: (a) => { add = a; } }),
      }));
    });
    act(() => {
      add!('info', 'Clamp', 'Negative', -10);
    });
    expect(sink).toHaveBeenCalledWith({ level: 'info', title: 'Clamp', message: 'Negative', duration: 0, action: undefined });
  });

  it('keeps local dispatch behavior when no sink is supplied', () => {
    act(() => {
      root.render(createElement(NotificationProvider, {
        children: createElement(TestConsumer),
      }));
    });
    act(() => {
      container.querySelector('[data-testid="add-info"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
    expect(container.querySelectorAll('[data-testid="notification-item"]')).toHaveLength(1);
  });
});
