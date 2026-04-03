// @ts-nocheck
/**
 * linkedScroll.test.ts — Unit tests for the linkedScroll extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * `@codemirror/view` and `@codemirror/state` are mocked.  The mock
 * captures the plugin class passed to `ViewPlugin.fromClass()` so we
 * can instantiate it directly and test the `update()` method with
 * lightweight mock objects.
 *
 * The `requestAnimationFrame` / `cancelAnimationFrame` globals are also
 * mocked so that tests can deterministically flush pending frame
 * callbacks instead of relying on the real browser scheduler.
 *
 * The `_syncingPaneIds` re-entrancy guard and `_linkedScrollEnabled`
 * module-level state are exercised through the public API and by
 * observing dispatched CustomEvents.
 */

// ── Mock requestAnimationFrame / cancelAnimationFrame ───────────

let _rafIdCounter = 0;
const _pendingRafCallbacks = new Map<number, FrameRequestCallback>();
const _cancelledRafIds = new Set<number>();

function mockRaf(callback: FrameRequestCallback): number {
  const id = ++_rafIdCounter;
  _pendingRafCallbacks.set(id, callback);
  return id;
}

function mockCancelRaf(id: number): void {
  _cancelledRafIds.add(id);
  _pendingRafCallbacks.delete(id);
}

/**
 * Execute all pending requestAnimationFrame callbacks in insertion
 * order.  Returns the number of callbacks flushed.
 */
function flushAnimationFrame(): number {
  let count = 0;
  // Snapshot current IDs so callbacks that schedule new rAFs don't
  // cause an infinite loop here.
  const ids = [..._pendingRafCallbacks.keys()];
  for (const id of ids) {
    const cb = _pendingRafCallbacks.get(id);
    if (cb !== undefined) {
      _pendingRafCallbacks.delete(id);
      const elapsed = performance.now();
      cb(elapsed);
      count++;
    }
  }
  return count;
}

function resetRafMock(): void {
  _rafIdCounter = 0;
  _pendingRafCallbacks.clear();
  _cancelledRafIds.clear();
}

// Install the mocks before any module that uses them is imported.
globalThis.requestAnimationFrame = mockRaf as any;
globalThis.cancelAnimationFrame = mockCancelRaf as any;

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

let mockCapturedPluginClass: any = null;

jest.mock('@codemirror/view', () => ({
  EditorView: class MockEditorView {},
  ViewPlugin: {
    fromClass(cls: any) {
      mockCapturedPluginClass = cls;
      // Return an empty array as the Extension value
      return [];
    },
    /** Test helper exposed on the mock for retrieving the captured class. */
    _getCapturedClass() {
      return mockCapturedPluginClass;
    },
    /** Test helper to reset the captured class between tests. */
    _resetCapturedClass() {
      mockCapturedPluginClass = null;
    },
  },
}));

jest.mock('@codemirror/state', () => ({}));

// Module under test — safe to import because CM deps are mocked.
import {
  linkedScrollExtension,
  setLinkedScrollEnabled,
  isLinkedScrollEnabled,
  suppressScrollSync,
  _resetModuleStateForTesting,
} from './linkedScroll';

// Grab the mock ViewPlugin for helper access.
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { ViewPlugin } = require('@codemirror/view');

// ── Helpers ─────────────────────────────────────────────────────────

/**
 * Create a mock EditorView suitable for the LinkedScrollPlugin constructor.
 * `viewport.from` controls which document position the viewport starts at;
 * `doc.lineAt` returns a line object with the given `number`.
 */
function createMockView(opts: { viewportFrom?: number; lineCount?: number } = {}) {
  const {
    viewportFrom = 0,
    lineCount = 100,
  } = opts;

  // Build simple mock lines.  lineAt(pos) searches from the end so that
  // the highest `from <= pos` wins (mirrors real CM behaviour).
  const lines: Array<{ number: number; from: number; to: number }> = [];
  let pos = 0;
  for (let i = 1; i <= lineCount; i++) {
    const from = pos;
    const to = pos + 10; // each mock line is 10 chars
    lines.push({ number: i, from, to });
    pos = to + 1; // +1 for the newline
  }

  const lineAt = (p: number) => {
    for (let i = lines.length - 1; i >= 0; i--) {
      if (p >= lines[i].from) return lines[i];
    }
    return lines[0];
  };

  return {
    viewport: { from: viewportFrom },
    state: { doc: { lineAt, lines: lineCount } },
  };
}

/**
 * Create a minimal ViewUpdate mock — only the fields the plugin reads.
 */
function createMockUpdate(
  view: any,
  overrides: { viewportChanged?: boolean } = {},
) {
  return {
    view,
    state: view.state,
    viewportChanged: overrides.viewportChanged ?? true,
  };
}

