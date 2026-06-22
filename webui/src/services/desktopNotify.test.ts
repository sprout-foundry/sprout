/**
 * desktopNotify.test.ts — Unit tests for the desktop notification service.
 *
 * The browser Notification API does not exist in jsdom, so we mock the global
 * before any imports of the module under test.  Tests cover:
 *
 * - getPermission()  — reflects Notification.permission under all states
 * - requestPermission() — short-circuits granted/denied, delegates when default
 * - setEnabled / isEnabled_ — flip internal toggle, guard all callers
 * - notify()         — early-exits on bad state, constructs Notification on success
 * - notifyIfHidden() — delegates to notify() only when the tab is backgrounded
 * - Permission transitions — default → granted / denied round-trips
 */

// ---------------------------------------------------------------------------
// Mock the Notification API — doesn't exist in jsdom
// ---------------------------------------------------------------------------

// Use vi.hoisted so everything is available before module evaluation
const { MockNotification, mockRequestPermission } = vi.hoisted(() => {
  const reqPerm = vi.fn().mockResolvedValue<'default' | 'granted' | 'denied'>('granted');
  const fn = vi.fn().mockImplementation((_title: string, _options?: NotificationOptions) => {
    return {
      title: _title,
      body: _options?.body,
      icon: _options?.icon,
      tag: _options?.tag,
      onclick: null as (() => void) | null,
    };
  });
  // Attach static properties so getPermission / requestPermission work on the constructor
  fn.permission = 'default' as 'default' | 'granted' | 'denied';
  fn.requestPermission = reqPerm;
  return { MockNotification: fn, mockRequestPermission: reqPerm };
});

// Stub the global — MockNotification IS the constructor with static properties
vi.stubGlobal('Notification', MockNotification);

// Also stub window.focus for onclick assertions
vi.stubGlobal('window', {
  ...window,
  focus: vi.fn(),
});

import * as desktopNotify from './desktopNotify';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Reset shared state between tests so we don't bleed mocks. */
function resetState() {
  vi.clearAllMocks();
  // Reset the module-level enabled flag (default is true)
  desktopNotify.setEnabled(true);
  // Reset the global permission to its default
  (Notification as any).permission = 'default';
  mockRequestPermission.mockResolvedValue('granted');
  // Reset document.hidden (jsdom default is false — tab visible)
  Object.defineProperty(document, 'hidden', {
    value: false,
    writable: true,
  });
}

// ---------------------------------------------------------------------------
// Tests: getPermission()
// ---------------------------------------------------------------------------

describe('getPermission', () => {
  beforeEach(() => resetState());

  it('returns "default" when Notification.permission is default', () => {
    (Notification as any).permission = 'default';
    expect(desktopNotify.getPermission()).toBe('default');
  });

  it('returns "granted" when Notification.permission is granted', () => {
    (Notification as any).permission = 'granted';
    expect(desktopNotify.getPermission()).toBe('granted');
  });

  it('returns "denied" when Notification.permission is denied', () => {
    (Notification as any).permission = 'denied';
    expect(desktopNotify.getPermission()).toBe('denied');
  });

  it('returns "denied" when Notification global is undefined', () => {
    // Temporarily remove the global
    const orig = globalThis.Notification;
    // @ts-ignore — deliberate deletion
    delete globalThis.Notification;
    expect(desktopNotify.getPermission()).toBe('denied');
    // Restore
    globalThis.Notification = orig;
  });
});

// ---------------------------------------------------------------------------
// Tests: requestPermission()
// ---------------------------------------------------------------------------

describe('requestPermission', () => {
  beforeEach(() => resetState());

  it('returns "granted" immediately when permission is already granted', async () => {
    (Notification as any).permission = 'granted';
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('granted');
    expect(mockRequestPermission).not.toHaveBeenCalled();
  });

  it('returns "denied" immediately when permission is already denied', async () => {
    (Notification as any).permission = 'denied';
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('denied');
    expect(mockRequestPermission).not.toHaveBeenCalled();
  });

  it('calls Notification.requestPermission() when permission is default', async () => {
    (Notification as any).permission = 'default';
    mockRequestPermission.mockResolvedValue('granted');
    const result = await desktopNotify.requestPermission();
    expect(mockRequestPermission).toHaveBeenCalledTimes(1);
    expect(result).toBe('granted');
  });

  it('returns "denied" when requestPermission resolves to denied', async () => {
    (Notification as any).permission = 'default';
    mockRequestPermission.mockResolvedValue('denied');
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('denied');
  });

  it('returns "denied" when Notification global is undefined', async () => {
    const orig = globalThis.Notification;
    // @ts-ignore
    delete globalThis.Notification;
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('denied');
    globalThis.Notification = orig;
  });
});

// ---------------------------------------------------------------------------
// Tests: setEnabled / isEnabled_
// ---------------------------------------------------------------------------

