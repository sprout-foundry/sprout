// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { NotificationProvider, useNotifications } from '../contexts/NotificationContext';
import { useLog } from '../utils/log';

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
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    expect(log).toHaveProperty('debug');
    expect(log).toHaveProperty('error');
    expect(log).toHaveProperty('warn');
    expect(log).toHaveProperty('info');
    expect(log).toHaveProperty('success');
  });

  it('error() calls console.error AND adds an error notification', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.error('Test error message', { title: 'Test Title' }); });
    
    expect(console.error).toHaveBeenCalledWith('Test error message');
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('error');
    expect(notifs[0].title).toBe('Test Title');
    expect(notifs[0].message).toBe('Test error message');
  });

  it('error() uses default title when no title provided', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.error('Test message'); });
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].title).toBe('Application Log');
  });

  it('warn() calls console.warn AND adds a warning notification', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.warn('Test warning', { title: 'Warning Title' }); });
    
    expect(console.warn).toHaveBeenCalledWith('Test warning');
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('warning');
    expect(notifs[0].title).toBe('Warning Title');
  });

  it('info() calls console.info AND adds info notification', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.info('Test info', { title: 'Info Title' }); });
    
    expect(console.info).toHaveBeenCalledWith('Test info');
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('info');
    expect(notifs[0].message).toBe('Test info');
  });

  it('success() calls console.log AND adds success notification', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.success('Test success', { title: 'Success Title' }); });
    
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Test success');
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(1);
    expect(notifs[0].type).toBe('success');
    expect(notifs[0].title).toBe('Success Title');
  });

  it('debug() only logs to console, does NOT add notification', () => {
    const { App, logRef, notifRef } = createHookRunner();
    
    act(() => { root.render(createElement(App)); });
    
    const log = logRef();
    act(() => { log.debug('Debug message', { extra: 'data' }); });
    
    expect(console.log).toHaveBeenCalledWith('Debug message', { extra: 'data' });
    
    const notifs = notifRef().notifications;
    expect(notifs).toHaveLength(0);
  });
});
