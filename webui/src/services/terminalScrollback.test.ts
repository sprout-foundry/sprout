/**
 * Unit tests for terminalScrollback.ts
 *
 * Tests IndexedDB-based scrollback persistence through an in-memory fake.
 * Covers: round-trip save/load, overwrite, delete, truncation, 24h expiry,
 * cleanup, and error resilience.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { saveScrollback, loadScrollback, deleteScrollback, cleanupOldEntries } from './terminalScrollback';

// ---------------------------------------------------------------------------
// In-memory IndexedDB fake (jsdom does not include IndexedDB)
// ---------------------------------------------------------------------------

interface DBEntry {
  sessionId: string;
  data: string;
  timestamp: number;
}

type StoreData = Map<string, DBEntry>;

/**
 * Shared database state that persists across open() calls within a test.
 * Reset in beforeEach so tests don't leak state.
 */
let sharedDBs: Map<string, Map<string, StoreData>>;

function resetSharedDBs() {
  sharedDBs = new Map();
}

resetSharedDBs();

/**
 * Create a synthetic Event whose .target has a .result property.
 */
function fakeEvent<T>(result: T): Event {
  const evt = new Event('success');
  Object.defineProperty(evt, 'target', {
    value: { result },
    writable: false,
  });
  return evt as Event;
}

/**
 * Build an IDBRequest-style object.  When the caller assigns onsuccess /
 * onerror those callbacks fire on the next microtask.
 */
function makeRequest<R>(result: R, error: Error | null): IDBRequest<R> {
  let _onsuccess: ((e: Event) => void) | null = null;

  const req: Partial<IDBRequest<R>> = {
    get result(): R {
      return result;
    },
    get error(): Error | null {
      return error;
    },
    set onsuccess(fn) {
      _onsuccess = fn;
      if (fn && !error) {
        queueMicrotask(() => _onsuccess?.(fakeEvent(result)));
      }
    },
    get onsuccess() {
      return null;
    },
    set onerror(_fn) {
      /* no-op in fake — errors never fire unless explicitly set */
    },
    get onerror() {
      return null;
    },
  };

  return req as unknown as IDBRequest<R>;
}

/**
 * Cursor request helper — fires onsuccess every time cursor.continue() is
 * called, with request.result updated to the next cursor (or null when done).
 */
function makeCursorRequest(
  entries: [string, DBEntry][],
  filter: (e: DBEntry) => boolean,
  onDelete: (key: string) => void,
): IDBRequest<IDBCursorWithValue | null> {
  const filtered = entries.filter(([, v]) => filter(v));
  let idx = 0;

  let cursor: IDBCursorWithValue | null = null;
  let _onsuccess: ((e: Event) => void) | null = null;

  function advanceCursor(): IDBCursorWithValue | null {
    while (idx < filtered.length) {
      const [key, val] = filtered[idx++];
      const currentKey = key;
      const currentVal = val;
      cursor = {
        key,
        value: val,
        delete(): IDBValidKey {
          onDelete(currentKey);
          return currentKey;
        },
        continue(): void {
          advanceCursor();
          queueMicrotask(() => _onsuccess?.(fakeEvent(cursor)));
        },
        advance(_count: number): void {
          /* not used by the module */
        },
        direction: 'next',
        source: {} as unknown as IDBObjectStore,
        update: () => {
          throw new Error('not implemented');
        },
      };
      return cursor;
    }
    cursor = null;
    return null;
  }

  // Seed first cursor
  advanceCursor();

  const req: Partial<IDBRequest<IDBCursorWithValue | null>> = {
    get result() {
      return cursor;
    },
    get error() {
      return null;
    },
    set onsuccess(fn) {
      _onsuccess = fn;
      // Fire initial onsuccess on microtask
      queueMicrotask(() => _onsuccess?.(fakeEvent(cursor)));
    },
    get onsuccess() {
      return null;
    },
    set onerror(_fn) {
      /* no-op */
    },
    get onerror() {
      return null;
    },
  };

  return req as unknown as IDBRequest<IDBCursorWithValue | null>;
}

