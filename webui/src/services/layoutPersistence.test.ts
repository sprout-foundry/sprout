/**
 * Comprehensive tests for layoutPersistence service.
 *
 * The module maintains module-level singleton state (debounceTimerId,
 * pendingSnapshot, beforeUnloadHandler).  We reset it between tests via
 * vi.runAllTimers() (to flush any pending debounce), then clearing
 * localStorage via window.localStorage.clear().
 */

import {
  readStorageItem,
  writeStorageItem,
  saveLayoutSnapshot,
  flush,
  loadLayoutSnapshot,
  clearLayoutSnapshot,
  dispose,
  initBeforeUnloadFlush,
  getLayoutStorageKey,
  getPanesStorageKey,
  loadSavedPanes,
  reconcilePaneSizes,
  evenPaneSizes,
  type CursorPosition,
  type ScrollPosition,
  type BufferLayoutEntry,
  type LayoutSnapshot,
} from './layoutPersistence';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeSnapshot(overrides: Partial<LayoutSnapshot> = {}): LayoutSnapshot {
  return {
    version: 1,
    activePaneId: 'pane-1',
    activeBufferFilePath: '/src/index.ts',
    buffers: [],
    bufferOrder: [],
    ...overrides,
  };
}

function makeEntry(filePath: string, overrides: Partial<BufferLayoutEntry> = {}): BufferLayoutEntry {
  return {
    filePath,
    paneId: 'pane-1',
    isActive: false,
    cursorPosition: { line: 1, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    ...overrides,
  };
}

function makeCursor(line: number, column: number): CursorPosition {
  return { line, column };
}

function makeScroll(top: number, left: number): ScrollPosition {
  return { top, left };
}

// ---------------------------------------------------------------------------
// Track beforeunload listeners
// ---------------------------------------------------------------------------

let realAddEventListener: typeof window.addEventListener;
let realRemoveEventListener: typeof window.removeEventListener;
const beforeUnloadHandlers: (() => void)[] = [];

// ---------------------------------------------------------------------------
// beforeEach / afterEach
// ---------------------------------------------------------------------------

beforeEach(() => {
  // Switch to fake timers first so we can flush pending work from the
  // previous test's module-level debounce timer.
  vi.useFakeTimers();

  // Flush any pending debounce timer so the module's internal state is
  // clean (pendingSnapshot = null, debounceTimerId = null).
  vi.runAllTimers();
  dispose();
  flush();

  // Clear jsdom's built-in localStorage (cannot reassign window.localStorage
  // in jsdom — it ignores simple assignment).
  window.localStorage.clear();

  // Save real event listener implementations before we spy on them.
  realAddEventListener = window.addEventListener;
  realRemoveEventListener = window.removeEventListener;

  // Track beforeunload listeners via spy
  beforeUnloadHandlers.length = 0;

  vi.spyOn(window, 'addEventListener').mockImplementation(
    (type: string, handler: EventListenerOrEventListenerObject, ...rest) => {
      if (type === 'beforeunload' && typeof handler === 'function') {
        beforeUnloadHandlers.push(handler as () => void);
      }
      realAddEventListener.call(window, type, handler, ...rest);
    },
  );

  vi.spyOn(window, 'removeEventListener').mockImplementation(
    (type: string, handler: EventListenerOrEventListenerObject, ...rest) => {
      if (type === 'beforeunload' && typeof handler === 'function') {
        const idx = beforeUnloadHandlers.indexOf(handler as () => void);
        if (idx >= 0) beforeUnloadHandlers.splice(idx, 1);
      }
      realRemoveEventListener.call(window, type, handler, ...rest);
    },
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

// ===========================================================================
// readStorageItem
// ===========================================================================

describe('readStorageItem', () => {
  it('returns the stored string for an existing key', () => {
    window.localStorage.setItem('test-key', 'hello');
    expect(readStorageItem('test-key')).toBe('hello');
  });

  it('returns null for a missing key', () => {
    expect(readStorageItem('nonexistent')).toBeNull();
  });

  it('returns null when localStorage.getItem throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('security error');
    });
    expect(readStorageItem('any-key')).toBeNull();
  });
});

// ===========================================================================
// writeStorageItem
// ===========================================================================

describe('writeStorageItem', () => {
  it('persists a string value', () => {
    writeStorageItem('k', 'v');
    expect(window.localStorage.getItem('k')).toBe('v');
  });

  it('silently ignores storage errors', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded');
    });
    expect(() => writeStorageItem('k', 'v')).not.toThrow();
  });
});