describe('setEnabled / isEnabled_', () => {
  beforeEach(() => resetState());

  it('returns true after setEnabled(true)', () => {
    desktopNotify.setEnabled(true);
    expect(desktopNotify.isEnabled_()).toBe(true);
  });

  it('returns false after setEnabled(false)', () => {
    desktopNotify.setEnabled(false);
    expect(desktopNotify.isEnabled_()).toBe(false);
  });

  it('toggles between true and false', () => {
    desktopNotify.setEnabled(true);
    expect(desktopNotify.isEnabled_()).toBe(true);
    desktopNotify.setEnabled(false);
    expect(desktopNotify.isEnabled_()).toBe(false);
    desktopNotify.setEnabled(true);
    expect(desktopNotify.isEnabled_()).toBe(true);
  });

  it('defaults to enabled (true) after reset', () => {
    // resetState sets enabled to true
    expect(desktopNotify.isEnabled_()).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: notify()
// ---------------------------------------------------------------------------

describe('notify', () => {
  beforeEach(() => resetState());

  it('does nothing when enabled is false', () => {
    desktopNotify.setEnabled(false);
    (Notification as any).permission = 'granted';
    desktopNotify.notify('Title', 'Body');
    expect(MockNotification).not.toHaveBeenCalled();
  });

  it('does nothing when Notification is undefined', () => {
    const orig = globalThis.Notification;
    // @ts-ignore
    delete globalThis.Notification;
    desktopNotify.notify('Title', 'Body');
    expect(MockNotification).not.toHaveBeenCalled();
    globalThis.Notification = orig;
  });

  it('does nothing when permission is not granted', () => {
    (Notification as any).permission = 'default';
    desktopNotify.notify('Title', 'Body');
    expect(MockNotification).not.toHaveBeenCalled();

    (Notification as any).permission = 'denied';
    desktopNotify.notify('Title', 'Body');
    // Still not called
    expect(MockNotification).not.toHaveBeenCalled();
  });

  it('creates a new Notification with correct title, body, icon, and tag', () => {
    (Notification as any).permission = 'granted';
    desktopNotify.setEnabled(true);
    desktopNotify.notify('Sprout', 'Task complete');

    expect(MockNotification).toHaveBeenCalledTimes(1);
    expect(MockNotification).toHaveBeenCalledWith('Sprout', {
      body: 'Task complete',
      icon: '/favicon.ico',
      tag: 'sprout-notification',
    });
  });

  it('creates a Notification without body when body is omitted', () => {
    (Notification as any).permission = 'granted';
    desktopNotify.setEnabled(true);
    desktopNotify.notify('Sprout');

    expect(MockNotification).toHaveBeenCalledTimes(1);
    expect(MockNotification).toHaveBeenCalledWith('Sprout', {
      body: undefined,
      icon: '/favicon.ico',
      tag: 'sprout-notification',
    });
  });

  it('sets onclick handler that focuses the window', () => {
    (Notification as any).permission = 'granted';
    desktopNotify.setEnabled(true);
    desktopNotify.notify('Sprout', 'Body');

    // MockNotification captures the return value
    const created = MockNotification.mock.results[0].value;
    expect(created.onclick).toBeInstanceOf(Function);

    // Simulate clicking the notification
    created.onclick();

    expect(window.focus).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: notifyIfHidden()
// ---------------------------------------------------------------------------

describe('notifyIfHidden', () => {
  beforeEach(() => resetState());

  it('does nothing when enabled is false', () => {
    desktopNotify.setEnabled(false);
    (document as any).hidden = true;
    desktopNotify.notifyIfHidden('Title', 'Body');
    expect(MockNotification).not.toHaveBeenCalled();
  });

  it('does nothing when document.hidden is false (tab is visible)', () => {
    desktopNotify.setEnabled(true);
    (Notification as any).permission = 'granted';
    (document as any).hidden = false;
    desktopNotify.notifyIfHidden('Title', 'Body');
    expect(MockNotification).not.toHaveBeenCalled();
  });

  it('calls notify() when document.hidden is true and permission is granted', () => {
    desktopNotify.setEnabled(true);
    (Notification as any).permission = 'granted';
    (document as any).hidden = true;
    desktopNotify.notifyIfHidden('Sprout', 'Input required');

    expect(MockNotification).toHaveBeenCalledTimes(1);
    expect(MockNotification).toHaveBeenCalledWith('Sprout', {
      body: 'Input required',
      icon: '/favicon.ico',
      tag: 'sprout-notification',
    });
  });

  it('does NOT fire notification when tab is visible even with granted permission', () => {
    desktopNotify.setEnabled(true);
    (Notification as any).permission = 'granted';
    (document as any).hidden = false;
    desktopNotify.notifyIfHidden('Sprout', 'Should not appear');
    expect(MockNotification).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Permission flow integration
// ---------------------------------------------------------------------------

describe('permission flow integration', () => {
  beforeEach(() => resetState());

  it('requestPermission() transitions from default to granted', async () => {
    (Notification as any).permission = 'default';
    mockRequestPermission.mockResolvedValue('granted');
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('granted');
    // After granting, getPermission reflects the result
    (Notification as any).permission = 'granted';
    expect(desktopNotify.getPermission()).toBe('granted');
    // Subsequent requestPermission short-circuits
    const result2 = await desktopNotify.requestPermission();
    expect(result2).toBe('granted');
    expect(mockRequestPermission).toHaveBeenCalledTimes(1); // only the first call
  });

  it('requestPermission() transitions from default to denied', async () => {
    (Notification as any).permission = 'default';
    mockRequestPermission.mockResolvedValue('denied');
    const result = await desktopNotify.requestPermission();
    expect(result).toBe('denied');
    // After denial, subsequent calls short-circuit
    (Notification as any).permission = 'denied';
    const result2 = await desktopNotify.requestPermission();
    expect(result2).toBe('denied');
    expect(mockRequestPermission).toHaveBeenCalledTimes(1); // only the first call
  });
});
