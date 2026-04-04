/**
 * Layout Persistence Service
 *
 * Saves and restores the editor layout state (open file tabs with their pane
 * positions, cursor/scroll positions, active buffer per pane) so that on page
 * reload the user sees the same set of open files in the same panes.
 *
 * Uses localStorage under the key `ledit.editor.layoutState`.
 * Writes are debounced (1 s) to avoid excessive I/O during rapid tab switching.
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface CursorPosition {
  line: number;
  column: number;
}

export interface ScrollPosition {
  top: number;
  left: number;
}

export interface BufferLayoutEntry {
  filePath: string;
  paneId: string;
  isActive: boolean;
  cursorPosition: CursorPosition;
  scrollPosition: ScrollPosition;
}

export interface LayoutSnapshot {
  /** Schema version — bump when the shape changes so we can migrate later. */
  version: 1;
  /** Which pane is currently focused. */
  activePaneId: string | null;
  /** File path of the globally active buffer. */
  activeBufferFilePath: string | null;
  /** Per-buffer layout entries. */
  buffers: BufferLayoutEntry[];
  /** File paths in tab insertion order. */
  bufferOrder: string[];
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const STORAGE_KEY = 'ledit.editor.layoutState';
const MAX_BUFFERS = 50;
const DEBOUNCE_MS = 1_000;

/** File-path prefix used for virtual / workspace-internal buffers. */
const VIRTUAL_PATH_PREFIX = '__workspace/';

/** Storage keys for pane layout type and pane sizes (shared with EditorManagerContext). */
export const PANE_LAYOUT_STORAGE_KEY = 'ledit.editor.paneLayout';
export const PANE_SIZES_STORAGE_KEY = 'ledit.editor.paneSizes';

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/** Safely read a localStorage key, returning null on failure. */
export function readStorageItem(key: string): string | null {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return null;
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Safely write a localStorage key. */
export function writeStorageItem(key: string, value: string): void {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return;
    window.localStorage.setItem(key, value);
  } catch {
    // Ignore storage errors (private browsing, quota exceeded, etc.)
  }
}

/** Whether a file path is a real file (not a virtual workspace buffer). */
function isRealFilePath(filePath: string): boolean {
  return !filePath.startsWith(VIRTUAL_PATH_PREFIX) && filePath.length > 0;
}

// ---------------------------------------------------------------------------
// Debounce state (module-level singleton)
// ---------------------------------------------------------------------------

let debounceTimerId: ReturnType<typeof setTimeout> | null = null;

/**
 * The latest snapshot passed to `saveLayoutSnapshot`, kept so that `flush()`
 * can write it synchronously (e.g. on `beforeunload`).
 */
let pendingSnapshot: LayoutSnapshot | null = null;

/** The `beforeunload` handler reference, stored so it can be removed. */
let beforeUnloadHandler: (() => void) | null = null;

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Synchronously flush any pending snapshot to localStorage.
 *
 * Call on `beforeunload` to ensure the latest layout state is persisted even
 * when the page is closed within the debounce window.
 */
export function flush(): void {
  if (pendingSnapshot === null) return;
  // Clear the debounce timer — we're writing immediately.
  if (debounceTimerId !== null) {
    clearTimeout(debounceTimerId);
    debounceTimerId = null;
  }
  const snapshot = pendingSnapshot;
  pendingSnapshot = null;
  flushSnapshot(snapshot);
}

/**
 * Register the `beforeunload` listener that flushes pending writes.
 *
 * Call once during application initialisation.  The listener is removed by
 * `dispose()`.
 */
export function initBeforeUnloadFlush(): void {
  if (beforeUnloadHandler !== null || typeof window === 'undefined') return;
  beforeUnloadHandler = () => flush();
  window.addEventListener('beforeunload', beforeUnloadHandler);
}

/**
 * Serialize a layout snapshot to localStorage.
 *
 * The write is debounced (1 s) so rapid successive calls (e.g. tab switching)
 * coalesce into a single write.  Call `flush()` (or just close the page — the
 * `beforeunload` listener handles it) to write immediately.
 * Call `dispose()` to tear down the timer and listener.
 */
export function saveLayoutSnapshot(state: LayoutSnapshot): void {
  pendingSnapshot = state;
  if (debounceTimerId !== null) {
    clearTimeout(debounceTimerId);
  }

  debounceTimerId = setTimeout(() => {
    debounceTimerId = null;
    pendingSnapshot = null;
    flushSnapshot(state);
  }, DEBOUNCE_MS);
}

/**
 * Immediately write the snapshot to localStorage (no debouncing).
 * Exported as internal helper — callers should prefer `saveLayoutSnapshot`.
 */