/**
 * Register a spy on `document.dispatchEvent` and return both the spy
 * and an array of captured CustomEvent objects.
 */
function spyOnDispatch() {
  const captured: CustomEvent[] = [];
  const orig = document.dispatchEvent.bind(document);
  const spy = jest.fn((event: Event) => {
    captured.push(event as CustomEvent);
    return true;
  });
  document.dispatchEvent = spy as any;
  return {
    spy,
    captured,
    restore() {
      document.dispatchEvent = orig;
    },
  };
}

/**
 * Flush the microtask queue so `queueMicrotask` callbacks run.
 */
function flushMicrotasks(): Promise<void> {
  return new Promise((resolve) => queueMicrotask(resolve));
}

// ── Tests ───────────────────────────────────────────────────────────

describe('linkedScroll', () => {
  let dispatch: ReturnType<typeof spyOnDispatch>;

  beforeEach(() => {
    // Reset all module-level state to a clean baseline.
    _resetModuleStateForTesting();
    resetRafMock();
    ViewPlugin._resetCapturedClass();
    dispatch = spyOnDispatch();
  });

  afterEach(() => {
    dispatch.restore();
    // Always leave module state clean so other test files are unaffected.
    _resetModuleStateForTesting();
    resetRafMock();
  });

  // ==================================================================
  // Module-level API: setLinkedScrollEnabled / isLinkedScrollEnabled
  // ==================================================================

  describe('module API', () => {
    it('isLinkedScrollEnabled returns false by default', () => {
      expect(isLinkedScrollEnabled()).toBe(false);
    });

    it('setLinkedScrollEnabled(true) enables linked scrolling', () => {
      setLinkedScrollEnabled(true);
      expect(isLinkedScrollEnabled()).toBe(true);
    });

    it('setLinkedScrollEnabled(false) disables linked scrolling', () => {
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);
      expect(isLinkedScrollEnabled()).toBe(false);
    });

    it('toggling multiple times retains the last value', () => {
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);
      setLinkedScrollEnabled(true);
      expect(isLinkedScrollEnabled()).toBe(true);
    });

    it('calling setLinkedScrollEnabled(false) when already false is a no-op', () => {
      setLinkedScrollEnabled(false);
      expect(isLinkedScrollEnabled()).toBe(false);
    });
  });

  // ==================================================================
  // ViewPlugin factory: linkedScrollExtension
  // ==================================================================

  describe('linkedScrollExtension', () => {
    it('returns a value (the CodeMirror Extension) from ViewPlugin.fromClass', () => {
      const ext = linkedScrollExtension('p1', () => '/file.ts');
      // The mock returns []; the real CM returns an Extension array.
      expect(Array.isArray(ext)).toBe(true);
    });

    it('registers a plugin class with ViewPlugin', () => {
      linkedScrollExtension('p1', () => '/file.ts');
      const cls = ViewPlugin._getCapturedClass();
      expect(cls).not.toBeNull();
      expect(typeof cls).toBe('function');
    });

    it('each call creates a fresh plugin class (distinct closure over paneId)', () => {
      const ext1 = linkedScrollExtension('pane-A', () => '/a.ts');
      const classA = ViewPlugin._getCapturedClass();

      const ext2 = linkedScrollExtension('pane-B', () => '/b.ts');
      const classB = ViewPlugin._getCapturedClass();

      // Each call should produce a new class (different closure).
      expect(classA).not.toBe(classB);
    });
  });

  // ==================================================================
  // Plugin update behaviour
  // ==================================================================

  describe('LinkedScrollPlugin update', () => {
    /**
     * Helper: create a plugin instance for a given paneId / filePath.
     */
    function createPlugin(
      paneId = 'pane-1',
      getFilePath: () => string | null = () => '/test/file.ts',
      viewOverrides: { viewportFrom?: number; lineCount?: number } = {},
    ) {
      ViewPlugin._resetCapturedClass();
      linkedScrollExtension(paneId, getFilePath);

      const PluginClass = ViewPlugin._getCapturedClass();
      const view = createMockView(viewOverrides);
      const plugin = new PluginClass(view);
      return { plugin, view };
    }

    // ── Happy path ───────────────────────────────────────────────

    it('dispatches editor:linked-scroll event when enabled and viewport changes', () => {
      const { plugin, view } = createPlugin('p1', () => '/foo.ts');
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured).toHaveLength(1);
      expect(dispatch.captured[0].type).toBe('editor:linked-scroll');
    });

    it('event detail includes correct sourcePaneId, filePath, and topLine', () => {
      // viewport.from = 0 → lineAt(0) returns line with from=0, number=1
      const { plugin, view } = createPlugin('my-pane', () => '/path/to/src.ts', {
        viewportFrom: 0,
      });
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured[0].detail).toEqual({
        sourcePaneId: 'my-pane',
        filePath: '/path/to/src.ts',
        topLine: 1,
      });
    });

    it('computes correct topLine when viewport starts at a later position', () => {
      // Each mock line is 10 chars + 1 newline = 11 chars.
      // Line 1: from=0, line 2: from=11, line 3: from=22, …
      // viewport.from = 11 → lineAt(11) → line 2
      const { plugin, view } = createPlugin('p1', () => '/f.ts', {
        viewportFrom: 11,
      });
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured[0].detail.topLine).toBe(2);
    });

    // ── Disabled state ──────────────────────────────────────────

    it('does NOT dispatch when linked scroll is disabled', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      // setLinkedScrollEnabled(false) is the default from beforeEach

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured).toHaveLength(0);
    });

    it('does NOT dispatch after being disabled mid-session', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      await flushMicrotasks();
      expect(dispatch.captured).toHaveLength(1);

      setLinkedScrollEnabled(false);
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1); // no additional dispatch
    });

    // ── viewportChanged = false ─────────────────────────────────

    it('does NOT dispatch when viewportChanged is false', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: false }));
      flushAnimationFrame();

      expect(dispatch.captured).toHaveLength(0);
    });

    // ── Null / empty filePath ───────────────────────────────────

    it('does NOT dispatch when getFilePath returns null', () => {
      const { plugin, view } = createPlugin('p1', () => null);
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured).toHaveLength(0);
    });

    it('does NOT dispatch when getFilePath returns empty string', () => {
      const { plugin, view } = createPlugin('p1', () => '');
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();

      expect(dispatch.captured).toHaveLength(0);
    });

    it('calls getFilePath on each update (not cached at construction)', async () => {
      let filePath: string | null = null;
      const getFP = () => filePath;
      const { plugin, view } = createPlugin('p1', getFP);
      setLinkedScrollEnabled(true);

      // First update: no file path yet
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(0);

      // Now provide a path — guard from first update already cleared
      // (no dispatch happened, no microtask was scheduled)
      filePath = '/now/visible.ts';
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);
      expect(dispatch.captured[0].detail.filePath).toBe('/now/visible.ts');
    });

    // ── Re-entrancy guard ───────────────────────────────────────

    it('does NOT dispatch when the source pane is in the syncing set', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      // First update: normal dispatch, pane added to _syncingPaneIds
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);

      // Immediately call update again — the pane is still in the syncing
      // set (microtask hasn't run yet), so it should be suppressed at
      // the guard check in update(), before any rAF is scheduled.
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1); // no second event
    });

    it('clears the re-entrancy guard after a microtask', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);

      // Flush the microtask queue so the guard clears
      await flushMicrotasks();

      // Now a subsequent update should schedule a new rAF and dispatch
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(2);
    });

    it('different pane IDs are guard-independent', () => {
      // Create two plugins with different pane IDs
      ViewPlugin._resetCapturedClass();
      linkedScrollExtension('pane-A', () => '/f.ts');
      const ClassA = ViewPlugin._getCapturedClass();
      const viewA = createMockView();
      const pluginA = new ClassA(viewA);

      ViewPlugin._resetCapturedClass();
      linkedScrollExtension('pane-B', () => '/f.ts');
      const ClassB = ViewPlugin._getCapturedClass();
      const viewB = createMockView();
      const pluginB = new ClassB(viewB);

      setLinkedScrollEnabled(true);

      // Scroll pane-A — it enters guard, but pane-B is unaffected
      pluginA.update(createMockUpdate(viewA, { viewportChanged: true }));
      pluginB.update(createMockUpdate(viewB, { viewportChanged: true }));
      flushAnimationFrame();

      // Both should dispatch because they have different pane IDs
      const sourcePaneIds = dispatch.captured.map((e) => e.detail.sourcePaneId);
      expect(sourcePaneIds).toEqual(['pane-A', 'pane-B']);
    });

    // ── suppressScrollSync (echo-loop prevention) ──────────────

    it('suppressScrollSync prevents dispatch from the suppressed pane', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      // Simulate the receiver path: mark pane as receiving sync
      suppressScrollSync('p1');

      // The guard check in update() sees p1 in _syncingPaneIds,
      // so no rAF is scheduled at all.
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(0);
    });

    it('suppressScrollSync guard clears after microtask', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      suppressScrollSync('p1');
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(0);

      // After microtask flush, the guard clears and dispatch resumes
      await flushMicrotasks();
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);
    });

    it('echo loop scenario: sender dispatches, receiver is suppressed, no echo', async () => {
      // Create two plugins for different panes
      ViewPlugin._resetCapturedClass();
      linkedScrollExtension('pane-A', () => '/f.ts');
      const ClassA = ViewPlugin._getCapturedClass();
      const viewA = createMockView();
      const pluginA = new ClassA(viewA);

      ViewPlugin._resetCapturedClass();
      linkedScrollExtension('pane-B', () => '/f.ts');
      const ClassB = ViewPlugin._getCapturedClass();
      const viewB = createMockView();
      const pluginB = new ClassB(viewB);

      setLinkedScrollEnabled(true);

      // Step 1: Pane A user scrolls — schedules rAF
      pluginA.update(createMockUpdate(viewA, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);
      expect(dispatch.captured[0].detail.sourcePaneId).toBe('pane-A');

      // Step 2: Pane B receives the event and calls suppressScrollSync
      suppressScrollSync('pane-B');

      // Step 3: Browser eventually fires viewport change on Pane B
      // (from programmatic scroll). With the guard, update() returns
      // early before scheduling any rAF.
      pluginB.update(createMockUpdate(viewB, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1); // still just the one from pane-A
    });

    // ── destroy cleanup ────────────────────────────────────────

    it('destroy removes the pane from the syncing set', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      // Put p1 in the guard via the extension
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      await flushMicrotasks();
      expect(dispatch.captured).toHaveLength(1);

      // Destroy the plugin — should clean up
      plugin.destroy();

      // The guard should be cleared now, so a new plugin can dispatch
      const newPlugin = createPlugin('p1', () => '/f.ts').plugin;
      newPlugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(2);
    });

    it('destroy cancels pending rAF dispatch', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      // Schedule rAF but do NOT flush — the rAF is pending
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      expect(dispatch.captured).toHaveLength(0); // not dispatched yet

      // Destroy after scheduling but before flush
      plugin.destroy();

      // Flush should be a no-op since destroy cancelled the rAF
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(0); // still no dispatch
    });

    // ── Multiple consecutive updates ────────────────────────────

    it('dispatches once per viewport change when guard clears between calls', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      for (let i = 0; i < 3; i++) {
        plugin.update(createMockUpdate(view, { viewportChanged: true }));
        flushAnimationFrame();
        await flushMicrotasks(); // clear guard between calls
      }

      expect(dispatch.captured).toHaveLength(3);
      // All events should have the same detail structure
      for (const event of dispatch.captured) {
        expect(event.detail).toEqual({
          sourcePaneId: 'p1',
          filePath: '/f.ts',
          topLine: 1,
        });
      }
    });

    // ── rAF debounce ───────────────────────────────────────────

    it('coalesces rapid scroll updates into a single dispatch per frame', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');
      setLinkedScrollEnabled(true);

      // Simulate rapid scroll events — each call cancels the previous rAF
      // and schedules a new one.
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      plugin.update(createMockUpdate(view, { viewportChanged: true }));

      // Only the last rAF should fire (the first two were cancelled)
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);
    });

    // ── Constructor stores the view reference ──────────────────

    it('stores the EditorView on the plugin instance', () => {
      const { plugin, view } = createPlugin('p1');
      expect(plugin.view).toBe(view);
    });

    // ── filePath changes across updates ─────────────────────────

    it('uses the current filePath at the time of each update', async () => {
      let currentPath = '/initial/file.ts';
      const getFP = () => currentPath;
      const { plugin, view } = createPlugin('p1', getFP);
      setLinkedScrollEnabled(true);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured[0].detail.filePath).toBe('/initial/file.ts');

      // Change file path — wait for guard to clear first
      currentPath = '/changed/file.ts';
      await flushMicrotasks();
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured[1].detail.filePath).toBe('/changed/file.ts');
    });

    // ── Rapid enable/disable toggling ──────────────────────────

    it('handles rapid enable/disable toggling: only the last state matters at update time', () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');

      // Rapidly toggle the enabled state
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);

      // At update time, enabled is false — no rAF should be scheduled
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(0);

      // Now enable and update again
      setLinkedScrollEnabled(true);
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1);
    });

    it('handles rapid enable/disable toggling across multiple updates', async () => {
      const { plugin, view } = createPlugin('p1', () => '/f.ts');

      // Enable, dispatch, then rapidly toggle
      setLinkedScrollEnabled(true);
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      await flushMicrotasks();
      expect(dispatch.captured).toHaveLength(1);

      // Rapidly toggle: end state is disabled
      setLinkedScrollEnabled(false);
      setLinkedScrollEnabled(true);
      setLinkedScrollEnabled(false);

      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(1); // no additional dispatch

      // Enable again
      setLinkedScrollEnabled(true);
      await flushMicrotasks();
      plugin.update(createMockUpdate(view, { viewportChanged: true }));
      flushAnimationFrame();
      expect(dispatch.captured).toHaveLength(2);
    });
  });
});
