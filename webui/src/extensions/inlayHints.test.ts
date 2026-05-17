/**
 * inlayHints.test.ts — Unit tests for the inlayHints CodeMirror extension.
 *
 * Tests the exported factory, widget behavior, language support, LSP guard,
 * decoration building, viewport filtering, debounce, annotation guard,
 * destroy lifecycle, API integration, document change detection, and theme.
 */

import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

declare global {
  var __inlayHintsMocks: {
    getSemanticInlayHints: ReturnType<typeof vi.fn>;
    isLspConnected: ReturnType<typeof vi.fn>;
  };
}

let activePlugins: any[] = [];

vi.mock('@codemirror/view', () => {
  if (!globalThis.__inlayHintsMocks) {
    globalThis.__inlayHintsMocks = {
      getSemanticInlayHints: vi.fn().mockResolvedValue({ inlay_hints: [] }),
      isLspConnected: vi.fn(() => true),
    };
  }
  const WidgetType = class {};
  const Decoration = {
    widget: vi.fn((opts: any) => ({
      _widget: opts.widget,
      _block: opts.block,
      range: vi.fn((f: number) => ({ from: f })),
    })),
    none: { type: 'none' },
    set: vi.fn((r: any, s: boolean) => ({ _ranges: r, _sorted: s })),
  };
  const ViewPlugin = {
    fromClass: vi.fn((cls: any, cfg: any) => ({ _pluginClass: cls, _config: cfg })),
  };
  const EditorView = { baseTheme: vi.fn((c: any) => c) };
  return { Decoration, ViewPlugin, EditorView, WidgetType };
});

vi.mock('@codemirror/state', () => {
  const a: any = { of: vi.fn((v: any) => v), _isInlayHintsAnnotation: true };
  return { Annotation: { define: vi.fn(() => a) } };
});

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: vi.fn(() => ({
      getSemanticInlayHints: (...a: any[]) => globalThis.__inlayHintsMocks.getSemanticInlayHints(...a),
    })),
  },
}));
vi.mock('./lspExtensions', () => ({
  isLSPClientConnected: (...a: any[]) => globalThis.__inlayHintsMocks.isLspConnected(...a),
}));
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

import { ApiService } from '../services/api';
import { inlayHintsExtension } from './inlayHints';
import { Decoration, ViewPlugin } from '@codemirror/view';

const mockDecoration = vi.mocked(Decoration);
const mockViewPlugin = vi.mocked(ViewPlugin);
const mocks = globalThis.__inlayHintsMocks;

function resetMocks() {
  for (const p of activePlugins) {
    try {
      p.destroy();
    } catch {}
  }
  activePlugins = [];
  vi.clearAllMocks();
  mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [] });
  mocks.isLspConnected.mockReturnValue(true);
}

function createMockView(content = '', viewFrom = 0, viewTo = content.length) {
  return {
    state: {
      doc: { toString: vi.fn(() => content), length: content.length, line: vi.fn(), lineAt: vi.fn() },
    },
    viewport: { from: viewFrom, to: viewTo },
    dispatch: vi.fn(),
  };
}

function createPlugin(
  opts: {
    getFilePath?: () => string | undefined;
    languageId?: string | null | undefined;
    initialContent?: string;
    changedContent?: string;
    viewFrom?: number;
    viewTo?: number;
  } = {},
) {
  const getFilePath = opts.getFilePath ?? (() => '/test.ts');
  const lang = opts.hasOwnProperty('languageId') ? opts.languageId : 'typescript';
  const init = opts.initialContent ?? 'initial';
  const changed = opts.changedContent ?? 'const x = 42;';

  const ext = inlayHintsExtension(getFilePath, () => changed, lang);
  const Cls = (ext[1] as any)._pluginClass;
  const mv = createMockView(init, opts.viewFrom ?? 0, opts.viewTo ?? changed.length);
  const plugin = new Cls(mv);
  activePlugins.push(plugin);

  mv.state.doc.toString.mockReturnValue(changed);
  mv.state.doc.length = changed.length;
  plugin.update({ docChanged: true, viewportChanged: false, transactions: [], view: mv });

  return { plugin, mockView: mv, ext, changedContent: changed };
}

