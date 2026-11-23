/**
 * Workspace-scoped recents for the command palette's quick-open.
 *
 * The v1 storage keyed one browser-global list by absolute paths, so files
 * from the previous folder stayed in the palette after a project folder switch —
 * and clicking one opened a path in the wrong project folder. v2 keys the list
 * per project folder and stores workspace-relative paths; a stale entry
 * from another project folder can never surface because it is simply not in
 * this project folder's bucket.
 *
 * Shape: { version: 2, roots: { [root]: { name, path, type }[] } }
 * Only the current project folder's bucket is read and written; buckets are
 * capped both per-root and globally so the key cannot grow unbounded.
 */

const STORAGE_KEY = 'sprout.commandPalette.recentFiles.v2';
export const RECENT_FILES_LIMIT = 15;
const MAX_ROOTS = 8;

export interface RecentFileEntry {
  name: string;
  path: string;
  type: string;
}

interface StoredShape {
  version: 2;
  roots: Record<string, RecentFileEntry[]>;
}

function readStore(storage: Storage): StoredShape {
  try {
    const raw = storage.getItem(STORAGE_KEY);
    if (!raw) return { version: 2, roots: {} };
    const parsed = JSON.parse(raw) as StoredShape | null;
    if (!parsed || parsed.version !== 2 || typeof parsed.roots !== 'object' || parsed.roots === null) {
      return { version: 2, roots: {} };
    }
    return parsed;
  } catch {
    return { version: 2, roots: {} };
  }
}

function writeStore(storage: Storage, store: StoredShape): void {
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch {
    // ignore quota / privacy-mode errors
  }
}

/** Drop the oldest roots beyond the cap so the key stays bounded. */
function capRoots(store: StoredShape): StoredShape {
  const roots = Object.keys(store.roots);
  if (roots.length <= MAX_ROOTS) return store;
  const kept = roots.slice(roots.length - MAX_ROOTS);
  const next: StoredShape = { version: 2, roots: {} };
  for (const root of kept) next.roots[root] = store.roots[root];
  return next;
}

/** Read this project folder's recents, most-recent first. */
export function loadRecentFiles(projectFolder: string, storage: Storage = window.localStorage): RecentFileEntry[] {
  if (!projectFolder) return [];
  const store = readStore(storage);
  const list = store.roots[projectFolder];
  if (!Array.isArray(list)) return [];
  return list.slice(0, RECENT_FILES_LIMIT);
}

/** Record a file open: move it to the front of this project folder's bucket. */
export function recordRecentFile(
  projectFolder: string,
  file: RecentFileEntry,
  storage: Storage = window.localStorage,
): RecentFileEntry[] {
  if (!projectFolder) return [];
  const store = readStore(storage);
  const prev = Array.isArray(store.roots[projectFolder]) ? store.roots[projectFolder] : [];
  const next = [file, ...prev.filter((f) => f.path !== file.path)].slice(0, RECENT_FILES_LIMIT);
  store.roots[projectFolder] = next;
  writeStore(storage, capRoots(store));
  return next;
}

/** Remove entries whose paths no longer exist in the given set of known paths. */
export function pruneRecentFiles(
  projectFolder: string,
  isKnown: (path: string) => boolean,
  storage: Storage = window.localStorage,
): RecentFileEntry[] {
  if (!projectFolder) return [];
  const store = readStore(storage);
  const list = store.roots[projectFolder];
  if (!Array.isArray(list)) return [];
  const next = list.filter((f) => isKnown(f.path));
  if (next.length === list.length) return list;
  store.roots[projectFolder] = next;
  writeStore(storage, capRoots(store));
  return next;
}

/** Test hook: drop every project folder's recents. */
export function clearRecentFiles(storage: Storage = window.localStorage): void {
  try {
    storage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}
