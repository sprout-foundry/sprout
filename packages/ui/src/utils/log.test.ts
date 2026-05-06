import { vi } from 'vitest';
import { createElement, StrictMode } from 'react';
import { render, act } from '@testing-library/react';
import { useNotifications } from '../contexts/NotificationContext';
import {
  debugLog, error, warn, info, success, setMinLevel, getMinLevel, useLog,
  Levels, DEFAULT_NOTIFICATION_DURATION, DEFAULT_LOG_TITLE
} from './log';

// Shared mock reference for addNotification across useLog tests
let addNotificationMock = vi.fn();

// Mock NotificationContext for useLog tests
vi.mock('../contexts/NotificationContext', () => ({
  useNotifications: vi.fn(() => ({
    addNotification: addNotificationMock,
  })),
}));

// ── debugLog ─────────────────────────────────────────────────────────────

describe('debugLog', () => {
  beforeEach(() => {
    vi.spyOn(console, 'log').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs to console when minLevel allows', () => {
    setMinLevel(0);
    debugLog('Test message');
    expect(console.log).toHaveBeenCalledWith('Test message');
  });

  it('does not log when minLevel is higher than debug', () => {
    setMinLevel(1);
    debugLog('Test message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('logs multiple arguments', () => {
    setMinLevel(0);
    debugLog('arg1', 'arg2', { key: 'value' });
    expect(console.log).toHaveBeenCalledWith('arg1', 'arg2', { key: 'value' });
  });

  it('does not log in production when NODE_ENV is production', () => {
    const originalEnv = process.env.NODE_ENV;
    process.env.NODE_ENV = 'production';
    debugLog('Test message');
    expect(console.log).not.toHaveBeenCalled();
    process.env.NODE_ENV = originalEnv;
  });

  it('logs in development when NODE_ENV is not production', () => {
    const originalEnv = process.env.NODE_ENV;
    process.env.NODE_ENV = 'development';
    setMinLevel(0);
    debugLog('Test message');
    expect(console.log).toHaveBeenCalledWith('Test message');
    process.env.NODE_ENV = originalEnv;
  });
});

// ── error ────────────────────────────────────────────────────────────────

describe('error', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs to console.error when minLevel allows', () => {
    setMinLevel(4);
    error('Error message');
    expect(console.error).toHaveBeenCalledWith('Error message');
  });

  it('does not log when minLevel is higher than error', () => {
    setMinLevel(5);
    error('Error message');
    expect(console.error).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(4);
    error('Error message', { title: 'Error Title' });
    expect(console.error).toHaveBeenCalledWith('Error message');
  });

  it('warns about showNotification being unsupported in non-React context', () => {
    setMinLevel(0);
    const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    error('Error message', { showNotification: true });
    expect(console.error).toHaveBeenCalledWith('Error message');
    expect(consoleWarnSpy).toHaveBeenCalledWith('showNotification option is only available when using the useLog() hook inside React components');
  });
});

// ── warn ─────────────────────────────────────────────────────────────────

describe('warn', () => {
  beforeEach(() => {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs to console.warn when minLevel allows', () => {
    setMinLevel(3);
    warn('Warning message');
    expect(console.warn).toHaveBeenCalledWith('Warning message');
  });

  it('does not log when minLevel is higher than warn', () => {
    setMinLevel(4);
    warn('Warning message');
    expect(console.warn).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(3);
    warn('Warning message', { title: 'Warning Title' });
    expect(console.warn).toHaveBeenCalledWith('Warning message');
  });

  it('warns about showNotification being unsupported in non-React context', () => {
    setMinLevel(0);
    const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    warn('Warning message', { showNotification: true });
    // warn() logs to console.warn, then also warns about showNotification
    expect(consoleWarnSpy).toHaveBeenCalled();
    expect(consoleWarnSpy).toHaveBeenCalledWith('showNotification option is only available when using the useLog() hook inside React components');
  });
});

// ── info ─────────────────────────────────────────────────────────────────

