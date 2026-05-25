// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import NotificationStack from './NotificationStack';
import type { NotificationData } from '../types/notification';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  vi.useFakeTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
  act(() => {
    root?.unmount();
  });
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
  container?.remove();
});

function makeNotification(overrides: Partial<NotificationData> = {}): NotificationData {
  return {
    id: 'notif-1',
    type: 'info',
    title: 'Default Title',
    message: 'Default message',
    createdAt: 1000,
    read: false,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('NotificationStack', () => {
  it('returns null when notifications array is empty', () => {
    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [],
        onDismiss: vi.fn(),
      }));
    });

    expect(document.querySelector('.notification-container')).toBeNull();
  });

  it('renders notification-container when there are notifications', () => {
    const notifications = [makeNotification()];
    const onDismiss = vi.fn();

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss,
      }));
    });

    const containerEl = document.querySelector('.notification-container');
    expect(containerEl).not.toBeNull();
  });

  it('renders container with role="region" and aria-label="Notifications"', () => {
    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [makeNotification()],
        onDismiss: vi.fn(),
      }));
    });

    const el = document.querySelector('.notification-container');
    expect(el?.getAttribute('role')).toBe('region');
    expect(el?.getAttribute('aria-label')).toBe('Notifications');
  });

  it('renders a NotificationItem for each notification', () => {
    const notifications = [
      makeNotification({ id: 'n1', type: 'info', title: 'First', message: 'msg1' }),
      makeNotification({ id: 'n2', type: 'error', title: 'Second', message: 'msg2' }),
    ];
    const onDismiss = vi.fn();

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss,
      }));
    });

    const items = document.querySelectorAll('.notification-item');
    expect(items).toHaveLength(2);

    // Check first notification
    const first = document.getElementById('notification-n1');
    expect(first).not.toBeNull();
    expect(first?.querySelector('.notification-title')?.textContent).toBe('First');
    expect(first?.querySelector('.notification-message')?.textContent).toBe('msg1');

    // Check second notification
    const second = document.getElementById('notification-n2');
    expect(second).not.toBeNull();
    expect(second?.querySelector('.notification-title')?.textContent).toBe('Second');
  });

  it('passes onDismiss to child NotificationItems', () => {
    const notifications = [makeNotification({ id: 'test-dispatch' })];
    const onDismiss = vi.fn();

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss,
      }));
    });

    const dismissBtn = document.querySelector('.notification-dismiss');
    act(() => {
      dismissBtn?.click();
    });

    // Allow exit animation to complete
    act(() => {
      vi.advanceTimersByTime(200);
    });

    expect(onDismiss).toHaveBeenCalledWith('test-dispatch');
  });

  it('applies custom className alongside notification-container', () => {
    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [makeNotification()],
        onDismiss: vi.fn(),
        className: 'custom-overlay',
      }));
    });

    const el = document.querySelector('.notification-container');
    expect(el?.classList.contains('custom-overlay')).toBe(true);
  });

  it('renders without className prop', () => {
    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [makeNotification()],
        onDismiss: vi.fn(),
      }));
    });

    const el = document.querySelector('.notification-container');
    expect(el?.classList.contains('notification-container')).toBe(true);
  });

  it('passes duration prop to NotificationItem', () => {
    const notifications = [
      makeNotification({ id: 'dur-test', duration: 3000 })
    ];

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss: vi.fn(),
      }));
    });

    // The NotificationItem should auto-dismiss after 3s + exit animation
    // We can verify this by advancing timers
    const onDismiss = vi.fn();

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss,
      }));
    });

    // No auto-dismiss before duration
    act(() => {
      vi.advanceTimersByTime(2999);
    });
    expect(onDismiss).not.toHaveBeenCalled();

    // After duration
    act(() => {
      vi.advanceTimersByTime(1);
    });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(onDismiss).toHaveBeenCalledWith('dur-test');
  });

  it('renders different notification types with correct classes', () => {
    const notifications = [
      makeNotification({ id: 't1', type: 'info', title: 'Info', message: 'info msg' }),
      makeNotification({ id: 't2', type: 'success', title: 'Success', message: 'success msg' }),
      makeNotification({ id: 't3', type: 'warning', title: 'Warning', message: 'warning msg' }),
      makeNotification({ id: 't4', type: 'error', title: 'Error', message: 'error msg' }),
    ];

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications,
        onDismiss: vi.fn(),
      }));
    });

    expect(document.getElementById('notification-t1')?.classList.contains('type-info')).toBe(true);
    expect(document.getElementById('notification-t2')?.classList.contains('type-success')).toBe(true);
    expect(document.getElementById('notification-t3')?.classList.contains('type-warning')).toBe(true);
    expect(document.getElementById('notification-t4')?.classList.contains('type-error')).toBe(true);
  });

  it('handles updates: removing a notification from the array', () => {
    const onDismiss = vi.fn();

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [
          makeNotification({ id: 'n1', title: 'One', message: 'msg1' }),
          makeNotification({ id: 'n2', title: 'Two', message: 'msg2' }),
        ],
        onDismiss,
      }));
    });

    expect(document.querySelectorAll('.notification-item')).toHaveLength(2);

    act(() => {
      root.render(createElement(NotificationStack, {
        notifications: [
          makeNotification({ id: 'n1', title: 'One', message: 'msg1' }),
        ],
        onDismiss,
      }));
    });

    expect(document.querySelectorAll('.notification-item')).toHaveLength(1);
    expect(document.getElementById('notification-n1')).not.toBeNull();
    expect(document.getElementById('notification-n2')).toBeNull();
  });
});
