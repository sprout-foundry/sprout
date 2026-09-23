/**
 * IndexedDB persistence for `?repo=` imports.
 *
 * The editor's file tree lives in the in-memory WASM VFS: on a page reload
 * the workspace is gone. This cache stores the imported file manifest so a
 * reload can re-seed the VFS without hitting the network clone again.
 *
 * Keyed by the `?repo=` URL (the value the bootstrap checks), entry holds
 * the canonical `owner/name` plus the file list. A `__last_repo__` pointer
 * remembers the most recent workspace for future "continue where you left
 * off" support.
 *
 * The module reads `globalThis.indexedDB` lazily so unit tests can stub it
 * and SSR/node contexts no-op cleanly.
 */

export interface RepoImportCacheEntry {
  /** Canonical repo name as reported by the platform import (`owner/name`). */
  repo: string;
  files: Array<{ path: string; content: string }>;
  importedAt: string;
}

const DB_NAME = 'sprout-workspace';
const DB_VERSION = 1;
const STORE = 'repo-imports';
const LAST_REPO_KEY = '__last_repo__';

/** Skip persisting manifests larger than this (IndexedDB has room, but a
 *  500-file/1MB-per-file import can be huge — don't block startup on it). */
const MAX_CACHE_BYTES = 25 * 1024 * 1024;

function indexedDb(): IDBFactory | null {
  const idb = (globalThis as Record<string, unknown>).indexedDB as IDBFactory | undefined;
  return idb ?? null;
}

let dbPromise: Promise<IDBDatabase> | null = null;

function openDb(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  dbPromise = new Promise((resolve, reject) => {
    const idb = indexedDb();
    if (!idb) {
      reject(new Error('IndexedDB unavailable'));
      return;
    }
    const req = idb.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE);
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('failed to open ' + DB_NAME));
  });
  return dbPromise;
}

function withStore<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore) => IDBRequest | void): Promise<T> {
  return openDb()
    .then((db) => {
      const tx = db.transaction(STORE, mode);
      const store = tx.objectStore(STORE);
      const req = run(store);
      return new Promise<T>((resolve, reject) => {
        if (req) {
          // put → result undefined; get → the stored value.
          req.onsuccess = () => resolve(req.result as T);
          req.onerror = () => reject(req.error ?? new Error('request failed'));
        } else {
          tx.oncomplete = () => resolve(undefined as T);
          tx.onerror = () => reject(tx.error ?? new Error('transaction failed'));
          tx.onabort = () => reject(tx.error ?? new Error('transaction aborted'));
        }
      });
    })
    .catch(() => {
      // Persistence is best-effort: a full/corrupt IndexedDB must never
      // break the editor. Reset the memoized promise so a later call can
      // retry with a fresh connection.
      dbPromise = null;
      throw new Error('repo import cache unavailable');
    });
}

function entryBytes(entry: RepoImportCacheEntry): number {
  let bytes = 0;
  for (const f of entry.files) {
    bytes += f.path.length + (f.content ? f.content.length : 0);
    if (bytes > MAX_CACHE_BYTES) return bytes;
  }
  return bytes;
}

/** Persist an imported manifest. Skips oversized entries (no-op, resolves). */
export async function saveRepoImport(repoKey: string, entry: RepoImportCacheEntry): Promise<void> {
  if (!indexedDb()) return;
  if (entryBytes(entry) > MAX_CACHE_BYTES) return;
  await withStore('readwrite', (store) => store.put(entry, repoKey));
}

/** Load a cached manifest, or null on miss (and when IndexedDB is absent). */
export async function loadRepoImport(repoKey: string): Promise<RepoImportCacheEntry | null> {
  if (!indexedDb()) return null;
  try {
    const result = await withStore<RepoImportCacheEntry | null>('readonly', (store) => store.get(repoKey));
    return result ?? null;
  } catch {
    return null;
  }
}

/** Remember the most recently imported workspace (best-effort). */
export async function setLastRepo(repoKey: string): Promise<void> {
  if (!indexedDb()) return;
  try {
    await withStore('readwrite', (store) => store.put({ repo: repoKey }, LAST_REPO_KEY));
  } catch {
    // best-effort
  }
}

/** The most recently imported workspace key, or null. */
export async function getLastRepo(): Promise<string | null> {
  if (!indexedDb()) return null;
  try {
    const result = await withStore<{ repo: string } | null>('readonly', (store) => store.get(LAST_REPO_KEY));
    return result?.repo ?? null;
  } catch {
    return null;
  }
}