describe('info', () => {
  beforeEach(() => {
    vi.spyOn(console, 'info').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs to console.info when minLevel allows', () => {
    setMinLevel(1);
    info('Info message');
    expect(console.info).toHaveBeenCalledWith('Info message');
  });

  it('does not log when minLevel is higher than info', () => {
    setMinLevel(2);
    info('Info message');
    expect(console.info).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(1);
    info('Info message', { title: 'Info Title' });
    expect(console.info).toHaveBeenCalledWith('Info message');
  });

  it('warns about showNotification being unsupported in non-React context', () => {
    setMinLevel(0);
    const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    info('Info message', { showNotification: true });
    expect(console.info).toHaveBeenCalledWith('Info message');
    expect(consoleWarnSpy).toHaveBeenCalledWith('showNotification option is only available when using the useLog() hook inside React components');
  });
});

// ── success ──────────────────────────────────────────────────────────────

describe('success', () => {
  beforeEach(() => {
    vi.spyOn(console, 'log').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs to console.log with [SUCCESS] prefix', () => {
    setMinLevel(2);
    success('Success message');
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Success message');
  });

  it('does not log when minLevel is higher than success', () => {
    setMinLevel(3);
    success('Success message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(2);
    success('Success message', { title: 'Success Title' });
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Success message');
  });

  it('warns about showNotification being unsupported in non-React context', () => {
    setMinLevel(0);
    const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    success('Success message', { showNotification: true });
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Success message');
    expect(consoleWarnSpy).toHaveBeenCalledWith('showNotification option is only available when using the useLog() hook inside React components');
  });
});

// ── setMinLevel / getMinLevel ─────────────────────────────────────────────

describe('setMinLevel and getMinLevel', () => {
  it('sets and gets minimum log level', () => {
    setMinLevel(2);
    expect(getMinLevel()).toBe(2);
  });

  it('can change min level multiple times', () => {
    setMinLevel(0);
    expect(getMinLevel()).toBe(0);
    setMinLevel(3);
    expect(getMinLevel()).toBe(3);
    setMinLevel(4);
    expect(getMinLevel()).toBe(4);
  });

  it('handles invalid level values', () => {
    setMinLevel(-1);
    expect(getMinLevel()).toBe(-1);
    setMinLevel(999);
    expect(getMinLevel()).toBe(999);
  });
});

// ── log level hierarchy ──────────────────────────────────────────────────

describe('log level hierarchy', () => {
  beforeEach(() => {
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'info').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('logs all at level 0', () => {
    setMinLevel(0);
    debugLog('debug');
    info('info');
    warn('warn');
    error('error');
    success('success');
    expect(console.log).toHaveBeenCalled(); // debug + success
    expect(console.info).toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs info and above at level 1', () => {
    setMinLevel(1);
    debugLog('debug');
    info('info');
    warn('warn');
    error('error');
    success('success');
    expect(console.log).toHaveBeenCalledTimes(1); // only success
    expect(console.info).toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs success and above at level 2', () => {
    setMinLevel(2);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).toHaveBeenCalledTimes(1); // only success
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs warn and above at level 3', () => {
    setMinLevel(3);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs only error at level 4', () => {
    setMinLevel(4);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).not.toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs nothing at level 5 or higher', () => {
    setMinLevel(5);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).not.toHaveBeenCalled();
    expect(console.error).not.toHaveBeenCalled();
  });
});

// ── Levels constant ──────────────────────────────────────────────────────

describe('Levels constant', () => {
  it('has correct numeric values', () => {
    expect(Levels.debug).toBe(0);
    expect(Levels.info).toBe(1);
    expect(Levels.success).toBe(2);
    expect(Levels.warn).toBe(3);
    expect(Levels.error).toBe(4);
  });
});

// ── useLog hook ──────────────────────────────────────────────────────────

/**
 * Test helper component that uses useLog and exposes its methods.
 */
function LogTester({ method, message, options }: {
  method: 'debug' | 'error' | 'warn' | 'info' | 'success';
  message: string;
  options?: any;
}) {
  const log = useLog();
  (log as any)[method](message, options);
  return null;
}