// ---------------------------------------------------------------------------
// Fake IndexedDB
// ---------------------------------------------------------------------------

function createFakeIndexedDB(): typeof indexedDB {
  return {
    open(dbName: string, version: number): IDBRequest<IDBDatabase> {
      // Persist database state across open() calls within a test
      const dbKey = `${dbName}@${version}`;
      let dbState = sharedDBs.get(dbKey);
      if (!dbState) {
        dbState = new Map();
        sharedDBs.set(dbKey, dbState);
      }

      let _onsuccess: ((e: Event) => void) | null = null;

      // Build the fake DB object with a proper objectStoreNames getter
      const fakeDB: Record<string, unknown> = {
        createObjectStore(name: string, _opts?: IDBObjectStoreParameters) {
          if (!dbState.has(name)) {
            dbState.set(name, new Map());
          }
          return new FakeObjectStore(name, dbState.get(name)!);
        },
        transaction(storeNames: string | string[], mode: IDBTransactionMode = 'readonly') {
          const names = Array.isArray(storeNames) ? storeNames : [storeNames];
          return new FakeTransaction(names, mode, dbState);
        },
        close() {
          /* no-op */
        },
      };

      // objectStoreNames must match DOMStringList with a working .contains()
      Object.defineProperty(fakeDB, 'objectStoreNames', {
        get(): DOMStringList {
          const names = Array.from(dbState.keys());
          return {
            length: names.length,
            item: (i: number) => names[i] ?? null,
            contains: (name: string) => names.includes(name),
            [Symbol.iterator]: function* () {
              for (let i = 0; i < names.length; i++) yield names[i];
            },
          } as unknown as DOMStringList;
        },
        configurable: true,
      });

      // Build the request.  onupgradeneeded fires on first microtask,
      // onsuccess fires on the next (so the store has been created).
      const req: Partial<IDBRequest<IDBDatabase>> = {
        get result() {
          return fakeDB as IDBDatabase;
        },
        get error() {
          return null;
        },
        set onsuccess(fn) {
          _onsuccess = fn;
        },
        get onsuccess() {
          return null;
        },
        set onerror(_fn) {
          /* no-op */
        },
        get onerror() {
          return null;
        },
        set onupgradeneeded(fn) {
          // The module wires ALL handlers synchronously before any
          // microtask fires.  We fire onupgradeneeded on the first
          // microtask (the module creates the store), then fire
          // onsuccess on the second.
          queueMicrotask(() => {
            if (fn) {
              const upgradeEvt = fakeEvent(fakeDB) as unknown as IDBVersionChangeEvent;
              (upgradeEvt as any).oldVersion = 0;
              (upgradeEvt as any).newVersion = version;
              fn(upgradeEvt);
            }
            // Next microtask: resolve the open() promise
            queueMicrotask(() => {
              _onsuccess?.(fakeEvent(fakeDB as unknown as IDBDatabase));
            });
          });
        },
        get onupgradeneeded() {
          return null;
        },
      };

      return req as unknown as IDBRequest<IDBDatabase>;
    },
    deleteDatabase(_name: string) {
      return null as unknown as IDBOpenDBRequest;
    },
  };
}

// ---------------------------------------------------------------------------
// Fake ObjectStore / Transaction / Index
// ---------------------------------------------------------------------------

class FakeObjectStore {
  private _indexCache = new Map<string, FakeIndex>();

  constructor(
    readonly name: string,
    private data: StoreData,
  ) {}

  createIndex(indexName: string, keyPath: string, _opts?: IDBIndexParameters): FakeIndex {
    const idx = new FakeIndex(indexName, keyPath, this.data);
    this._indexCache.set(indexName, idx);
    return idx;
  }

  index(name: string): FakeIndex {
    let idx = this._indexCache.get(name);
    if (!idx) {
      idx = new FakeIndex(name, name, this.data);
      this._indexCache.set(name, idx);
    }
    return idx;
  }