function flushSnapshot(state: LayoutSnapshot): void {
  try {
    // Filter to real file buffers only
    const filteredBuffers = state.buffers.filter((b) => isRealFilePath(b.filePath));

    // Deduplicate by filePath, keeping the entry that is active (or the last seen)
    const bufferByPath = new Map<string, BufferLayoutEntry>();
    for (const entry of filteredBuffers) {
      const existing = bufferByPath.get(entry.filePath);
      if (!existing || entry.isActive) {
        bufferByPath.set(entry.filePath, entry);
      }
    }

    // Deduplicate bufferOrder, preserving order
    const dedupedOrder: string[] = [];
    const seen = new Set<string>();
    for (const fp of state.bufferOrder) {
      if (isRealFilePath(fp) && !seen.has(fp)) {
        seen.add(fp);
        dedupedOrder.push(fp);
      }
    }

    // Truncate to MAX_BUFFERS — drop oldest entries that are not the active buffer
    let buffersArr = Array.from(bufferByPath.values());
    let orderArr = [...dedupedOrder];

    if (buffersArr.length > MAX_BUFFERS) {
      // Prioritise keeping the active buffer and the active buffer in each pane
      const keepPaths = new Set<string>();
      if (state.activeBufferFilePath && isRealFilePath(state.activeBufferFilePath)) {
        keepPaths.add(state.activeBufferFilePath);
      }
      for (const entry of buffersArr) {
        if (entry.isActive) keepPaths.add(entry.filePath);
      }

      // Build truncated list: priority paths first, then most-recent in bufferOrder
      const priority: BufferLayoutEntry[] = [];
      const rest: BufferLayoutEntry[] = [];
      for (const entry of buffersArr) {
        if (keepPaths.has(entry.filePath)) {
          priority.push(entry);
        } else {
          rest.push(entry);
        }
      }

      // Sort rest by position in bufferOrder (last = most recent)
      const orderIndex = new Map(orderArr.map((p, i) => [p, i]));
      rest.sort((a, b) => {
        const ia = orderIndex.get(a.filePath) ?? -1;
        const ib = orderIndex.get(b.filePath) ?? -1;
        return ib - ia; // most recent first
      });

      buffersArr = [...priority, ...rest.slice(0, MAX_BUFFERS - priority.length)];

      // Rebuild order to match surviving buffers
      const survivingPaths = new Set(buffersArr.map((b) => b.filePath));
      orderArr = orderArr.filter((p) => survivingPaths.has(p));
    }

    const snapshot: LayoutSnapshot = {
      version: 1,
      activePaneId: state.activePaneId,
      activeBufferFilePath: state.activeBufferFilePath,
      buffers: buffersArr,
      bufferOrder: orderArr,
    };

    writeStorageItem(STORAGE_KEY, JSON.stringify(snapshot));
  } catch {
    // Silently ignore serialization errors
  }
}

/**
 * Read and parse the saved layout from localStorage.
 *
 * Returns `null` if no snapshot exists, the JSON is malformed, or the schema
 * version is unrecognised.
 */
export function loadLayoutSnapshot(): LayoutSnapshot | null {
  try {
    const raw = readStorageItem(STORAGE_KEY);
    if (!raw) return null;

    const parsed = JSON.parse(raw) as LayoutSnapshot;

    // Basic shape validation
    if (!parsed || typeof parsed !== 'object') return null;
    if (parsed.version !== 1) return null;

    return parsed;
  } catch {
    return null;
  }
}

/**
 * Remove the persisted layout snapshot from localStorage.
 */
export function clearLayoutSnapshot(): void {
  try {
    if (typeof window === 'undefined' || !window.localStorage) return;
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Ignore
  }
}

/**
 * Tear down the internal debounce timer and the `beforeunload` listener.
 *
 * Call this when the editor is being unmounted to prevent leaked timers.
 * Any pending debounced write is flushed synchronously to avoid data loss
 * (e.g. during React StrictMode double-mount in development).
 */
export function dispose(): void {
  if (debounceTimerId !== null) {
    clearTimeout(debounceTimerId);
    debounceTimerId = null;
  }
  // Flush whatever is pending rather than discarding it — the cost of one
  // synchronous localStorage write on unmount is negligible, and it
  // guarantees no state is lost (e.g. StrictMode teardown remount).
  if (pendingSnapshot !== null) {
    flushSnapshot(pendingSnapshot);
    pendingSnapshot = null;
  }
  if (beforeUnloadHandler !== null) {
    window.removeEventListener('beforeunload', beforeUnloadHandler);
    beforeUnloadHandler = null;
  }
}
