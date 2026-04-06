// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { NotificationProvider, useNotifications } from '../contexts/NotificationContext';
import { Levels, setMinLevel, getMinLevel, error, warn, info, success, debugLog, useLog } from '../utils/log';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

Object.defineProperty(globalThis, 'crypto', {
  value: {
    randomUUID: () => 'test-uuid-' + Math.random().toString(36).slice(2),
  },
  writable: true,
  configurable: true,
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
  jest.useFakeTimers();
  jest.clearAllMocks();
  // Suppress console output in tests
  jest.spyOn(console, 'error').mockImplementation(() => {});
  jest.spyOn(console, 'warn').mockImplementation(() => {});
  jest.spyOn(console, 'info').mockImplementation(() => {});
  jest.spyOn(console, 'log').mockImplementation(() => {});

  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  jest.useRealTimers();
  jest.restoreAllMocks();
  setMinLevel(Levels.debug); // safety net: reset minLevel even if a test throws before its inline reset
});

// Create a test component that captures the log + notification functions
function createHookRunner() {
  let logRef: ReturnType<typeof useLog> | null = null;
  let notifRef: ReturnType<typeof useNotifications> | null = null;

  function Inner() {
    notifRef = useNotifications();
    logRef = useLog();
    return null;
  }

  function App() {
    return createElement(NotificationProvider, null, createElement(Inner));
  }

  return { logRef: () => logRef, notifRef: () => notifRef, App };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useLog', () => {
  it('returns a log object with debug, error, warn, info, and success methods', () => {
    const { App, logRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    expect(log).toHaveProperty('debug');
    expect(log).toHaveProperty('error');
    expect(log).toHaveProperty('warn');
    expect(log).toHaveProperty('info');
    expect(log).toHaveProperty('success');
  });

  it('error() calls console.error AND adds an error notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.error('Test error message', { title: 'Test Title' });
    });

    expect(console.error).toHaveBeenCalledWith('Test error message');

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('error');
    expect(notifs[0].title).toBe('Test Title');
    expect(notifs[0].message).toBe('Test error message');
  });

  it('error() uses default title when no title provided', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.error('Test message');
    });

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].title).toBe('Application Log');
  });

  it('warn() calls console.warn AND adds a warning notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.warn('Test warning', { title: 'Warning Title' });
    });

    expect(console.warn).toHaveBeenCalledWith('Test warning');

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('warning');
    expect(notifs[0].title).toBe('Warning Title');
  });

  it('info() calls console.info AND adds info notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.info('Test info', { title: 'Info Title' });
    });

    expect(console.info).toHaveBeenCalledWith('Test info');

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('info');
    expect(notifs[0].message).toBe('Test info');
  });

  it('success() calls console.log AND adds success notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.success('Test success', { title: 'Success Title' });
    });

    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Test success');

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('success');
    expect(notifs[0].title).toBe('Success Title');
  });

  it('debug() only logs to console, does NOT add notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    const log = logRef();
    act(() => {
      log.debug('Debug message', { extra: 'data' });
    });

    expect(console.log).toHaveBeenCalledWith('Debug message', { extra: 'data' });

    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// Log level filtering tests
// ---------------------------------------------------------------------------

describe('log level configuration', () => {
  it('getMinLevel() returns a number', () => {
    const level = getMinLevel();
    expect(typeof level).toBe('number');
  });

  it('setMinLevel() updates the minimum level', () => {
    setMinLevel(Levels.error);
    expect(getMinLevel()).toBe(Levels.error);
    setMinLevel(Levels.debug); // reset
  });

  it('default level is debug (0) in non-production', () => {
    // In test env (NODE_ENV !== 'production'), default should be 0
    expect(getMinLevel()).toBe(Levels.debug);
  });
});

describe('Levels constant', () => {
  it('has correct numeric values for all levels', () => {
    expect(Levels.debug).toBe(0);
    expect(Levels.info).toBe(1);
    expect(Levels.success).toBe(2);
    expect(Levels.warn).toBe(3);
    expect(Levels.error).toBe(4);
  });
});

describe('plain functions respect minLevel', () => {
  it('error logs when minLevel is set to error (4)', () => {
    setMinLevel(Levels.error);
    error('test');
    expect(console.error).toHaveBeenCalled();
    setMinLevel(Levels.debug); // reset
  });

  it('warn is suppressed when minLevel is error', () => {
    setMinLevel(Levels.error);
    warn('test');
    expect(console.warn).not.toHaveBeenCalled();
    setMinLevel(Levels.debug); // reset
  });

  it('info is suppressed when minLevel is error', () => {
    setMinLevel(Levels.error);
    info('test');
    expect(console.info).not.toHaveBeenCalled();
    setMinLevel(Levels.debug); // reset
  });

  it('success is suppressed when minLevel is error', () => {
    setMinLevel(Levels.error);
    success('test');
    expect(console.log).not.toHaveBeenCalled();
    setMinLevel(Levels.debug); // reset
  });

  it('debugLog is suppressed when minLevel is error', () => {
    setMinLevel(Levels.error);
    debugLog('test');
    expect(console.log).not.toHaveBeenCalled();
    setMinLevel(Levels.debug); // reset
  });

  it('warn and error both log when minLevel is warn (3), but info/success/debug are suppressed', () => {
    setMinLevel(Levels.warn);

    warn('warn-msg');
    error('error-msg');
    info('info-msg');
    success('success-msg');
    debugLog('debug-msg');

    expect(console.warn).toHaveBeenCalledWith('warn-msg');
    expect(console.error).toHaveBeenCalledWith('error-msg');
    expect(console.info).not.toHaveBeenCalled();
    // success and debugLog both use console.log — neither should fire
    expect(console.log).not.toHaveBeenCalled();

    setMinLevel(Levels.debug); // reset
  });
});

describe('useLog hook respects minLevel for console but always sends notifications', () => {
  it('warn does not write to console at error level but still sends notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    setMinLevel(Levels.error);

    const log = logRef();
    act(() => {
      log.warn('suppressed warn', { title: 'Warn Title' });
    });

    // Console should be suppressed (warn level < error)
    expect(console.warn).not.toHaveBeenCalled();

    // But notification should still fire
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('warning');
    expect(notifs[0].message).toBe('suppressed warn');

    setMinLevel(Levels.debug); // reset
  });

  it('info does not write to console at warn level but still sends notification', () => {
    const { App, logRef, notifRef } = createHookRunner();

    act(() => {
      root.render(createElement(App));
    });

    setMinLevel(Levels.warn);

    const log = logRef();
    act(() => {
      log.info('suppressed info', { title: 'Info Title' });
    });

    // Console should be suppressed (info level < warn)
    expect(console.info).not.toHaveBeenCalled();

    // But notification should still fire
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('info');
    expect(notifs[0].message).toBe('suppressed info');

    setMinLevel(Levels.debug); // reset
  });
});
