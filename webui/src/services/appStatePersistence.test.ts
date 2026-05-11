/**
 * Tests for appStatePersistence
 *
 * Tests getUIContextScope, getAppStateStorageKey, and loadPersistedAppState.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Mocks (before imports) ──────────────────────────────────────────

vi.mock('../constants/app', () => ({
  APP_STATE_STORAGE_KEY: 'sprout:webui:state:v2',
  INSTANCE_PID_STORAGE_KEY: 'sprout:webui:instancePid',
  INSTANCE_SWITCH_RESET_KEY: 'sprout:webui:instanceSwitchReset',
  MAX_PERSISTED_LOGS: 1000,
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('./notificationBus', () => ({
  notificationBus: {
    notify: vi.fn(),
  },
}));

// ── Imports ──────────────────────────────────────────────────────────

import { getUIContextScope, getAppStateStorageKey, loadPersistedAppState } from './appStatePersistence';
import { APP_STATE_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY, INSTANCE_SWITCH_RESET_KEY } from '../constants/app';
import { notificationBus } from './notificationBus';

// ── localStorage / sessionStorage mock factories ─────────────────────

interface StorageMock {
  store: Record<string, string>;
  getItem: ReturnType<typeof vi.fn>;
  setItem: ReturnType<typeof vi.fn>;
  removeItem: ReturnType<typeof vi.fn>;
  clear: ReturnType<typeof vi.fn>;
}

function createStorageMock(): StorageMock {
  const store: Record<string, string> = {};
  return {
    store,
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      Object.keys(store).forEach((k) => delete store[k]);
    }),
  };
}

// ── Helpers ──────────────────────────────────────────────────────────

function setLocationPathname(path: string) {
  Object.defineProperty(window, 'location', {
    value: { pathname: path },
    writable: true,
    configurable: true,
  });
}

function setLocalStorageMock(storage: StorageMock) {
  Object.defineProperty(window, 'localStorage', {
    value: {
      getItem: storage.getItem,
      setItem: storage.setItem,
      removeItem: storage.removeItem,
      clear: storage.clear,
    },
    writable: true,
    configurable: true,
  });
}

function setSessionStorageMock(storage: StorageMock | undefined) {
  if (storage) {
    Object.defineProperty(window, 'sessionStorage', {
      value: {
        getItem: storage.getItem,
        setItem: storage.setItem,
        removeItem: storage.removeItem,
        clear: storage.clear,
      },
      writable: true,
      configurable: true,
    });
  } else {
    Object.defineProperty(window, 'sessionStorage', {
      value: undefined,
      writable: true,
      configurable: true,
    });
  }
}

// ── getUIContextScope ────────────────────────────────────────────────

describe('getUIContextScope', () => {
  describe('local context', () => {
    it('returns "local" for root path "/"', () => {
      setLocationPathname('/');
      expect(getUIContextScope()).toBe('local');
    });

    it('returns "local" for path "/chat"', () => {
      setLocationPathname('/chat');
      expect(getUIContextScope()).toBe('local');
    });

    it('returns "local" for path "/billing"', () => {
      setLocationPathname('/billing');
      expect(getUIContextScope()).toBe('local');
    });

    it('returns "local" for empty pathname', () => {
      setLocationPathname('');
      expect(getUIContextScope()).toBe('local');
    });
  });

  describe('SSH context', () => {
    it('returns "ssh:key" for path "/ssh/key"', () => {
      setLocationPathname('/ssh/my-session-key');
      expect(getUIContextScope()).toBe('ssh:my-session-key');
    });

    it('returns "ssh:key" for path "/ssh/key/"', () => {
      setLocationPathname('/ssh/my-session-key/');
      expect(getUIContextScope()).toBe('ssh:my-session-key');
    });

    it('returns "ssh:key" for path "/ssh/key/chat"', () => {
      setLocationPathname('/ssh/my-session-key/chat');
      expect(getUIContextScope()).toBe('ssh:my-session-key');
    });

    it('returns "ssh:unknown" for path "/ssh/"', () => {
      setLocationPathname('/ssh/');
      expect(getUIContextScope()).toBe('ssh:unknown');
    });

    it('returns "ssh:key" for nested paths like "/ssh/abc123/editor"', () => {
      setLocationPathname('/ssh/abc123/editor');
      expect(getUIContextScope()).toBe('ssh:abc123');
    });
  });
});

// ── getAppStateStorageKey ────────────────────────────────────────────

describe('getAppStateStorageKey', () => {
  let ls: StorageMock;

  beforeEach(() => {
    ls = createStorageMock();
    setLocalStorageMock(ls);
    setLocationPathname('/');
  });

  it('returns key with format "APP_STATE_STORAGE_KEY:pid:scope"', () => {
    ls.setItem(INSTANCE_PID_STORAGE_KEY, '12345');
    expect(getAppStateStorageKey()).toBe('sprout:webui:state:v2:12345:local');
  });

  it('includes SSH scope when on SSH path', () => {
    ls.setItem(INSTANCE_PID_STORAGE_KEY, '12345');
    setLocationPathname('/ssh/my-session');
    expect(getAppStateStorageKey()).toBe('sprout:webui:state:v2:12345:ssh:my-session');
  });

  it('falls back to "default" when instancePid not in localStorage', () => {
    // getItem returns null when key not set
    expect(getAppStateStorageKey()).toBe('sprout:webui:state:v2:default:local');
  });
});

// ── loadPersistedAppState ────────────────────────────────────────────

describe('loadPersistedAppState', () => {
  let ls: StorageMock;
  let ss: StorageMock;

  beforeEach(() => {
    ls = createStorageMock();
    ss = createStorageMock();
    setLocalStorageMock(ls);
    setSessionStorageMock(ss);
    setLocationPathname('/');
  });

  afterEach(() => {
    ls.getItem.mockClear();
    ls.setItem.mockClear();
    ls.removeItem.mockClear();
    ss.getItem.mockClear();
    ss.setItem.mockClear();
    ss.removeItem.mockClear();
  });

  it('returns null when window is undefined (SSR)', () => {
    const originalWindow = global.window;
    // @ts-expect-error SSR guard test
    delete global.window;
    const result = loadPersistedAppState();
    expect(result).toBeNull();
    global.window = originalWindow;
  });

  describe('basic loading', () => {
    it('returns null when localStorage has no data for the key', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      // No data at the app state key
      const result = loadPersistedAppState();
      expect(result).toBeNull();
    });

    it('returns parsed state with correct fields on valid JSON', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: 'anthropic',
        model: 'claude-sonnet-4-20250514',
        sessionId: 'session-123',
        queryCount: 5,
        currentView: 'chat',
        messages: [],
        fileEdits: [],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));

      const result = loadPersistedAppState();
      expect(result).not.toBeNull();
      expect(result!.provider).toBe('anthropic');
      expect(result!.model).toBe('claude-sonnet-4-20250514');
      expect(result!.sessionId).toBe('session-123');
      expect(result!.queryCount).toBe(5);
      expect(result!.currentView).toBe('chat');
      expect(result!.messages).toEqual([]);
      expect(result!.fileEdits).toEqual([]);
      expect(result!.subagentActivities).toEqual([]);
    });

    it('defaults provider to "" when not a string', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: 123, model: '', sessionId: null, queryCount: 0, currentView: 'chat' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.provider).toBe('');
    });

    it('defaults model to "" when not a string', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: null, sessionId: null, queryCount: 0, currentView: 'chat' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.model).toBe('');
    });

    it('defaults sessionId to null when not a string', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: '', sessionId: 123, queryCount: 0, currentView: 'chat' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.sessionId).toBeNull();
    });

    it('defaults queryCount to 0 when not a number', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: '', sessionId: null, queryCount: 'bad', currentView: 'chat' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.queryCount).toBe(0);
    });

    it('defaults currentView to "chat" when invalid', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: '', sessionId: null, queryCount: 0, currentView: 'invalid' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.currentView).toBe('chat');
    });

    it('accepts valid currentView values', () => {
      for (const view of ['chat', 'editor', 'git', 'tasks', 'billing', 'team']) {
        ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
        const data = { provider: '', model: '', sessionId: null, queryCount: 0, currentView: view };
        ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
        const result = loadPersistedAppState();
        expect(result!.currentView, `currentView="${view}"`).toBe(view);
        // Reset for next iteration
        ls = createStorageMock();
        setLocalStorageMock(ls);
      }
    });
  });

  describe('messages parsing', () => {
    it('parses messages with timestamp strings into Date objects', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [{ role: 'user', content: 'hello', timestamp: '2024-01-01T00:00:00Z' }],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages.length).toBe(1);
      expect(result!.messages[0].timestamp).toBeInstanceOf(Date);
      expect(result!.messages[0].role).toBe('user');
      expect(result!.messages[0].content).toBe('hello');
    });

    it('handles messages without timestamp', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [{ role: 'assistant', content: 'hi' }],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages.length).toBe(1);
      expect(result!.messages[0].timestamp).toBeInstanceOf(Date);
    });

    it('handles toolRefs as array in messages', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [{ role: 'assistant', content: '', toolRefs: ['ref1', 'ref2'] }],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages[0].toolRefs).toEqual(['ref1', 'ref2']);
    });

    it('handles toolRefs as non-array (undefined)', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [{ role: 'assistant', content: '', toolRefs: 'not-an-array' }],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages[0].toolRefs).toBeUndefined();
    });

    it('handles messages not being an array', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: 'not-an-array',
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages).toEqual([]);
    });

    it('handles missing messages field', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: '', sessionId: null, queryCount: 0, currentView: 'chat' };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.messages).toEqual([]);
    });
  });

  describe('fileEdits parsing', () => {
    it('parses fileEdits with timestamp strings into Date objects', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [],
        fileEdits: [{ path: 'foo.go', timestamp: '2024-01-01T00:00:00Z' }],
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.fileEdits.length).toBe(1);
      expect(result!.fileEdits[0].timestamp).toBeInstanceOf(Date);
      expect(result!.fileEdits[0].path).toBe('foo.go');
    });

    it('handles fileEdits not being an array', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = {
        provider: '',
        model: '',
        sessionId: null,
        queryCount: 0,
        currentView: 'chat',
        messages: [],
        fileEdits: 'not-an-array',
      };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.fileEdits).toEqual([]);
    });

    it('handles missing fileEdits field', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      const data = { provider: '', model: '', sessionId: null, queryCount: 0, currentView: 'chat', messages: [] };
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify(data));
      const result = loadPersistedAppState();
      expect(result!.fileEdits).toEqual([]);
    });
  });

  describe('instance switch reset', () => {
    it('returns null and clears storage when reset key is "1"', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      ss.setItem(INSTANCE_SWITCH_RESET_KEY, '1');
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify({ provider: 'anthropic' }));

      const result = loadPersistedAppState();
      expect(result).toBeNull();

      // Should have cleared the session storage key
      expect(ss.getItem).toHaveBeenCalledWith(INSTANCE_SWITCH_RESET_KEY);
      expect(ss.removeItem).toHaveBeenCalledWith(INSTANCE_SWITCH_RESET_KEY);

      // Should have removed the app state from localStorage
      expect(ls.removeItem).toHaveBeenCalledWith(`sprout:webui:state:v2:100:local`);
    });

    it('does NOT clear storage when reset key is not "1"', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      ss.setItem(INSTANCE_SWITCH_RESET_KEY, '0');
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify({ provider: 'anthropic' }));

      const result = loadPersistedAppState();
      expect(result).not.toBeNull();
      expect(result!.provider).toBe('anthropic');
      expect(ls.removeItem).not.toHaveBeenCalled();
    });

    it('does NOT clear storage when reset key is absent', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      // Don't set reset key at all
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify({ provider: 'anthropic' }));

      const result = loadPersistedAppState();
      expect(result).not.toBeNull();
      expect(result!.provider).toBe('anthropic');
    });

    it('does NOT clear storage when sessionStorage is undefined', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      setSessionStorageMock(undefined);
      ls.setItem(`sprout:webui:state:v2:100:local`, JSON.stringify({ provider: 'anthropic' }));

      const result = loadPersistedAppState();
      expect(result).not.toBeNull();
      expect(result!.provider).toBe('anthropic');
    });
  });

  describe('error handling', () => {
    it('returns null and calls notificationBus.notify on invalid JSON', () => {
      ls.setItem(INSTANCE_PID_STORAGE_KEY, '100');
      ls.setItem(`sprout:webui:state:v2:100:local`, 'not valid json {');

      const result = loadPersistedAppState();
      expect(result).toBeNull();

      // Verify notificationBus.notify was called with the expected message
      expect(notificationBus.notify).toHaveBeenCalledWith(
        'warning',
        'Settings',
        'Failed to load saved application state',
      );
    });
  });
});