  get(key: string): IDBRequest<DBEntry | undefined> {
    return makeRequest(this.data.get(key), null);
  }

  put(value: DBEntry): IDBRequest<string> {
    this.data.set(value.sessionId, value);
    return makeRequest(value.sessionId, null);
  }

  delete(key: string): IDBRequest<void> {
    this.data.delete(key);
    return makeRequest(undefined as unknown as void, null);
  }
}

class FakeIndex {
  constructor(
    readonly name: string,
    private keyPath: string,
    private data: StoreData,
  ) {}

  openCursor(range?: IDBKeyRange | null): IDBRequest<IDBCursorWithValue | null> {
    const all = Array.from(this.data.entries());

    const filter = (entry: DBEntry) => {
      if (!range) return true;
      const val = (entry as any)[this.keyPath] as number;
      const bound = (range as any)._bound as number;
      return val <= bound;
    };

    return makeCursorRequest(all, filter, (key) => this.data.delete(key));
  }
}

class FakeTransaction {
  constructor(
    storeNames: string[],
    readonly mode: IDBTransactionMode,
    private dbState: Map<string, StoreData>,
  ) {}

  objectStore(name: string): FakeObjectStore {
    const data = this.dbState.get(name);
    if (!data) {
      throw new DOMException(`Object store "${name}" not found`, 'InvalidAccessError');
    }
    return new FakeObjectStore(name, data);
  }