describe('useLog hook', () => {
  beforeEach(() => {
    addNotificationMock = vi.fn();
    // Reset the mock's return value so useNotifications returns our fresh mock
    (useNotifications as ReturnType<typeof vi.fn>).mockImplementation(() => ({
      addNotification: addNotificationMock,
    }));
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'info').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('returns log object with all methods', () => {
    let capturedLog: ReturnType<typeof useLog> | null = null;
    const CaptureLog = () => {
      capturedLog = useLog();
      return null;
    };
    render(createElement(StrictMode, null, createElement(CaptureLog)));
    expect(capturedLog).not.toBeNull();
    expect(typeof capturedLog!.debug).toBe('function');
    expect(typeof capturedLog!.error).toBe('function');
    expect(typeof capturedLog!.warn).toBe('function');
    expect(typeof capturedLog!.info).toBe('function');
    expect(typeof capturedLog!.success).toBe('function');
  });

  it('log.error calls addNotification with error type', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'error',
        message: 'Error from hook',
        options: { title: 'Custom Error Title', duration: 3000 },
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('error', 'Custom Error Title', 'Error from hook', 3000);
  });

  it('log.error uses default title when none provided', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'error',
        message: 'Error from hook',
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('error', DEFAULT_LOG_TITLE, 'Error from hook', undefined);
  });

  it('log.warn calls addNotification with warning type', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'warn',
        message: 'Warning from hook',
        options: { title: 'Custom Warn Title' },
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('warning', 'Custom Warn Title', 'Warning from hook', undefined);
  });

  it('log.warn uses default title when none provided', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'warn',
        message: 'Warning from hook',
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('warning', DEFAULT_LOG_TITLE, 'Warning from hook', undefined);
  });

  it('log.info calls addNotification with info type', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'info',
        message: 'Info from hook',
        options: { title: 'Custom Info Title' },
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('info', 'Custom Info Title', 'Info from hook', undefined);
  });

  it('log.info uses default title when none provided', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'info',
        message: 'Info from hook',
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('info', DEFAULT_LOG_TITLE, 'Info from hook', undefined);
  });

  it('log.success calls addNotification with success type', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'success',
        message: 'Success from hook',
        options: { title: 'Custom Success Title' },
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('success', 'Custom Success Title', 'Success from hook', undefined);
  });

  it('log.success uses default title when none provided', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'success',
        message: 'Success from hook',
      })));
    });
    expect(addNotificationMock).toHaveBeenCalledWith('success', DEFAULT_LOG_TITLE, 'Success from hook', undefined);
  });

  it('log.debug delegates to debugLog', () => {
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'debug',
        message: 'Debug from hook',
        options: { key: 'value' },
      })));
    });
    expect(console.log).toHaveBeenCalledWith('Debug from hook', { key: 'value' });
  });

  it('log.error respects minLevel for console output', () => {
    setMinLevel(5);
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'error',
        message: 'Should not log to console',
      })));
    });
    expect(console.error).not.toHaveBeenCalled();
    expect(addNotificationMock).toHaveBeenCalled();
  });

  it('log.warn respects minLevel for console output', () => {
    setMinLevel(4);
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'warn',
        message: 'Should not log to console',
      })));
    });
    expect(console.warn).not.toHaveBeenCalled();
    expect(addNotificationMock).toHaveBeenCalled();
  });

  it('log.info respects minLevel for console output', () => {
    setMinLevel(2);
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'info',
        message: 'Should not log to console',
      })));
    });
    expect(console.info).not.toHaveBeenCalled();
    expect(addNotificationMock).toHaveBeenCalled();
  });

  it('log.success respects minLevel for console output', () => {
    setMinLevel(3);
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'success',
        message: 'Should not log to console',
      })));
    });
    expect(console.log).not.toHaveBeenCalled();
    expect(addNotificationMock).toHaveBeenCalled();
  });

  it('notifications always fire regardless of minLevel', () => {
    setMinLevel(5);
    act(() => {
      render(createElement(StrictMode, null, createElement(LogTester, {
        method: 'error',
        message: 'Always notify',
      })));
    });
    expect(addNotificationMock).toHaveBeenCalled();
  });
});

// ── exports ──────────────────────────────────────────────────────────────

describe('exports', () => {
  it('exports DEFAULT_NOTIFICATION_DURATION', () => {
    expect(DEFAULT_NOTIFICATION_DURATION).toBe(5000);
  });

  it('exports DEFAULT_LOG_TITLE', () => {
    expect(DEFAULT_LOG_TITLE).toBe('Application Log');
  });
});