// ===========================================================================
// saveLayoutSnapshot — debounce coalescing
// ===========================================================================

describe('saveLayoutSnapshot', () => {
  it('does not write immediately (debounced)', () => {
    const snapshot = makeSnapshot();
    saveLayoutSnapshot(snapshot);
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('writes after the debounce timer fires', () => {
    const snapshot = makeSnapshot({
      activePaneId: 'pane-2',
      activeBufferFilePath: '/src/app.ts',
    });
    saveLayoutSnapshot(snapshot);
    vi.advanceTimersByTime(1_000);

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('pane-2');
    expect(loaded!.activeBufferFilePath).toBe('/src/app.ts');
  });

  it('coalesces multiple rapid saves — only the last snapshot is persisted', () => {
    const snap1 = makeSnapshot({ activePaneId: 'first' });
    const snap2 = makeSnapshot({ activePaneId: 'second' });
    const snap3 = makeSnapshot({ activePaneId: 'third' });

    saveLayoutSnapshot(snap1);
    vi.advanceTimersByTime(300);
    saveLayoutSnapshot(snap2);
    vi.advanceTimersByTime(300);
    saveLayoutSnapshot(snap3);

    // Debounce hasn't fully elapsed yet — nothing should be written.
    expect(loadLayoutSnapshot()).toBeNull();

    // Advance past the 1s debounce window from snap3 ( snap3 was saved at
    // virtual time 600; debounce fires at 1600).
    vi.advanceTimersByTime(1_000);
    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('third');
  });

  it('replaces a pending timer when a new snapshot is saved', () => {
    const snap1 = makeSnapshot({ activePaneId: 'old' });
    const snap2 = makeSnapshot({ activePaneId: 'new' });

    saveLayoutSnapshot(snap1);
    vi.advanceTimersByTime(500);
    saveLayoutSnapshot(snap2);

    // Need to advance the FULL debounce period from the last save to fire it.
    expect(loadLayoutSnapshot()).toBeNull();
    vi.advanceTimersByTime(1_000);
    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('new');
  });
});

// ===========================================================================
// flush — synchronous write of pending snapshot
// ===========================================================================

describe('flush', () => {
  it('writes the pending snapshot immediately', () => {
    const snapshot = makeSnapshot({ activePaneId: 'flushed' });
    saveLayoutSnapshot(snapshot);

    expect(loadLayoutSnapshot()).toBeNull();

    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('flushed');
  });

  it('clears the debounce timer so no double-write happens', () => {
    const snapshot = makeSnapshot({ activePaneId: 'once' });
    saveLayoutSnapshot(snapshot);
    flush();

    vi.advanceTimersByTime(2_000);

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('once');
  });

  it('is a no-op when there is no pending snapshot', () => {
    expect(() => flush()).not.toThrow();
    expect(loadLayoutSnapshot()).toBeNull();
  });
});

// ===========================================================================
// loadLayoutSnapshot
// ===========================================================================

describe('loadLayoutSnapshot', () => {
  it('returns null when nothing is stored', () => {
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('parses and returns a valid stored snapshot', () => {
    const snapshot: LayoutSnapshot = {
      version: 1,
      activePaneId: 'p1',
      activeBufferFilePath: '/a.ts',
      buffers: [makeEntry('/a.ts')],
      bufferOrder: ['/a.ts'],
    };
    window.localStorage.setItem(getLayoutStorageKey(), JSON.stringify(snapshot));

    const loaded = loadLayoutSnapshot();
    expect(loaded).toEqual(snapshot);
  });

  it('returns null for malformed JSON', () => {
    window.localStorage.setItem(getLayoutStorageKey(), 'not json at all {{');
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('returns null when JSON is valid but version is not 1', () => {
    window.localStorage.setItem(
      getLayoutStorageKey(),
      JSON.stringify({
        version: 999,
        activePaneId: null,
        activeBufferFilePath: null,
        buffers: [],
        bufferOrder: [],
      }),
    );
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('returns null when JSON is a primitive string (not an object)', () => {
    window.localStorage.setItem(getLayoutStorageKey(), '"just a string"');
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('returns null when JSON is null', () => {
    window.localStorage.setItem(getLayoutStorageKey(), 'null');
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('returns null when JSON is an array', () => {
    window.localStorage.setItem(getLayoutStorageKey(), '[1,2,3]');
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('returns null when localStorage.getItem throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('boom');
    });
    expect(loadLayoutSnapshot()).toBeNull();
  });
});

// ===========================================================================
// clearLayoutSnapshot
// ===========================================================================

describe('clearLayoutSnapshot', () => {
  it('removes the stored snapshot', () => {
    window.localStorage.setItem(getLayoutStorageKey(), JSON.stringify(makeSnapshot()));
    expect(loadLayoutSnapshot()).not.toBeNull();

    clearLayoutSnapshot();
    expect(loadLayoutSnapshot()).toBeNull();
  });

  it('is a no-op when nothing is stored', () => {
    expect(() => clearLayoutSnapshot()).not.toThrow();
  });
});

// ===========================================================================
// Virtual buffer filtering
// ===========================================================================

describe('virtual buffer filtering', () => {
  it('filters out buffers whose filePath starts with __workspace/', () => {
    const snapshot = makeSnapshot({
      buffers: [makeEntry('/real/file.ts'), makeEntry('__workspace/virtual.buf'), makeEntry('/another/real.go')],
      bufferOrder: ['/real/file.ts', '__workspace/virtual.buf', '/another/real.go'],
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers).toHaveLength(2);
    expect(loaded!.buffers.map((b) => b.filePath)).toEqual(['/real/file.ts', '/another/real.go']);
    expect(loaded!.bufferOrder).toEqual(['/real/file.ts', '/another/real.go']);
  });

  it('filters out buffers with empty filePath', () => {
    const snapshot = makeSnapshot({
      buffers: [makeEntry('/real.ts'), makeEntry('')],
      bufferOrder: ['/real.ts', ''],
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers).toHaveLength(1);
    expect(loaded!.buffers[0].filePath).toBe('/real.ts');
    expect(loaded!.bufferOrder).toEqual(['/real.ts']);
  });
});

// ===========================================================================
// Buffer deduplication
// ===========================================================================

describe('buffer deduplication', () => {
  it('deduplicates buffers by filePath, preferring the active entry', () => {
    const snapshot = makeSnapshot({
      buffers: [
        makeEntry('/duplicate.ts', {
          paneId: 'pane-1',
          isActive: false,
          cursorPosition: makeCursor(5, 10),
        }),
        makeEntry('/duplicate.ts', {
          paneId: 'pane-2',
          isActive: true,
          cursorPosition: makeCursor(20, 3),
        }),
      ],
      bufferOrder: ['/duplicate.ts', '/duplicate.ts'],
      activePaneId: 'pane-2',
      activeBufferFilePath: '/duplicate.ts',
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers).toHaveLength(1);
    expect(loaded!.buffers[0].isActive).toBe(true);
    expect(loaded!.buffers[0].paneId).toBe('pane-2');
    expect(loaded!.buffers[0].cursorPosition).toEqual(makeCursor(20, 3));
  });

  it('when neither entry is active, keeps the first one seen', () => {
    // Code: `if (!existing || entry.isActive)` — non-active second entry
    // does not replace the first.
    const snapshot = makeSnapshot({
      buffers: [
        makeEntry('/dup.ts', {
          paneId: 'pane-1',
          isActive: false,
          cursorPosition: makeCursor(1, 0),
        }),
        makeEntry('/dup.ts', {
          paneId: 'pane-2',
          isActive: false,
          cursorPosition: makeCursor(99, 99),
        }),
      ],
      bufferOrder: ['/dup.ts'],
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers).toHaveLength(1);
    expect(loaded!.buffers[0].cursorPosition).toEqual(makeCursor(1, 0));
  });
});

// ===========================================================================
// bufferOrder deduplication
// ===========================================================================

describe('bufferOrder deduplication', () => {
  it('removes duplicate paths from bufferOrder while preserving order', () => {
    const snapshot = makeSnapshot({
      buffers: [makeEntry('/a.ts'), makeEntry('/b.ts')],
      bufferOrder: ['/a.ts', '/b.ts', '/a.ts', '/c.ts', '/b.ts'],
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.bufferOrder).toEqual(['/a.ts', '/b.ts', '/c.ts']);
  });

  it('filters out virtual paths from bufferOrder', () => {
    const snapshot = makeSnapshot({
      buffers: [],
      bufferOrder: ['/real.ts', '__workspace/internal', '/other.ts'],
    });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.bufferOrder).toEqual(['/real.ts', '/other.ts']);
  });
});

// ===========================================================================
// MAX_BUFFERS truncation
// ===========================================================================

describe('MAX_BUFFERS truncation', () => {
  it('truncates to MAX_BUFFERS (50) when exceeded', () => {
    const buffers: BufferLayoutEntry[] = [];
    const bufferOrder: string[] = [];
    for (let i = 0; i < 60; i++) {
      const path = `/file-${String(i).padStart(3, '0')}.ts`;
      buffers.push(makeEntry(path, { cursorPosition: makeCursor(i, 0) }));
      bufferOrder.push(path);
    }
    saveLayoutSnapshot(makeSnapshot({ buffers, bufferOrder }));
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers.length).toBeLessThanOrEqual(50);
    expect(loaded!.bufferOrder.length).toBeLessThanOrEqual(50);
  });

  it('preserves the activeBufferFilePath buffer when truncating', () => {
    const buffers: BufferLayoutEntry[] = [];
    const bufferOrder: string[] = [];
    for (let i = 0; i < 60; i++) {
      const path = `/file-${String(i).padStart(3, '0')}.ts`;
      buffers.push(
        makeEntry(path, {
          isActive: i === 55,
          cursorPosition: makeCursor(i, 0),
        }),
      );
      bufferOrder.push(path);
    }
    saveLayoutSnapshot(
      makeSnapshot({
        activeBufferFilePath: '/file-055.ts',
        buffers,
        bufferOrder,
      }),
    );
    flush();

    const loaded = loadLayoutSnapshot();
    const paths = loaded!.buffers.map((b) => b.filePath);
    expect(paths).toContain('/file-055.ts');
    expect(loaded!.buffers.length).toBeLessThanOrEqual(50);
  });

  it('preserves all active-buffer entries when truncating', () => {
    const buffers: BufferLayoutEntry[] = [];
    const bufferOrder: string[] = [];
    for (let i = 0; i < 60; i++) {
      const path = `/file-${String(i).padStart(3, '0')}.ts`;
      buffers.push(
        makeEntry(path, {
          isActive: i === 0 || i === 59,
        }),
      );
      bufferOrder.push(path);
    }
    saveLayoutSnapshot(makeSnapshot({ buffers, bufferOrder }));
    flush();

    const loaded = loadLayoutSnapshot();
    const paths = loaded!.buffers.map((b) => b.filePath);
    expect(paths).toContain('/file-000.ts');
    expect(paths).toContain('/file-059.ts');
  });

  it('drops the oldest buffers (lowest bufferOrder index) first when truncating', () => {
    // bufferOrder: file-000 (oldest, idx 0) … file-054 (newest, idx 54).
    // The code sorts by bufferOrder index descending (highest = newest first),
    // then keeps the top 50.
    const buffers: BufferLayoutEntry[] = [];
    const bufferOrder: string[] = [];
    for (let i = 0; i < 55; i++) {
      const path = `/file-${String(i).padStart(3, '0')}.ts`;
      buffers.push(makeEntry(path));
      bufferOrder.push(path);
    }

    saveLayoutSnapshot(makeSnapshot({ buffers, bufferOrder }));
    flush();

    const loaded = loadLayoutSnapshot();
    const paths = loaded!.buffers.map((b) => b.filePath);
    expect(loaded!.buffers.length).toBe(50);
    // file-000 through file-004 have the lowest bufferOrder indices (oldest)
    expect(paths).not.toContain('/file-000.ts');
    expect(paths).not.toContain('/file-001.ts');
    expect(paths).not.toContain('/file-002.ts');
    expect(paths).not.toContain('/file-003.ts');
    expect(paths).not.toContain('/file-004.ts');
    // The newest should survive
    expect(paths).toContain('/file-054.ts');
  });

  it('does not truncate when buffers are exactly at MAX_BUFFERS', () => {
    const buffers: BufferLayoutEntry[] = [];
    const bufferOrder: string[] = [];
    for (let i = 0; i < 50; i++) {
      const path = `/file-${String(i).padStart(3, '0')}.ts`;
      buffers.push(makeEntry(path));
      bufferOrder.push(path);
    }
    saveLayoutSnapshot(makeSnapshot({ buffers, bufferOrder }));
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded!.buffers).toHaveLength(50);
    expect(loaded!.bufferOrder).toHaveLength(50);
  });
});

// ===========================================================================
// dispose
// ===========================================================================

describe('dispose', () => {
  it('flushes pending snapshot and clears the debounce timer', () => {
    const snapshot = makeSnapshot({ activePaneId: 'timer-test' });
    saveLayoutSnapshot(snapshot);

    dispose();

    vi.advanceTimersByTime(2_000);

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('timer-test');
  });

  it('flushes pending writes including buffer data before cleaning up', () => {
    const snapshot = makeSnapshot({
      activePaneId: 'disposed-flush',
      buffers: [makeEntry('/pending.ts')],
      bufferOrder: ['/pending.ts'],
    });
    saveLayoutSnapshot(snapshot);

    dispose();

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('disposed-flush');
    expect(loaded!.buffers).toHaveLength(1);
  });

  it('removes the beforeunload listener', () => {
    initBeforeUnloadFlush();
    expect(getBeforeUnloadListeners().length).toBe(1);

    dispose();

    expect(getBeforeUnloadListeners().length).toBe(0);
  });

  it('is safe to call when nothing has been initialised', () => {
    expect(() => dispose()).not.toThrow();
  });

  it('subsequent saveLayoutSnapshot after dispose still works', () => {
    dispose();

    const snapshot = makeSnapshot({ activePaneId: 'after-dispose' });
    saveLayoutSnapshot(snapshot);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('after-dispose');
  });
});

// ===========================================================================
// initBeforeUnloadFlush
// ===========================================================================

describe('initBeforeUnloadFlush', () => {
  it('registers a beforeunload listener', () => {
    initBeforeUnloadFlush();
    expect(getBeforeUnloadListeners().length).toBe(1);
  });

  it('the listener flushes pending snapshots on beforeunload', () => {
    initBeforeUnloadFlush();

    const snapshot = makeSnapshot({
      activePaneId: 'beforeunload-test',
      buffers: [makeEntry('/unload.ts')],
      bufferOrder: ['/unload.ts'],
    });
    saveLayoutSnapshot(snapshot);

    fireBeforeUnload();

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('beforeunload-test');
  });

  it('calling initBeforeUnloadFlush twice does not register two listeners', () => {
    initBeforeUnloadFlush();
    initBeforeUnloadFlush();
    expect(getBeforeUnloadListeners().length).toBe(1);
  });

  it('dispose removes the listener so a second init re-registers', () => {
    initBeforeUnloadFlush();
    dispose();
    expect(getBeforeUnloadListeners().length).toBe(0);

    initBeforeUnloadFlush();
    expect(getBeforeUnloadListeners().length).toBe(1);

    dispose();
  });
});

// ===========================================================================
// Full integration round-trip
// ===========================================================================

describe('full round-trip', () => {
  it('save → flush → load returns equivalent data', () => {
    const original: LayoutSnapshot = {
      version: 1,
      activePaneId: 'main-pane',
      activeBufferFilePath: '/src/main.ts',
      buffers: [
        makeEntry('/src/main.ts', {
          paneId: 'main-pane',
          isActive: true,
          cursorPosition: makeCursor(42, 13),
          scrollPosition: makeScroll(500, 0),
        }),
        makeEntry('/lib/util.ts', {
          paneId: 'side-pane',
          isActive: true,
          cursorPosition: makeCursor(7, 2),
          scrollPosition: makeScroll(100, 0),
        }),
      ],
      bufferOrder: ['/lib/util.ts', '/src/main.ts'],
    };

    saveLayoutSnapshot(original);
    flush();

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.version).toBe(1);
    expect(loaded!.activePaneId).toBe('main-pane');
    expect(loaded!.activeBufferFilePath).toBe('/src/main.ts');
    expect(loaded!.buffers).toHaveLength(2);
    expect(loaded!.bufferOrder).toEqual(['/lib/util.ts', '/src/main.ts']);
  });

  it('save with debounce → let timer fire → load', () => {
    const snapshot = makeSnapshot({
      activePaneId: 'debounced',
      buffers: [makeEntry('/debounced.ts', { cursorPosition: makeCursor(10, 5) })],
      bufferOrder: ['/debounced.ts'],
    });

    saveLayoutSnapshot(snapshot);
    vi.advanceTimersByTime(1_000);

    const loaded = loadLayoutSnapshot();
    expect(loaded).not.toBeNull();
    expect(loaded!.activePaneId).toBe('debounced');
    expect(loaded!.buffers[0].cursorPosition).toEqual(makeCursor(10, 5));
  });
});

// ===========================================================================
// Pane structure persistence: closing a pane must not strand the size map
// ===========================================================================
//
// Regression: closing the *primary* pane (pane-1) and keeping a split-created
// pane left localStorage holding a pane list without pane-1, while paneSizes
// and paneLayout were written by separate effects. loadSavedPanes() rejected
// the list (it required pane-1 and >= 2 panes), so restore skipped the pane
// block entirely and the app booted with a default pane-1 while paneSizes on
// disk still described the survivor. The size map and the pane list then
// disagreed, and the bad geometry survived reload.

describe('loadSavedPanes', () => {
  it('accepts a single surviving pane that is not pane-1', () => {
    // Exactly the state observed after closing pane-1 and keeping the
    // split-created pane.
    window.localStorage.setItem(
      getPanesStorageKey(),
      JSON.stringify([{ id: 'pane-1792502003579', position: 'secondary' }]),
    );

    const panes = loadSavedPanes();
    expect(panes).not.toBeNull();
    expect(panes).toEqual([{ id: 'pane-1792502003579', position: 'secondary' }]);
  });

  it('accepts a single pane-1 (the default, unsplit state)', () => {
    window.localStorage.setItem(getPanesStorageKey(), JSON.stringify([{ id: 'pane-1', position: 'primary' }]));

    const panes = loadSavedPanes();
    expect(panes).not.toBeNull();
    expect(panes).toEqual([{ id: 'pane-1', position: 'primary' }]);
  });

  it('accepts a split whose primary pane was closed', () => {
    window.localStorage.setItem(
      getPanesStorageKey(),
      JSON.stringify([
        { id: 'pane-2', position: 'secondary' },
        { id: 'pane-3', position: 'tertiary' },
      ]),
    );

    const panes = loadSavedPanes();
    expect(panes).not.toBeNull();
    expect(panes).toHaveLength(2);
  });

  it('returns null for an empty or malformed list', () => {
    window.localStorage.setItem(getPanesStorageKey(), JSON.stringify([]));
    expect(loadSavedPanes()).toBeNull();

    window.localStorage.setItem(getPanesStorageKey(), 'not json');
    expect(loadSavedPanes()).toBeNull();
  });

  it('drops entries without a string id', () => {
    window.localStorage.setItem(
      getPanesStorageKey(),
      JSON.stringify([{ id: 'pane-1', position: 'primary' }, { position: 'secondary' }, null]),
    );
    expect(loadSavedPanes()).toEqual([{ id: 'pane-1', position: 'primary' }]);
  });
});

describe('reconcilePaneSizes', () => {
  it('drops a closed pane and renormalises the survivor to 100', () => {
    // The observed broken state: sizes mention only the surviving pane.
    const result = reconcilePaneSizes({ 'pane-1792502003579': 100 }, ['pane-1792502003579']);
    expect(result).toEqual({ 'pane-1792502003579': 100 });
  });

  it('drops entries for panes that no longer exist', () => {
    const result = reconcilePaneSizes({ 'pane-1': 50, 'pane-2': 50 }, ['pane-2']);
    expect(result).toEqual({ 'pane-2': 100 });
  });

  it('preserves proportions when every pane has a size', () => {
    const result = reconcilePaneSizes({ a: 30, b: 70 }, ['a', 'b']);
    expect(result!.a).toBeCloseTo(30);
    expect(result!.b).toBeCloseTo(70);
  });

  it('renormalises sizes that do not sum to 100', () => {
    const result = reconcilePaneSizes({ a: 1, b: 1 }, ['a', 'b']);
    expect(result!.a).toBeCloseTo(50);
    expect(result!.b).toBeCloseTo(50);
  });

  it('gives a newly added pane an even share of the remainder', () => {
    const result = reconcilePaneSizes({ a: 50 }, ['a', 'b']);
    expect(result!.b).toBeCloseTo(50);
    expect(result!.a).toBeCloseTo(50);
  });

  it('returns null when nothing usable is saved', () => {
    expect(reconcilePaneSizes(null, ['a'])).toBeNull();
    expect(reconcilePaneSizes({}, ['a'])).toBeNull();
    expect(reconcilePaneSizes({ b: 50 }, ['a'])).toBeNull();
    expect(reconcilePaneSizes({ a: 0, b: 0 }, ['a', 'b'])).toBeNull();
    expect(reconcilePaneSizes({ a: 50 }, [])).toBeNull();
  });

  it('ignores non-finite and non-numeric entries', () => {
    const result = reconcilePaneSizes({ a: 50, b: Number.NaN } as unknown as Record<string, number>, ['a', 'b']);
    expect(result!.a).toBeCloseTo(50);
    expect(result!.b).toBeCloseTo(50);
  });

  it('always sums to 100', () => {
    const result = reconcilePaneSizes({ a: 10, b: 20, c: 30 }, ['a', 'b', 'c', 'd']);
    const total = Object.values(result!).reduce((s, v) => s + v, 0);
    expect(total).toBeCloseTo(100);
  });
});

describe('evenPaneSizes', () => {
  it('splits evenly', () => {
    expect(evenPaneSizes(['a', 'b'])).toEqual({ a: 50, b: 50 });
    expect(evenPaneSizes(['a'])).toEqual({ a: 100 });
  });

  it('handles the empty list', () => {
    expect(evenPaneSizes([])).toEqual({});
  });
});

// ===========================================================================
// Utility
// ===========================================================================

function getBeforeUnloadListeners(): Array<() => void> {
  return beforeUnloadHandlers;
}

function fireBeforeUnload(): void {
  beforeUnloadHandlers.forEach((handler) => handler());
}