const flush = (ms = 1000) => new Promise<void>((r) => setTimeout(r, ms));

// ═══════════════════════════════════════════════════════════════════

describe('InlayHintWidget', () => {
  beforeEach(() => {
    resetMocks();
    mocks.getSemanticInlayHints.mockResolvedValue({
      inlay_hints: [
        { from: 0, to: 5, label: ': number', kind: 'type' },
        { from: 10, to: 12, label: ' = arg', kind: 'parameter' },
      ],
    });
  });

  it('creates widgets with correct label and kind', async () => {
    createPlugin();
    await flush(600);
    const c = mockDecoration.widget.mock.calls;
    expect(c.length).toBeGreaterThanOrEqual(2);
    expect(c[0][0].widget.label).toBe(': number');
    expect(c[0][0].widget.kind).toBe('type');
    expect(c[1][0].widget.label).toBe(' = arg');
    expect(c[1][0].widget.kind).toBe('parameter');
  });

  it('type hint DOM', async () => {
    createPlugin();
    await flush(600);
    const d = mockDecoration.widget.mock.calls[0][0].widget.toDOM();
    expect(d.tagName).toBe('SPAN');
    expect(d.className).toContain('cm-inlayHint-type');
    expect(d.textContent).toBe(': number');
    expect(d.getAttribute('role')).toBe('presentation');
  });

  it('parameter hint DOM', async () => {
    createPlugin();
    await flush(600);
    const d = mockDecoration.widget.mock.calls[1][0].widget.toDOM();
    expect(d.className).toContain('cm-inlayHint-parameter');
    expect(d.textContent).toBe(' = arg');
  });

  it('none kind DOM', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: '→', kind: 'none' }] });
    createPlugin();
    await flush(600);
    const d = mockDecoration.widget.mock.calls[0][0].widget.toDOM();
    expect(d.className).toContain('cm-inlayHint');
    expect(d.className).not.toContain('cm-inlayHint-type');
  });

  it('eq true', async () => {
    createPlugin();
    await flush(600);
    const widget = mockDecoration.widget.mock.calls[0][0].widget;
    // eq should return true for a widget with the same label and kind of the same type
    // Create a comparable widget using the same constructor
    const sameWidget = Object.create(Object.getPrototypeOf(widget));
    sameWidget.label = ': number';
    sameWidget.kind = 'type';
    expect(widget.eq(sameWidget)).toBe(true);
  });
  it('eq false (label)', async () => {
    createPlugin();
    await flush(600);
    const widget = mockDecoration.widget.mock.calls[0][0].widget;
    const diffWidget = Object.create(Object.getPrototypeOf(widget));
    diffWidget.label = ': x';
    diffWidget.kind = 'type';
    expect(widget.eq(diffWidget)).toBe(false);
  });
  it('eq false (kind)', async () => {
    createPlugin();
    await flush(600);
    const widget = mockDecoration.widget.mock.calls[0][0].widget;
    const diffWidget = Object.create(Object.getPrototypeOf(widget));
    diffWidget.label = ': number';
    diffWidget.kind = 'parameter';
    expect(widget.eq(diffWidget)).toBe(false);
  });
  it('eq false (non-widget)', async () => {
    createPlugin();
    await flush(600);
    const widget = mockDecoration.widget.mock.calls[0][0].widget;
    expect(widget.eq({ label: ': number', kind: 'type' } as any)).toBe(false);
  });
  it('ignoreEvent', async () => {
    createPlugin();
    await flush(600);
    expect(mockDecoration.widget.mock.calls[0][0].widget.ignoreEvent(new Event('click'))).toBe(true);
  });
});