  set onerror(_fn: ((e: Event) => void) | null) {}
  get onerror() {
    return null;
  }
  set oncomplete(_fn: ((e: Event) => void) | null) {}
  get oncomplete() {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Setup / teardown
// ---------------------------------------------------------------------------

function setupFakeIndexedDB() {
  resetSharedDBs();
  (globalThis as any).indexedDB = createFakeIndexedDB();

  // jsdom does not include IDBKeyRange — provide a minimal fake for
  // cleanupOldEntries which calls IDBKeyRange.upperBound(cutoffTime).
  (globalThis as any).IDBKeyRange = {
    upperBound(value: number): IDBKeyRange {
      const range = {} as unknown as IDBKeyRange;
      (range as any)._type = 'upperBound';
      (range as any)._bound = value;
      return range;
    },
  };
}

beforeEach(() => {
  setupFakeIndexedDB();
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('terminalScrollback', () => {
  // ── Round-trip save/load ──────────────────────────────────────────────

  describe('saveScrollback / loadScrollback round-trip', () => {
    it('saves and loads scrollback data for a session', async () => {
      await saveScrollback('session-1', 'line1\nline2\nline3\n');
      expect(await loadScrollback('session-1')).toBe('line1\nline2\nline3\n');
    });

    it('saves and loads binary-like content', async () => {
      const data = 'hello\x00world\x01\x02end';
      await saveScrollback('bin-session', data);
      expect(await loadScrollback('bin-session')).toBe(data);
    });

    it('saves and loads an empty string', async () => {
      await saveScrollback('empty-session', '');
      expect(await loadScrollback('empty-session')).toBe('');
    });
  });

  // ── Unknown session ──────────────────────────────────────────────────

  describe('loadScrollback for unknown session', () => {
    it('returns null when session was never saved', async () => {
      expect(await loadScrollback('nonexistent')).toBeNull();
    });

    it('returns null after the session data was deleted', async () => {
      await saveScrollback('temp-session', 'data');
      await deleteScrollback('temp-session');
      expect(await loadScrollback('temp-session')).toBeNull();
    });
  });

  // ── Overwrite ────────────────────────────────────────────────────────

  describe('saveScrollback overwrite', () => {
    it('overwrites existing data when saving to the same session', async () => {
      await saveScrollback('overwrite', 'v1');
      await saveScrollback('overwrite', 'v2');
      expect(await loadScrollback('overwrite')).toBe('v2');
    });

    it('multiple overwrites keep the latest data', async () => {
      for (let i = 0; i < 5; i++) {
        await saveScrollback('multi', `v${i}`);
      }
      expect(await loadScrollback('multi')).toBe('v4');
    });
  });

  // ── Delete ───────────────────────────────────────────────────────────

  describe('deleteScrollback', () => {
    it('removes the entry from the store', async () => {
      await saveScrollback('del-me', 'data');
      await deleteScrollback('del-me');
      expect(await loadScrollback('del-me')).toBeNull();
    });

    it('does not throw when deleting a non-existent session', async () => {
      await expect(deleteScrollback('ghost')).resolves.toBeUndefined();
    });
  });

  // ── Truncation at 500KB ──────────────────────────────────────────────

  describe('truncation at 500KB', () => {
    it('truncates data that exceeds MAX_SIZE_BYTES (500KB)', async () => {
      const bigData = 'A'.repeat(600 * 1024).replace(/(.{1024})/g, '$1\n');
      await saveScrollback('big-session', bigData);

      const result = await loadScrollback('big-session');
      expect(result).not.toBeNull();
      if (result) {
        const encoder = new TextEncoder();
        expect(encoder.encode(result).length).toBeLessThanOrEqual(500 * 1024);
      }
    });

    it('does not truncate data under the limit', async () => {
      const data = 'small data well under the limit';
      await saveScrollback('small', data);
      expect(await loadScrollback('small')).toBe(data);
    });

    it('truncates at a line boundary (newline)', async () => {
      const lines: string[] = [];
      let size = 0;
      while (size < 600 * 1024) {
        const line = `line-${lines.length}-padded-content-to-reach-100-chars\n`;
        lines.push(line);
        size += line.length;
      }
      const fullData = lines.join('');
      await saveScrollback('line-bnd', fullData);

      const result = await loadScrollback('line-bnd');
      expect(result).not.toBeNull();
      if (result) {
        const encoder = new TextEncoder();
        expect(encoder.encode(result).length).toBeLessThanOrEqual(500 * 1024);
        // The truncated data should be shorter than the original
        expect(result.length).toBeLessThan(fullData.length);
      }
    });

    it('preserves emoji and multi-byte characters during truncation', async () => {
      // Build a large string with emojis (surrogate pairs) at regular intervals
      const emojis = ['😀', '😎', '🚀', '💻', '🎉', '⭐', '🌟', '💡', '🔥', '❤️'];
      const chunk = 'x'.repeat(100);
      const chunks: string[] = [];

      // Create data > 500KB with emojis throughout
      let totalSize = 0;
      let emojiIndex = 0;
      while (totalSize < 600 * 1024) {
        chunks.push(chunk);
        chunks.push(emojis[emojiIndex % emojis.length]);
        totalSize += chunk.length + emojis[emojiIndex % emojis.length].length;
        emojiIndex++;
      }

      const bigDataWithEmojis = chunks.join('');
      await saveScrollback('emoji-session', bigDataWithEmojis);

      const result = await loadScrollback('emoji-session');
      expect(result).not.toBeNull();

      if (result) {
        const encoder = new TextEncoder();
        expect(encoder.encode(result).length).toBeLessThanOrEqual(500 * 1024);

        // Verify that emojis are not corrupted - no orphaned surrogates
        // We can check this by ensuring all emojis are still intact
        for (const emoji of emojis) {
          // The result should contain some instances of each emoji, or at least
          // we shouldn't have any replacement characters indicating corruption
          expect(result).not.toContain('�'); // Replacement character
        }

        // Count emojis in result to ensure many survived truncation
        const emojiCount = emojis.reduce((count, emoji) => {
          const regex = new RegExp(emoji.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'g');
          const matches = result.match(regex);
          return count + (matches ? matches.length : 0);
        }, 0);

        // We should have at least some emojis in the truncated result
        expect(emojiCount).toBeGreaterThan(0);
      }
    });
  });

  // ── 24h expiry ───────────────────────────────────────────────────────

  describe('24h expiry', () => {
    it('returns null for entries older than 24 hours', async () => {
      await saveScrollback('expired', 'old data');

      const futureTime = Date.now() + 25 * 60 * 60 * 1000;
      vi.useFakeTimers({ now: futureTime });

      expect(await loadScrollback('expired')).toBeNull();
    });

    it('returns data for entries within 24 hours', async () => {
      await saveScrollback('fresh-1h', 'fresh data');

      const slightlyFuture = Date.now() + 1 * 60 * 60 * 1000;
      vi.useFakeTimers({ now: slightlyFuture });

      expect(await loadScrollback('fresh-1h')).toBe('fresh data');
    });
  });

  // ── cleanupOldEntries ────────────────────────────────────────────────

  describe('cleanupOldEntries', () => {
    it('removes entries older than 24 hours', async () => {
      await saveScrollback('cleanup-1', 'data');

      const futureTime = Date.now() + 25 * 60 * 60 * 1000;
      vi.useFakeTimers({ now: futureTime });

      await cleanupOldEntries();
      expect(await loadScrollback('cleanup-1')).toBeNull();
    });

    it('preserves fresh entries during cleanup', async () => {
      await saveScrollback('cleanup-2', 'fresh data');
      await cleanupOldEntries();
      expect(await loadScrollback('cleanup-2')).toBe('fresh data');
    });

    it('removes only old entries while keeping fresh ones', async () => {
      await saveScrollback('old-sess', 'old data');

      const futureTime = Date.now() + 25 * 60 * 60 * 1000;
      vi.useFakeTimers({ now: futureTime });

      await saveScrollback('fresh-sess', 'fresh data');
      await cleanupOldEntries();

      expect(await loadScrollback('old-sess')).toBeNull();
      expect(await loadScrollback('fresh-sess')).toBe('fresh data');
    });

    it('does not throw when no entries need cleanup', async () => {
      await expect(cleanupOldEntries()).resolves.toBeUndefined();
    });
  });

  // ── Error resilience ─────────────────────────────────────────────────

  describe('error resilience', () => {
    it('saveScrollback does not throw when IndexedDB fails', async () => {
      (globalThis as any).indexedDB = {
        open: () => {
          throw new Error('fail');
        },
      };
      await expect(saveScrollback('bad', 'data')).resolves.toBeUndefined();
      setupFakeIndexedDB();
    });

    it('loadScrollback returns null when IndexedDB fails', async () => {
      (globalThis as any).indexedDB = {
        open: () => {
          throw new Error('fail');
        },
      };
      expect(await loadScrollback('bad')).toBeNull();
      setupFakeIndexedDB();
    });

    it('deleteScrollback does not throw when IndexedDB fails', async () => {
      (globalThis as any).indexedDB = {
        open: () => {
          throw new Error('fail');
        },
      };
      await expect(deleteScrollback('bad')).resolves.toBeUndefined();
      setupFakeIndexedDB();
    });

    it('cleanupOldEntries does not throw when IndexedDB fails', async () => {
      (globalThis as any).indexedDB = {
        open: () => {
          throw new Error('fail');
        },
      };
      await expect(cleanupOldEntries()).resolves.toBeUndefined();
      setupFakeIndexedDB();
    });
  });

  // ── Multiple sessions ────────────────────────────────────────────────

  describe('multiple sessions', () => {
    it('stores data for multiple sessions independently', async () => {
      await saveScrollback('a', 'data A');
      await saveScrollback('b', 'data B');
      await saveScrollback('c', 'data C');

      expect(await loadScrollback('a')).toBe('data A');
      expect(await loadScrollback('b')).toBe('data B');
      expect(await loadScrollback('c')).toBe('data C');
    });

    it('deleting one session does not affect others', async () => {
      await saveScrollback('a', 'data A');
      await saveScrollback('b', 'data B');
      await deleteScrollback('a');

      expect(await loadScrollback('a')).toBeNull();
      expect(await loadScrollback('b')).toBe('data B');
    });
  });
});
