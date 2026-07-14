/**
 * Tests for cloudSessionStore — browser-local persistence for cloud chat sessions.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Mocks ────────────────────────────────────────────────────────────

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── localStorage mock ────────────────────────────────────────────────

function createStorageMock(throwOnAccess = false): Storage {
  const store: Record<string, string> = {};
  const noop = () => {};
  if (throwOnAccess) {
    const throwing = vi.fn(() => {
      throw new Error('localStorage unavailable');
    });
    return {
      length: 0,
      clear: throwing,
      getItem: throwing,
      key: throwing,
      removeItem: throwing,
      setItem: throwing,
    } as unknown as Storage;
  }
  return {
    length: 0,
    clear: () => Object.keys(store).forEach((k) => delete store[k]),
    getItem: (key: string) => (key in store ? store[key] : null),
    key: (i: number) => Object.keys(store)[i] ?? null,
    removeItem: (key: string) => {
      delete store[key];
    },
    setItem: (key: string, value: string) => {
      store[key] = value;
    },
  } as unknown as Storage;
}

// ── Imports (after mocks) ────────────────────────────────────────────

import type { Message } from '../types/app';
import {
  CLOUD_SESSION_PREFIX,
  clearAllSessions,
  deleteSession,
  deriveSessionTitle,
  deserializeMessages,
  getActiveSessionId,
  hasSession,
  listSessions,
  resetActiveSessionId,
  restoreSession,
  saveSession,
  setActiveSessionId,
} from './cloudSessionStore';

// ── Helpers ──────────────────────────────────────────────────────────

function userMsg(content: string, id = `u-${content}`): Message {
  return { id, type: 'user', content, timestamp: new Date('2024-01-01T00:00:00Z') };
}
function assistantMsg(content: string, id = `a-${content}`): Message {
  return { id, type: 'assistant', content, timestamp: new Date('2024-01-02T00:00:00Z') };
}

function setStorage(storage: Storage | null): void {
  if (storage) {
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true, writable: true });
  } else {
    // Simulate localStorage being completely absent (SSR / restricted).
    Object.defineProperty(window, 'localStorage', {
      get() {
        throw new Error('localStorage unavailable');
      },
      configurable: true,
    });
  }
}

// ── Tests ────────────────────────────────────────────────────────────

describe('cloudSessionStore', () => {
  let originalLocalStorage: Storage | undefined;

  beforeEach(() => {
    originalLocalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage')?.value as Storage | undefined;
    setStorage(createStorageMock());
    resetActiveSessionId();
  });

  afterEach(() => {
    if (originalLocalStorage) {
      Object.defineProperty(window, 'localStorage', {
        value: originalLocalStorage,
        configurable: true,
        writable: true,
      });
    }
    vi.clearAllMocks();
  });

  describe('deriveSessionTitle', () => {
    it('uses the first user message, trimmed to 50 chars', () => {
      const long = 'x'.repeat(120);
      const title = deriveSessionTitle([assistantMsg('hi'), userMsg(long)]);
      expect(title).toHaveLength(51); // 50 chars + ellipsis
      expect(title.endsWith('…')).toBe(true);
    });

    it('collapses whitespace in the title', () => {
      const title = deriveSessionTitle([userMsg('  hello\n\n  world  ')]);
      expect(title).toBe('hello world');
    });

    it('falls back to "Cloud chat" when there is no user message', () => {
      expect(deriveSessionTitle([assistantMsg('only assistant')])).toBe('Cloud chat');
      expect(deriveSessionTitle([])).toBe('Cloud chat');
      expect(deriveSessionTitle([userMsg('   ')])).toBe('Cloud chat');
    });
  });

  describe('saveSession / listSessions', () => {
    it('persists a session and lists it with derived metadata', () => {
      const id = saveSession([userMsg('Hello there'), assistantMsg('Hi!')]);
      expect(id).toBeTruthy();

      const { sessions, current_session_id } = listSessions();
      expect(sessions).toHaveLength(1);
      expect(sessions[0].session_id).toBe(id);
      expect(sessions[0].name).toBe('Hello there');
      expect(sessions[0].message_count).toBe(2);
      expect(current_session_id).toBe(id);
      // last_updated is an ISO string
      expect(new Date(sessions[0].last_updated).getTime()).not.toBeNaN();
    });

    it('reuses an explicit session id and updates metadata in place', () => {
      const id = saveSession([userMsg('first')], { sessionId: 'fixed-id' });
      expect(id).toBe('fixed-id');

      saveSession([userMsg('first'), assistantMsg('reply')], { sessionId: 'fixed-id' });

      const { sessions } = listSessions();
      expect(sessions).toHaveLength(1); // updated, not duplicated
      expect(sessions[0].message_count).toBe(2);
      expect(sessions[0].name).toBe('first');
    });

    it('respects an explicit name over the derived title', () => {
      const id = saveSession([userMsg('Hello')], { name: 'Custom Name' });
      const { sessions } = listSessions();
      expect(sessions[0].name).toBe('Custom Name');
      expect(id).toBeTruthy();
    });

    it('returns null and does not throw when localStorage is unavailable', () => {
      setStorage(null);
      const id = saveSession([userMsg('hi')]);
      expect(id).toBeNull();
      expect(listSessions().sessions).toHaveLength(0);
    });

    it('returns null when persisting an empty transcript', () => {
      const id = saveSession([]);
      expect(id).toBeNull();
    });

    it('lists sessions ordered newest-first by last_updated', async () => {
      const first = saveSession([userMsg('one')]);
      // Bump time so ordering is deterministic.
      await new Promise((r) => setTimeout(r, 5));
      const second = saveSession([userMsg('two')]);

      const { sessions } = listSessions();
      expect(sessions[0].session_id).toBe(second);
      expect(sessions[1].session_id).toBe(first);
    });
  });

  describe('active session id tracking', () => {
    it('saveSession with no explicit id generates a fresh one each call', () => {
      const a = saveSession([userMsg('a')]);
      const b = saveSession([userMsg('b')]);
      expect(a).not.toBe(b);
      expect(listSessions().sessions).toHaveLength(2);
    });

    it('restoreSession marks the session active so later saves update it', () => {
      const id = saveSession([userMsg('hello'), assistantMsg('world')]);
      setActiveSessionId(null); // simulate fresh page load
      expect(getActiveSessionId()).toBeNull();

      const restored = restoreSession(id!);
      expect(restored).not.toBeNull();
      expect(getActiveSessionId()).toBe(id);

      // A subsequent save with no explicit id should update, not duplicate.
      saveSession([userMsg('hello'), assistantMsg('world'), userMsg('again')]);
      expect(listSessions().sessions).toHaveLength(1);
      expect(hasSession(id!)).toBe(true);
    });

    it('resetActiveSessionId forces the next save to create a new record', () => {
      const id = saveSession([userMsg('first conv')]);
      resetActiveSessionId();
      const id2 = saveSession([userMsg('second conv')]);
      expect(id2).not.toBe(id);
      expect(listSessions().sessions).toHaveLength(2);
    });
  });

  describe('restoreSession', () => {
    it('returns null for an unknown session id', () => {
      expect(restoreSession('does-not-exist')).toBeNull();
    });

    it('returns the stored transcript', () => {
      const id = saveSession([userMsg('q'), assistantMsg('a')]);
      const restored = restoreSession(id!);
      expect(restored).not.toBeNull();
      expect(restored!.name).toBe('q');
      expect(restored!.messages).toHaveLength(2);
    });

    it('returns null for an empty id', () => {
      expect(restoreSession('')).toBeNull();
    });
  });

  describe('deserializeMessages', () => {
    it('round-trips timestamps back to Date objects', () => {
      const ts = new Date('2024-06-01T12:00:00Z');
      const id = saveSession([{ id: 'm1', type: 'user', content: 'x', timestamp: ts }]);
      const restored = restoreSession(id!);
      const msgs = deserializeMessages(restored!.messages);
      expect(msgs[0].timestamp).toBeInstanceOf(Date);
      expect((msgs[0].timestamp as Date).toISOString()).toBe(ts.toISOString());
    });

    it('filters out non user/assistant roles', () => {
      const msgs = deserializeMessages([
        { id: '1', type: 'user', content: 'a', timestamp: '2024-01-01T00:00:00Z' },
        // @ts-expect-error — simulate a stray system/tool message persisted by mistake
        { id: '2', type: 'system', content: 'sys', timestamp: '2024-01-01T00:00:00Z' },
        { id: '3', type: 'assistant', content: 'b', timestamp: '2024-01-01T00:00:00Z' },
      ]);
      expect(msgs).toHaveLength(2);
      expect(msgs.map((m) => m.type)).toEqual(['user', 'assistant']);
    });
  });

  describe('deleteSession', () => {
    it('removes a session from storage and the index', () => {
      const id = saveSession([userMsg('bye')]);
      expect(hasSession(id!)).toBe(true);

      const removed = deleteSession(id!);
      expect(removed).toBe(true);
      expect(hasSession(id!)).toBe(false);
      expect(listSessions().sessions.find((s) => s.session_id === id)).toBeUndefined();
    });

    it('returns false-ish for an unknown id', () => {
      expect(deleteSession('nope')).toBe(false);
    });
  });

  describe('clearAllSessions', () => {
    it('wipes every cloud session key', () => {
      saveSession([userMsg('one')]);
      saveSession([userMsg('two')]);
      expect(listSessions().sessions).toHaveLength(2);

      clearAllSessions();
      expect(listSessions().sessions).toHaveLength(0);
      expect(getActiveSessionId()).toBeNull();
    });
  });

  describe('storage key format', () => {
    it('uses the documented sprout-cloud-session-{id} key prefix', () => {
      const id = saveSession([userMsg('fmt')]);
      // The per-session record is stored under the prefix + id.
      const ls = window.localStorage;
      const raw = ls.getItem(`${CLOUD_SESSION_PREFIX}${id}`);
      expect(raw).not.toBeNull();
      expect(JSON.parse(raw!).messages).toHaveLength(1);
    });
  });

  describe('quota handling', () => {
    it('does not throw and gives up gracefully when storage setItem throws', () => {
      // A storage that rejects writes but allows reads/gets.
      const throwingStorage = createStorageMock();
      const originalSet = throwingStorage.setItem;
      throwingStorage.setItem = vi.fn(() => {
        throw new DOMException('quota', 'QuotaExceededError');
      });
      setStorage(throwingStorage);
      void originalSet;
      expect(() => saveSession([userMsg('x')])).not.toThrow();
      // The save should have failed gracefully (no record written).
      expect(listSessions().sessions).toHaveLength(0);
    });
  });
});