describe('inlayHintsExtension factory', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('returns [theme, ViewPlugin]', () => {
    const e = inlayHintsExtension(
      () => '',
      () => '',
      'typescript',
    );
    expect(Array.isArray(e)).toBe(true);
    expect(e.length).toBe(2);
  });
  it('theme has CSS', () => {
    const t = inlayHintsExtension(
      () => '',
      () => '',
      'typescript',
    )[0] as any;
    expect(t).toHaveProperty('.cm-inlayHint');
  });
  it('ViewPlugin config', () => {
    inlayHintsExtension(
      () => '',
      () => '',
      'typescript',
    );
    const [, cfg] = mockViewPlugin.fromClass.mock.calls[0];
    expect(cfg.decorations({ decorations: 'x' })).toBe('x');
  });
  it('null/undefined languageId', () => {
    expect(
      Array.isArray(
        inlayHintsExtension(
          () => '',
          () => '',
          null,
        ),
      ),
    ).toBe(true);
    expect(
      Array.isArray(
        inlayHintsExtension(
          () => '',
          () => '',
          undefined,
        ),
      ),
    ).toBe(true);
  });
});

describe('language support', () => {
  beforeEach(() => {
    resetMocks();
  });
  for (const l of ['typescript', 'typescript-jsx', 'javascript', 'javascript-jsx', 'go']) {
    it(`"${l}" fetches`, async () => {
      mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
      createPlugin({ languageId: l });
      await flush(600);
      expect(mocks.getSemanticInlayHints).toHaveBeenCalled();
    });
  }
  for (const l of ['python', 'rust', 'java', 'cpp', 'css', 'html', 'json', 'markdown', '', null, undefined] as any[]) {
    it(`"${l ?? 'undefined'}" rejects`, async () => {
      createPlugin({ languageId: l });
      await flush(600);
      expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
    });
  }
});

describe('LSP guard', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('no fetch when disconnected', async () => {
    mocks.isLspConnected.mockReturnValue(false);
    createPlugin();
    await flush(600);
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
  });
  it('fetches when connected', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
    const { changedContent } = createPlugin();
    await flush(600);
    expect(mocks.getSemanticInlayHints).toHaveBeenCalledWith('/test.ts', changedContent, 'typescript');
  });
});

describe('decoration building', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('creates widgets', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({
      inlay_hints: [
        { from: 0, to: 5, label: ': a', kind: 'type' },
        { from: 10, to: 15, label: ' b', kind: 'parameter' },
      ],
    });
    createPlugin();
    await flush(600);
    expect(mockDecoration.widget.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
  it('block: false', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
    createPlugin();
    await flush(600);
    expect(mockDecoration.widget.mock.calls[0][0].block).toBe(false);
  });
  for (const [label, val] of [
    ['empty', []],
    ['null', null],
    ['missing', undefined],
    ['non-array', 'bad'],
  ] as any[]) {
    it(`no widgets for ${label} hints`, async () => {
      mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: val });
      createPlugin();
      await flush(600);
      expect(mockDecoration.widget).not.toHaveBeenCalled();
    });
  }
});

describe('viewport filtering', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('only viewport hints', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({
      inlay_hints: [
        { from: 0, to: 5, label: 'before', kind: 'type' },
        { from: 10, to: 12, label: 'inside', kind: 'parameter' },
        { from: 22, to: 25, label: 'after', kind: 'type' },
      ],
    });
    createPlugin({ changedContent: 'long content here for testing', viewFrom: 10, viewTo: 20 });
    await flush(600);
    const labels = mockDecoration.widget.mock.calls.map((c: any) => c[0].widget.label);
    expect(labels).toContain('inside');
    expect(labels).not.toContain('before');
    expect(labels).not.toContain('after');
  });
});

