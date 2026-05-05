// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import NotificationItem from './NotificationItem';
import type { NotificationType } from '../contexts/NotificationContext';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  jest.useFakeTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  jest.useRealTimers();
  act(() => {
    root?.unmount();
  });
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('NotificationItem', () => {
  const baseProps = {
    id: 'test-id',
    title: 'Test Title',
    message: 'Test message body',
    onClose: jest.fn(),
  };

  it.each<NotificationType>(['info', 'success', 'warning', 'error'])(
    'renders type "%s" with correct icon',
    (type) => {
      const iconMap: Record<NotificationType, string> = {
        info: 'ℹ',
        success: '✓',
        warning: '⚠',
        error: '✕',
      };

      act(() => {
        root.render(createElement(NotificationItem, {
          ...baseProps,
          type,
        }));
      });

      const icon = container.querySelector('.notification-icon');
      expect(icon).not.toBeNull();
      expect(icon?.textContent).toBe(iconMap[type]);
    }
  );

  it('renders the notification container with correct ID', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
      }));
    });

    const el = document.getElementById('notification-test-id');
    expect(el).not.toBeNull();
  });

  it('applies type class to container', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'error',
      }));
    });

    const el = document.getElementById('notification-test-id');
    expect(el?.classList.contains('type-error')).toBe(true);
  });

  it('renders with role="alert" and aria-live="polite"', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
      }));
    });

    const el = document.getElementById('notification-test-id');
    expect(el?.getAttribute('role')).toBe('alert');
    expect(el?.getAttribute('aria-live')).toBe('polite');
  });

  it('has tabIndex=0 for keyboard accessibility', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
      }));
    });

    const el = document.getElementById('notification-test-id');
    expect(el?.getAttribute('tabIndex')).toBe('0');
  });

  it('renders title when provided', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'success',
      }));
    });

    const title = container.querySelector('.notification-title');
    expect(title).not.toBeNull();
    expect(title?.textContent).toBe('Test Title');
  });

  it('renders message text', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'warning',
      }));
    });

    const msg = container.querySelector('.notification-message');
    expect(msg).not.toBeNull();
    expect(msg?.textContent).toBe('Test message body');
  });

  it('renders dismiss button with correct aria-label', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
      }));
    });

    const btn = container.querySelector('.notification-dismiss');
    expect(btn).not.toBeNull();
    expect(btn?.getAttribute('aria-label')).toBe('Dismiss notification');
    expect(btn?.getAttribute('type')).toBe('button');
  });

  it('calls onClose with correct id when dismiss button is clicked', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    const btn = container.querySelector('.notification-dismiss');
    act(() => {
      btn?.click();
    });

    // After exit animation duration (200ms)
    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledWith('test-id');
  });

  it('handles Escape key to close', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    const el = document.getElementById('notification-test-id');
    act(() => {
      el?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledWith('test-id');
  });

  it('handles Enter key to close', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    const el = document.getElementById('notification-test-id');
    act(() => {
      el?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledWith('test-id');
  });

  it('does not close on other key presses', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    const el = document.getElementById('notification-test-id');
    act(() => {
      el?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }));
      el?.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true }));
    });

    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('auto-dismisses after duration', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        duration: 3000,
        onClose,
      }));
    });

    // Advance past auto-dismiss timer (3s) + exit animation (200ms)
    act(() => {
      jest.advanceTimersByTime(3000);
    });

    // Close was triggered, now advance exit animation
    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledWith('test-id');
  });

  it('does not auto-dismiss when duration is 0', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        duration: 0,
        onClose,
      }));
    });

    act(() => {
      jest.advanceTimersByTime(10000);
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not auto-dismiss when duration is negative', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        duration: -1,
        onClose,
      }));
    });

    act(() => {
      jest.advanceTimersByTime(10000);
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('uses default 5000ms duration when not specified', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        id: 'test-id',
        type: 'info',
        title: 'Title',
        message: 'Msg',
        onClose,
      }));
    });

    // Advance to just before default timeout
    act(() => {
      jest.advanceTimersByTime(4999);
    });

    expect(onClose).not.toHaveBeenCalled();

    // Advance past default timeout
    act(() => {
      jest.advanceTimersByTime(1);
    });

    // Exit animation
    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledWith('test-id');
  });

  it('prevents multiple onClose calls (idempotent close)', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    const btn = container.querySelector('.notification-dismiss');

    // Click dismiss multiple times rapidly
    act(() => {
      btn?.click();
      btn?.click();
      btn?.click();
    });

    act(() => {
      jest.advanceTimersByTime(200);
    });

    // Should only call once due to isClosingRef guard
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('clears auto-dismiss timer when manually closed', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        duration: 5000,
        onClose,
      }));
    });

    // Manually close before auto-dismiss
    const btn = container.querySelector('.notification-dismiss');
    act(() => {
      btn?.click();
    });
    act(() => {
      jest.advanceTimersByTime(200);
    });

    expect(onClose).toHaveBeenCalledTimes(1);

    // Advance past where auto-dismiss would have fired
    act(() => {
      jest.advanceTimersByTime(5000);
    });
    act(() => {
      jest.advanceTimersByTime(200);
    });

    // Still only one call (auto-dismiss was cleared)
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('adds exit animation class before closing', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose: jest.fn(),
      }));
    });

    const btn = container.querySelector('.notification-dismiss');
    act(() => {
      btn?.click();
    });

    const el = document.getElementById('notification-test-id');
    expect(el?.classList.contains('notification-item-exit')).toBe(true);
  });

  it('handles re-render with different id cleanly', () => {
    const onClose = jest.fn();

    act(() => {
      root.render(createElement(NotificationItem, {
        ...baseProps,
        type: 'info',
        onClose,
      }));
    });

    // Rerender with a different id
    act(() => {
      root.render(createElement(NotificationItem, {
        id: 'new-id',
        type: 'info',
        title: 'New Title',
        message: 'New message',
        onClose,
      }));
    });

    const newEl = document.getElementById('notification-new-id');
    expect(newEl).not.toBeNull();
    expect(newEl?.querySelector('.notification-title')?.textContent).toBe('New Title');
    // Old notification should be gone
    expect(document.getElementById('notification-test-id')).toBeNull();
  });

  it('renders without title when title is empty string', () => {
    act(() => {
      root.render(createElement(NotificationItem, {
        id: 'test-id',
        type: 'info',
        title: '',
        message: 'Message only',
        onClose: jest.fn(),
      }));
    });

    const title = container.querySelector('.notification-title');
    expect(title).toBeNull();
  });
});
