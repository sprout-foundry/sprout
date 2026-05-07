/**
 * NotificationCenter.test.tsx — Unit tests for the NotificationCenter component.
 *
 * Covers:
 * - Rendering (open/closed states)
 * - Empty state display
 * - Notification list rendering (reverse order, icons, timestamps)
 * - Dismiss individual notification
 * - Dismiss all notifications
 * - Copy message to clipboard
 * - Escape key to close
 * - Outside click to close
 * - Relative time formatting
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import NotificationCenter from './NotificationCenter';
import type { Notification } from '../contexts/NotificationContext';

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

const mockRemoveNotification = vi.fn();
const mockClearNotifications = vi.fn();
let mockNotifications: Notification[] = [];

vi.mock('../contexts/NotificationContext', () => ({
  useNotifications: () => ({
    notifications: mockNotifications,
    removeNotification: mockRemoveNotification,
    clearNotifications: mockClearNotifications,
  }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
const mockPositionRef = { current: null as HTMLDivElement | null };

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  mockNotifications = [];
  mockRemoveNotification.mockReset();
  mockClearNotifications.mockReset();
  mockPositionRef.current = null;

  // Mock navigator.clipboard
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: vi.fn().mockResolvedValue(undefined),
    },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

function makeNotification(overrides: Partial<Notification> = {}): Notification {
  return {
    id: 'test-1',
    type: 'info',
    title: 'Test Notification',
    message: 'This is a test message',
    createdAt: Date.now(),
    ...overrides,
  };
}

function renderNotificationCenter(props: { isOpen?: boolean; onClose?: () => void; positionRef?: { current: HTMLDivElement | null } } = {}) {
  const isOpen = props.isOpen ?? true;
  const onClose = props.onClose ?? vi.fn();
  const positionRef = props.positionRef ?? { current: null };

  act(() => {
    root.render(createElement(NotificationCenter, { isOpen, onClose, positionRef }));
  });
}

// ---------------------------------------------------------------------------
// Tests: Rendering (open/closed)
// ---------------------------------------------------------------------------

describe('Rendering', () => {
  it('returns null when isOpen is false', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: false });
    expect(container.innerHTML).toBe('');
  });

  it('renders panel when isOpen is true', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center')).not.toBeNull();
  });

  it('has correct role and aria attributes', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    const panel = container.querySelector('.notification-center');
    expect(panel?.getAttribute('role')).toBe('dialog');
    expect(panel?.getAttribute('aria-label')).toBe('Notification center');
  });

  it('renders the notifications header', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-title')?.textContent).toBe('Notifications');
  });
});

// ---------------------------------------------------------------------------
// Tests: Empty state
// ---------------------------------------------------------------------------

describe('Empty state', () => {
  it('shows "No notifications" when notifications array is empty', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-empty')).not.toBeNull();
    expect(container.querySelector('.notification-center-empty-text')?.textContent).toBe('No notifications');
  });

  it('does not show "Dismiss All" button when no notifications', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-dismiss-all')).toBeNull();
  });

  it('does not render notification list when empty', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-list')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: Notification list
// ---------------------------------------------------------------------------

describe('Notification list', () => {
  it('shows notifications in reverse order (newest first)', () => {
    const now = Date.now();
    mockNotifications = [
      makeNotification({ id: 'first', title: 'First', createdAt: now - 10000 }),
      makeNotification({ id: 'second', title: 'Second', createdAt: now - 5000 }),
      makeNotification({ id: 'third', title: 'Third', createdAt: now }),
    ];
    renderNotificationCenter({ isOpen: true });

    const items = container.querySelectorAll('.notification-center-item-title');
    expect(items).toHaveLength(3);
    expect(items[0]?.textContent).toBe('Third');
    expect(items[1]?.textContent).toBe('Second');
    expect(items[2]?.textContent).toBe('First');
  });

  it('displays notification title', () => {
    mockNotifications = [makeNotification({ title: 'My Title' })];
    renderNotificationCenter({ isOpen: true });
    const titleEl = container.querySelector('.notification-center-item-title');
    expect(titleEl?.textContent?.trim()).toBe('My Title');
  });

  it('displays notification message', () => {
    mockNotifications = [makeNotification({ message: 'My Message' })];
    renderNotificationCenter({ isOpen: true });
    const msgEl = container.querySelector('.notification-center-item-message');
    expect(msgEl?.textContent?.trim()).toBe('My Message');
  });

  it('renders notification list with role="list"', () => {
    mockNotifications = [makeNotification()];
    renderNotificationCenter({ isOpen: true });
    const list = container.querySelector('.notification-center-list');
    expect(list?.getAttribute('role')).toBe('list');
  });

  it('shows "Dismiss All" button when notifications exist', () => {
    mockNotifications = [makeNotification()];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-dismiss-all')).not.toBeNull();
    expect(container.querySelector('.notification-center-dismiss-all')?.textContent).toBe('Dismiss All');
  });

  it('adds type-specific CSS class to notification items', () => {
    mockNotifications = [
      makeNotification({ id: 'info-1', type: 'info' }),
      makeNotification({ id: 'success-1', type: 'success' }),
      makeNotification({ id: 'warning-1', type: 'warning' }),
      makeNotification({ id: 'error-1', type: 'error' }),
    ];
    renderNotificationCenter({ isOpen: true });
    const items = container.querySelectorAll('.notification-center-item');
    // Items are reversed, so last in mock is first in DOM
    expect(items[0]?.classList.contains('type-error')).toBe(true);
    expect(items[1]?.classList.contains('type-warning')).toBe(true);
    expect(items[2]?.classList.contains('type-success')).toBe(true);
    expect(items[3]?.classList.contains('type-info')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: Type icons
// ---------------------------------------------------------------------------

describe('Type icons', () => {
  it('shows ℹ for info type', () => {
    mockNotifications = [makeNotification({ type: 'info' })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-icon')?.textContent).toBe('ℹ');
  });

  it('shows ✓ for success type', () => {
    mockNotifications = [makeNotification({ type: 'success' })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-icon')?.textContent).toBe('✓');
  });

  it('shows ⚠ for warning type', () => {
    mockNotifications = [makeNotification({ type: 'warning' })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-icon')?.textContent).toBe('⚠');
  });

  it('shows ✕ for error type', () => {
    mockNotifications = [makeNotification({ type: 'error' })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-icon')?.textContent).toBe('✕');
  });

  it('shows ℹ as fallback for unknown type', () => {
    // @ts-expect-error — intentionally passing unknown type to test fallback
    mockNotifications = [makeNotification({ type: 'unknown' })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-icon')?.textContent).toBe('ℹ');
  });
});

// ---------------------------------------------------------------------------
// Tests: Dismiss individual
// ---------------------------------------------------------------------------

describe('Dismiss individual notification', () => {
  it('calls removeNotification with correct id when dismiss button is clicked', () => {
    mockNotifications = [makeNotification({ id: 'notif-1', title: 'A' })];
    renderNotificationCenter({ isOpen: true });

    const dismissBtn = container.querySelector('.notification-center-item-dismiss');
    act(() => {
      dismissBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(mockRemoveNotification).toHaveBeenCalledTimes(1);
    expect(mockRemoveNotification).toHaveBeenCalledWith('notif-1');
  });

  it('calls removeNotification for the correct notification when multiple exist', () => {
    const now = Date.now();
    mockNotifications = [
      makeNotification({ id: 'a', title: 'A', createdAt: now }),
      makeNotification({ id: 'b', title: 'B', createdAt: now - 1000 }),
    ];
    renderNotificationCenter({ isOpen: true });

    // Items are reversed: B first, A second in DOM
    const dismissBtns = container.querySelectorAll('.notification-center-item-dismiss');
    // The second button (A) should dismiss 'a'
    act(() => {
      dismissBtns[1]?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(mockRemoveNotification).toHaveBeenCalledWith('a');
  });
});

// ---------------------------------------------------------------------------
// Tests: Dismiss all
// ---------------------------------------------------------------------------

describe('Dismiss all notifications', () => {
  it('calls clearNotifications when Dismiss All button is clicked', () => {
    mockNotifications = [
      makeNotification({ id: 'a', title: 'A' }),
      makeNotification({ id: 'b', title: 'B' }),
    ];
    renderNotificationCenter({ isOpen: true });

    const dismissAllBtn = container.querySelector('.notification-center-dismiss-all');
    act(() => {
      dismissAllBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(mockClearNotifications).toHaveBeenCalledTimes(1);
  });

  it('does not crash when clicking Dismiss All with single notification', () => {
    mockNotifications = [makeNotification()];
    renderNotificationCenter({ isOpen: true });

    const dismissAllBtn = container.querySelector('.notification-center-dismiss-all');
    act(() => {
      dismissAllBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(mockClearNotifications).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: Copy message
// ---------------------------------------------------------------------------

describe('Copy message to clipboard', () => {
  it('calls navigator.clipboard.writeText when copy button is clicked', async () => {
    mockNotifications = [makeNotification({ id: 'notif-1', message: 'Copy me' })];
    renderNotificationCenter({ isOpen: true });

    const copyBtn = container.querySelector('.notification-center-item-copy');
    await act(async () => {
      copyBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('Copy me');
  });

  it('shows "Copied!" feedback after copying', async () => {
    mockNotifications = [makeNotification({ id: 'notif-1', message: 'Copy me' })];
    renderNotificationCenter({ isOpen: true });

    const copyBtn = container.querySelector('.notification-center-item-copy');
    await act(async () => {
      copyBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(copyBtn?.textContent).toBe('Copied!');
  });

  it('resets copy button to emoji after timeout', async () => {
    vi.useFakeTimers();
    mockNotifications = [makeNotification({ id: 'notif-1', message: 'Copy me' })];
    renderNotificationCenter({ isOpen: true });

    const copyBtn = container.querySelector('.notification-center-item-copy');
    await act(async () => {
      copyBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(copyBtn?.textContent).toBe('Copied!');

    // Advance timer past 2000ms
    await act(async () => {
      vi.advanceTimersByTime(2000);
    });

    expect(copyBtn?.textContent).toBe('📋');
    vi.useRealTimers();
  });

  it('does not crash when clipboard.writeText throws an error', async () => {
    const originalClipboard = navigator.clipboard;
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: vi.fn().mockRejectedValue(new Error('Clipboard API unavailable')),
      },
      writable: true,
      configurable: true,
    });

    // Suppress console.error for this test
    const originalError = console.error;
    console.error = vi.fn();

    mockNotifications = [makeNotification({ id: 'notif-1', message: 'Copy me' })];
    renderNotificationCenter({ isOpen: true });

    const copyBtn = container.querySelector('.notification-center-item-copy');
    await act(async () => {
      copyBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Should not throw; button should remain in original state (not show "Copied!")
    expect(copyBtn?.textContent).toBe('📋');

    console.error = originalError;
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
      configurable: true,
    });
  });

  it('only marks the clicked notification as copied, not others', async () => {
    mockNotifications = [
      makeNotification({ id: 'notif-1', message: 'First' }),
      makeNotification({ id: 'notif-2', message: 'Second' }),
    ];
    renderNotificationCenter({ isOpen: true });

    const copyBtns = container.querySelectorAll('.notification-center-item-copy');
    // Items are reversed: notif-2 first in DOM
    await act(async () => {
      copyBtns[0]?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(copyBtns[0]?.textContent).toBe('Copied!');
    expect(copyBtns[1]?.textContent).toBe('📋');
  });
});

// ---------------------------------------------------------------------------
// Tests: Escape key
// ---------------------------------------------------------------------------

describe('Escape key closes panel', () => {
  it('calls onClose when Escape key is pressed', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not call onClose for other keys', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not set up keydown listener when not open', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: false, onClose });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('cleans up keydown listener when component unmounts', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    act(() => {
      root.unmount();
    });

    // After unmount, pressing Escape should not trigger onClose
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Outside click
// ---------------------------------------------------------------------------

describe('Outside click closes panel', () => {
  it('calls onClose when clicking outside the panel', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    act(() => {
      document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not call onClose when clicking inside the panel', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    const panel = container.querySelector('.notification-center')!;
    act(() => {
      panel.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not call onClose when clicking on positionRef element', () => {
    const onClose = vi.fn();
    const bellDiv = document.createElement('div');
    bellDiv.className = 'bell-icon';
    document.body.appendChild(bellDiv);
    const positionRef = { current: bellDiv };

    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose, positionRef });

    act(() => {
      bellDiv.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
    bellDiv.remove();
  });

  it('does not set up mousedown listener when not open', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: false, onClose });

    act(() => {
      document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('cleans up mousedown listener when component unmounts', () => {
    const onClose = vi.fn();
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true, onClose });

    act(() => {
      root.unmount();
    });

    // After unmount, clicking outside should not trigger onClose
    act(() => {
      document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Relative timestamps
// ---------------------------------------------------------------------------

describe('Relative timestamps', () => {
  it('shows "just now" for notifications less than 10 seconds old', () => {
    mockNotifications = [makeNotification({ createdAt: Date.now() - 5000 })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-time')?.textContent).toBe('just now');
  });

  it('shows seconds for notifications between 10-59 seconds old', () => {
    mockNotifications = [makeNotification({ createdAt: Date.now() - 30000 })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-time')?.textContent).toBe('30s ago');
  });

  it('shows minutes for notifications between 1-59 minutes old', () => {
    mockNotifications = [makeNotification({ createdAt: Date.now() - 15 * 60 * 1000 })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-time')?.textContent).toBe('15m ago');
  });

  it('shows hours for notifications between 1-23 hours old', () => {
    mockNotifications = [makeNotification({ createdAt: Date.now() - 3 * 60 * 60 * 1000 })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-time')?.textContent).toBe('3h ago');
  });

  it('shows days for notifications 24+ hours old', () => {
    mockNotifications = [makeNotification({ createdAt: Date.now() - 3 * 24 * 60 * 60 * 1000 })];
    renderNotificationCenter({ isOpen: true });
    expect(container.querySelector('.notification-center-item-time')?.textContent).toBe('3d ago');
  });
});

// ---------------------------------------------------------------------------
// Tests: Button attributes
// ---------------------------------------------------------------------------

describe('Button attributes', () => {
  it('copy button has correct aria-label and title', () => {
    mockNotifications = [makeNotification()];
    renderNotificationCenter({ isOpen: true });
    const copyBtn = container.querySelector('.notification-center-item-copy');
    expect(copyBtn?.getAttribute('aria-label')).toBe('Copy message');
    expect(copyBtn?.getAttribute('title')).toBe('Copy message');
  });

  it('dismiss button has correct aria-label and title', () => {
    mockNotifications = [makeNotification()];
    renderNotificationCenter({ isOpen: true });
    const dismissBtn = container.querySelector('.notification-center-item-dismiss');
    expect(dismissBtn?.getAttribute('aria-label')).toBe('Dismiss notification');
    expect(dismissBtn?.getAttribute('title')).toBe('Dismiss');
  });

  it('all buttons have type="button" to prevent form submission', () => {
    mockNotifications = [makeNotification({ id: 'a' })];
    renderNotificationCenter({ isOpen: true });
    const buttons = container.querySelectorAll('button');
    buttons.forEach((btn) => {
      expect(btn.getAttribute('type')).toBe('button');
    });
    expect(buttons.length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Tests: Multiple notifications
// ---------------------------------------------------------------------------

describe('Multiple notifications', () => {
  it('renders all notification items with correct content', () => {
    const now = Date.now();
    mockNotifications = [
      makeNotification({ id: 'a', type: 'info', title: 'Info Title', message: 'Info message', createdAt: now }),
      makeNotification({ id: 'b', type: 'error', title: 'Error Title', message: 'Error message', createdAt: now - 60000 }),
    ];
    renderNotificationCenter({ isOpen: true });

    const items = container.querySelectorAll('.notification-center-item');
    expect(items).toHaveLength(2);

    // Array is [...notifications].reverse(), so last in array = first in DOM
    // mockNotifications = [a (newest), b (oldest)] → reversed = [b, a]
    const firstItem = items[0];
    expect(firstItem.querySelector('.notification-center-item-title')?.textContent).toBe('Error Title');
    expect(firstItem.querySelector('.notification-center-item-message')?.textContent).toBe('Error message');
    expect(firstItem.querySelector('.notification-center-item-icon')?.textContent).toBe('✕');

    const secondItem = items[1];
    expect(secondItem.querySelector('.notification-center-item-title')?.textContent).toBe('Info Title');
    expect(secondItem.querySelector('.notification-center-item-message')?.textContent).toBe('Info message');
    expect(secondItem.querySelector('.notification-center-item-icon')?.textContent).toBe('ℹ');
  });

  it('each notification item has its own copy and dismiss buttons', () => {
    mockNotifications = [
      makeNotification({ id: 'a' }),
      makeNotification({ id: 'b' }),
      makeNotification({ id: 'c' }),
    ];
    renderNotificationCenter({ isOpen: true });

    const copyButtons = container.querySelectorAll('.notification-center-item-copy');
    const dismissButtons = container.querySelectorAll('.notification-center-item-dismiss');
    expect(copyButtons).toHaveLength(3);
    expect(dismissButtons).toHaveLength(3);
  });

  it('each notification item has a timestamp', () => {
    mockNotifications = [
      makeNotification({ id: 'a', createdAt: Date.now() }),
      makeNotification({ id: 'b', createdAt: Date.now() - 100000 }),
    ];
    renderNotificationCenter({ isOpen: true });

    const times = container.querySelectorAll('.notification-center-item-time');
    expect(times).toHaveLength(2);
    expect(times[0]).not.toBeNull();
    expect(times[1]).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: Re-rendering
// ---------------------------------------------------------------------------

describe('Re-rendering', () => {
  it('updates when notifications are added while open', () => {
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });

    expect(container.querySelector('.notification-center-empty')).not.toBeNull();

    // Simulate notifications being added
    mockNotifications = [makeNotification({ id: 'new-1', title: 'New' })];
    renderNotificationCenter({ isOpen: true });

    expect(container.querySelector('.notification-center-empty')).toBeNull();
    expect(container.querySelector('.notification-center-item-title')?.textContent).toBe('New');
  });

  it('updates when notifications are removed while open', () => {
    mockNotifications = [makeNotification({ id: 'a' })];
    renderNotificationCenter({ isOpen: true });

    expect(container.querySelectorAll('.notification-center-item')).toHaveLength(1);

    // Simulate all notifications being dismissed
    mockNotifications = [];
    renderNotificationCenter({ isOpen: true });

    expect(container.querySelector('.notification-center-empty')).not.toBeNull();
    expect(container.querySelector('.notification-center-list')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// End of tests
// ---------------------------------------------------------------------------