describe('sorting', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('sorted=true passed to Decoration.set', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({
      inlay_hints: [
        { from: 20, to: 25, label: 'c', kind: 'type' },
        { from: 5, to: 10, label: 'a', kind: 'parameter' },
      ],
    });
    createPlugin({ changedContent: 'a very long document for sorting test here' });
    await flush(600);
    expect(mockDecoration.set.mock.calls.some((c: any) => c[1] === true)).toBe(true);
  });
});

describe('edge cases', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('undefined path', async () => {
    createPlugin({ getFilePath: () => undefined });
    await flush(600);
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
  });
  it('empty path', async () => {
    createPlugin({ getFilePath: () => '' });
    await flush(600);
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
  });
  it('hint outside viewport', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 50, to: 100, label: 'out', kind: 'type' }] });
    createPlugin({ changedContent: 'short', viewTo: 4 });
    await flush(600);
    expect(mockDecoration.widget).not.toHaveBeenCalled();
  });
  it('clamped to doc length', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 5, to: 999, label: 'c', kind: 'type' }] });
    createPlugin({ changedContent: 'const x = 1;' });
    await flush(600);
    expect(mockDecoration.widget).toHaveBeenCalledTimes(1);
  });
  it('fetch error', async () => {
    mocks.getSemanticInlayHints.mockRejectedValue(new Error('fail'));
    createPlugin();
    await flush(600);
    expect(mockDecoration.widget).not.toHaveBeenCalled();
  });
  it('empty label', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: '', kind: 'type' }] });
    createPlugin();
    await flush(600);
    expect(mockDecoration.widget.mock.calls[0][0].widget.toDOM().textContent).toBe('');
  });
  it('negative pos', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: -5, to: -1, label: 'n', kind: 'type' }] });
    createPlugin({ changedContent: 'hello' });
    await flush(600);
    expect(mockDecoration.widget).not.toHaveBeenCalled();
  });
  it('zero-width', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 3, to: 3, label: 'z', kind: 'type' }] });
    createPlugin({ changedContent: 'hello' });
    await flush(600);
    expect(mockDecoration.widget).toHaveBeenCalledTimes(1);
  });
});

describe('debounce', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetMocks();
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });
  it('waits for debounce period', async () => {
    createPlugin();
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);
    await vi.runAllTimersAsync();
    expect(mocks.getSemanticInlayHints).toHaveBeenCalled();
  });
  it('resets on new update', async () => {
    const { plugin, mockView } = createPlugin();
    vi.advanceTimersByTime(100);
    mockView.state.doc.toString.mockReturnValue('new');
    plugin.update({ docChanged: true, viewportChanged: false, transactions: [], view: mockView });
    vi.advanceTimersByTime(100);
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);
    await vi.runAllTimersAsync();
    expect(mocks.getSemanticInlayHints).toHaveBeenCalled();
  });
});

describe('annotation guard', () => {
  beforeEach(() => {
    resetMocks();
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
  });
  it('skips with internal annotation', async () => {
    const { plugin, mockView } = createPlugin();
    await flush(600);
    const n = mocks.getSemanticInlayHints.mock.calls.length;
    plugin.update({
      docChanged: true,
      viewportChanged: false,
      transactions: [{ annotation: (a: any) => (a._isInlayHintsAnnotation ? true : undefined) }],
      view: mockView,
    });
    await flush(600);
    expect(mocks.getSemanticInlayHints.mock.calls.length).toBe(n);
  });
  it('re-schedules without annotation', async () => {
    const { plugin, mockView } = createPlugin();
    await flush(600);
    mockView.state.doc.toString.mockReturnValue('new stuff');
    plugin.update({
      docChanged: true,
      viewportChanged: false,
      transactions: [{ annotation: () => undefined }],
      view: mockView,
    });
    await flush(600);
    expect(mocks.getSemanticInlayHints.mock.calls.length).toBeGreaterThan(1);
  });
});

