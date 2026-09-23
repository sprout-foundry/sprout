/**
 * Tests for the ?repo= import cache (IndexedDB persistence).
 *
 * jsdom provides no IndexedDB, so a minimal in-memory fake implements just
 * the surface repoImportCache uses (open → db.transaction → objectStore
 * put/get). The module reads globalThis.indexedDB lazily, so the fake is
 * installed per test and removed for the "unavailable" case.
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';

// ── Minimal in-memory IndexedDB fake ──────────────────────────────

type Handler = (() => void) | null;

class FakeRequest {
  result: unknown;
  onsuccess: Handler = null;
  onerror: Handler = null;
  onupgradeneeded: Handler = null;
  constructor(result?: unknown) {
    this.result = result;
  }
}

function setIndexedDB(enabled: boolean) {
  if (!enabled) {
    delete (globalThis as Record<string, unknown>).indexedDB;
    return;
  }
  const store = new Map<string, unknown>();
  let hasStore = false;
  const open = (name: string, version: number) => {
    void name;
    void version;
    const db = {
      objectStoreNames: { contains: () => hasStore },
      createObjectStore: () => {},
      transaction: () => {
        const tx: {
          objectStore: () => { put(v: unknown, k?: unknown): FakeRequest; get(k: unknown): FakeRequest };
          oncomplete: Handler;
          onerror: Handler;
          onabort: Handler;
        } = {
          oncomplete: null,
          onerror: null,
          onabort: null,
          objectStore: () => ({
            put(value: unknown, key?: unknown) {
              if (key !== undefined) store.set(String(key), value);
              const req = new FakeRequest(undefined);
              queueMicrotask(() => req.onsuccess?.());
              return req;
            },
            get(key: unknown) {
              const req = new FakeRequest(store.get(String(key)));
              queueMicrotask(() => req.onsuccess?.());
              return req;
            },
          }),
        };
        // Fire oncomplete on the next microtask — by then the caller has
        // assigned its handler (synchronous flow after transaction() returns).
        queueMicrotask(() => tx.oncomplete?.());
        return tx;
      },
    };
    const req = new FakeRequest(db);
    queueMicrotask(() => {
      if (!hasStore) {
        req.onupgradeneeded?.();
        hasStore = true;
      }
      req.onsuccess?.();
    });
    return req;
  };
  (globalThis as Record<string, unknown>).indexedDB = { open } as unknown as IDBFactory;
}

// ── Tests ─────────────────────────────────────────────────────────

// The module is (re-)imported per test so its lazy `globalThis.indexedDB`
// reads observe the fresh fake; its memoized connection stays valid
// within one fake instance.
let saveRepoImport: typeof import('./repoImportCache').saveRepoImport;
let loadRepoImport: typeof import('./repoImportCache').loadRepoImport;
let setLastRepo: typeof import('./repoImportCache').setLastRepo;
let getLastRepo: typeof import('./repoImportCache').getLastRepo;

beforeEach(async () => {
  setIndexedDB(true);
  const mod = await import('./repoImportCache');
  saveRepoImport = mod.saveRepoImport;
  loadRepoImport = mod.loadRepoImport;
  setLastRepo = mod.setLastRepo;
  getLastRepo = mod.getLastRepo;
});

afterEach(() => {
  setIndexedDB(false);
});

describe('repoImportCache (IndexedDB persistence)', () => {
  it('round-trips a manifest under the repo URL key', async () => {
    const entry = {
      repo: 'octocat/Hello-World',
      files: [{ path: 'README', content: 'hello' }],
      importedAt: '2026-09-23T00:00:00Z',
    };
    await saveRepoImport('https://github.com/octocat/Hello-World', entry);
    const loaded = await loadRepoImport('https://github.com/octocat/Hello-World');
    expect(loaded).toEqual(entry);
  });

  it('returns null on cache miss', async () => {
    expect(await loadRepoImport('https://github.com/nobody/nothing')).toBeNull();
  });

  it('remembers the last imported repo', async () => {
    await setLastRepo('https://github.com/octocat/Hello-World');
    expect(await getLastRepo()).toBe('https://github.com/octocat/Hello-World');
  });

  it('is a no-op (null reads) when IndexedDB is unavailable', async () => {
    setIndexedDB(false);
    await expect(saveRepoImport('k', { repo: 'r', files: [], importedAt: 'x' })).resolves.toBeUndefined();
    expect(await loadRepoImport('k')).toBeNull();
    expect(await getLastRepo()).toBeNull();
  });

  it('skips persisting oversized manifests', async () => {
    // ~13MB per file, 2 files = 26MB > the 25MB cap → not persisted.
    const big = 'x'.repeat(13 * 1024 * 1024);
    const entry = {
      repo: 'big/repo',
      files: [
        { path: 'a.bin', content: big },
        { path: 'b.bin', content: big },
      ],
      importedAt: '2026-09-23T00:00:00Z',
    };
    await saveRepoImport('https://github.com/big/repo', entry);
    expect(await loadRepoImport('https://github.com/big/repo')).toBeNull();
  });
});