describe('view reference tracking', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetMocks();
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('updates this.view after update() is called with reconfiguration', () => {
    const { plugin, mockView: originalView } = createPlugin();
    const newMockView = createMockView('new content', 0, 20);

    // Simulate a reconfiguration transaction
    plugin.update({
      docChanged: false,
      viewportChanged: false,
      transactions: [{ annotation: () => undefined, reconfigured: true }],
      view: newMockView,
    });

    // Verify that this.view has been updated to the new view
    expect((plugin as any).view).toBe(newMockView);
    expect((plugin as any).view).not.toBe(originalView);
  });

  it('view reference is updated before scheduleUpdate is called', () => {
    const { plugin } = createPlugin();
    const newMockView = createMockView('content', 0, 20);

    let viewAtScheduleUpdate: any = null;
    const originalScheduleUpdate = (plugin as any).scheduleUpdate.bind(plugin);
    (plugin as any).scheduleUpdate = () => {
      viewAtScheduleUpdate = (plugin as any).view;
      return originalScheduleUpdate();
    };

    plugin.update({
      docChanged: true,
      viewportChanged: false,
      transactions: [{ annotation: () => undefined }],
      view: newMockView,
    });

    // Verify that this.view was already updated when scheduleUpdate was called
    expect(viewAtScheduleUpdate).toBe(newMockView);
  });

  it('async callback uses this.view instead of captured view parameter', async () => {
    let capturedView: any = null;
    let dispatchedView: any = null;

    // Track the view that gets dispatched to after async fetch
    mocks.getSemanticInlayHints.mockImplementationOnce(() => {
      return new Promise((resolve) => {
        setTimeout(() => {
          resolve({ inlay_hints: [{ from: 0, to: 3, label: ': RESULT', kind: 'type' }] });
        }, 100);
      });
    });

    const { plugin } = createPlugin();

    // Track the view captured by buildViewportDecorations during buildDecorations
    const originalBuildViewportDecorations = (plugin as any).buildViewportDecorations.bind(plugin);
    (plugin as any).buildViewportDecorations = function (view: any) {
      capturedView = view;
      return originalBuildViewportDecorations(view);
    };

    // Track which view dispatch gets called on after async fetch
    const thisViewRef = (plugin as any).view;
    const originalDispatch = thisViewRef.dispatch.bind(thisViewRef);
    thisViewRef.dispatch = (...args: any[]) => {
      dispatchedView = thisViewRef;
      return originalDispatch(...args);
    };

    vi.advanceTimersByTime(600); // Wait for debounce + fetch

    // The buildViewportDecorations should have been called
    expect(capturedView).not.toBeNull();

    // After the async fetch resolves, dispatch should be called
    // This confirms the async callback used this.view (which is the current view)
    expect(dispatchedView).toBe(thisViewRef);
  });
});

describe('destroy', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetMocks();
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });
  it('prevents fetch', () => {
    const { plugin } = createPlugin();
    plugin.destroy();
    vi.advanceTimersByTime(600);
    expect(mocks.getSemanticInlayHints).not.toHaveBeenCalled();
  });
  it('clears cache', () => {
    const { plugin } = createPlugin();
    plugin.destroy();
    expect((plugin as any).cachedHints).toEqual([]);
  });
});

describe('API integration', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('correct params', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [] });
    createPlugin({
      getFilePath: () => '/main.go',
      languageId: 'go',
      initialContent: 'x',
      changedContent: 'package main',
    });
    await flush(600);
    expect(mocks.getSemanticInlayHints).toHaveBeenCalledWith('/main.go', 'package main', 'go');
  });
  it('singleton ApiService', async () => {
    createPlugin();
    await flush(600);
    expect(vi.mocked(ApiService).getInstance).toHaveBeenCalled();
  });
});

describe('document changes', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('re-fetches', async () => {
    mocks.getSemanticInlayHints
      .mockResolvedValueOnce({ inlay_hints: [{ from: 0, to: 3, label: ': a', kind: 'type' }] })
      .mockResolvedValueOnce({ inlay_hints: [{ from: 0, to: 3, label: ': b', kind: 'type' }] });
    const { plugin, mockView } = createPlugin();
    await flush(600);
    expect(mocks.getSemanticInlayHints).toHaveBeenCalledTimes(1);
    mockView.state.doc.toString.mockReturnValue('new content');
    plugin.update({ docChanged: true, viewportChanged: false, transactions: [], view: mockView });
    await flush(600);
    expect(mocks.getSemanticInlayHints).toHaveBeenCalledTimes(2);
  });
  it('skips when content unchanged', async () => {
    mocks.getSemanticInlayHints.mockResolvedValue({ inlay_hints: [{ from: 0, to: 3, label: ': T', kind: 'type' }] });
    const { plugin, mockView } = createPlugin();
    await flush(600);
    const n = mocks.getSemanticInlayHints.mock.calls.length;
    plugin.update({ docChanged: false, viewportChanged: true, transactions: [], view: mockView });
    await flush(600);
    expect(mocks.getSemanticInlayHints.mock.calls.length).toBe(n);
  });
  it('discards stale fetch via generation counter', async () => {
    // First fetch hangs, second resolves immediately — stale result should be discarded
    let resolveStale: (v: any) => void;
    const stalePromise = new Promise((r) => {
      resolveStale = r;
    });
    mocks.getSemanticInlayHints
      .mockImplementationOnce(() => stalePromise)
      .mockResolvedValueOnce({ inlay_hints: [{ from: 0, to: 3, label: ': FRESH', kind: 'type' }] });

    const { plugin, mockView } = createPlugin();
    await flush(600); // First fetch starts but hangs

    // Change content to trigger a second fetch (bumps generation)
    mockView.state.doc.toString.mockReturnValue('completely different');
    plugin.update({ docChanged: true, viewportChanged: false, transactions: [], view: mockView });
    await flush(600); // Second fetch resolves

    // Now resolve the stale first fetch
    resolveStale!({ inlay_hints: [{ from: 0, to: 3, label: ': STALE', kind: 'type' }] });
    await flush(100);

    // Only the fresh result should appear in widgets
    const labels = mockDecoration.widget.mock.calls.map((c: any) => c[0].widget.label);
    expect(labels).toContain(': FRESH');
    expect(labels).not.toContain(': STALE');
  });
});

describe('theme', () => {
  beforeEach(() => {
    resetMocks();
  });
  it('CSS classes', () => {
    const t = inlayHintsExtension(
      () => '',
      () => '',
      'typescript',
    )[0] as any;
    expect(t).toHaveProperty('.cm-inlayHint');
    expect(t).toHaveProperty('.cm-inlayHint-type');
  });
  it('CSS props', () => {
    const b = (
      inlayHintsExtension(
        () => '',
        () => '',
        'typescript',
      )[0] as any
    )['.cm-inlayHint'];
    expect(b.pointerEvents).toBe('none');
    expect(b.userSelect).toBe('none');
    expect(b.fontSize).toBe('0.85em');
    expect(b.verticalAlign).toBe('middle');
  });
  it('dark mode', () => {
    expect(
      inlayHintsExtension(
        () => '',
        () => '',
        'typescript',
      )[0] as any,
    ).toHaveProperty('&dark .cm-inlayHint');
  });
  it('light mode', () => {
    expect(
      inlayHintsExtension(
        () => '',
        () => '',
        'typescript',
      )[0] as any,
    ).toHaveProperty('&light .cm-inlayHint');
  });
  it('distinct colors', () => {
    const t = inlayHintsExtension(
      () => '',
      () => '',
      'typescript',
    )[0] as any;
    expect(t['.cm-inlayHint-type'].color).not.toBe(t['.cm-inlayHint-parameter'].color);
  });
});
