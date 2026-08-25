/**
 * useEditorDiagnostics.test.ts — Unit tests for the useEditorDiagnostics hook.
 *
 * Covers:
 * - isSemanticLanguage helper (semantic vs non-semantic languages)
 * - fetchDiagnostics with no viewRef (early return)
 * - LSP client active → skip semantic diagnostics
 * - Semantic diagnostics success path
 * - Semantic diagnostics with no diagnostic capabilities → fallback
 * - Semantic diagnostics error → fallback to basic
 * - Basic diagnostics success path
 * - Basic diagnostics error → clear diagnostics
 * - Empty/no diagnostics → clear existing diagnostics
 * - fetchDiagnosticsRef stays in sync
 * - Debounced cleanup on unmount
 * - Unmount guard during async operations
 * - Edit-trigger fetch debounce/coalescing (rapid edits → one request, latest content)
 * - Save trigger bypasses the pending debounce (fires immediately)
 * - Unmount cancels a pending debounced fetch
 */
// @ts-nocheck
import { act, createElement } from 'react';
import { flushSync } from 'react-dom';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — all mock state is created in vi.hoisted() to avoid TDZ errors.
// vi.mock factories are hoisted above const/let declarations, so any variable
// they reference must be available at hoist time.
// ---------------------------------------------------------------------------

const mocks = vi.hoisted(() => {
  const mockResolveLanguageId = vi.fn();
  const mockClearDiagnostics = vi.fn();
  const mockDebouncedUpdate = vi.fn();
  const mockGetClientForLanguageSync = vi.fn();
  const mockGetLSPClientService = vi.fn();
  const mockGetLSPState = vi.fn();
  const mockGetInstance = vi.fn();
  const mockGetSemanticDiagnostics = vi.fn();
  const mockGetDiagnostics = vi.fn();

  const mockApiService = {
    getInstance: (...a) => mockGetInstance(...a),
    getSemanticDiagnostics: (...a) => mockGetSemanticDiagnostics(...a),
    getDiagnostics: (...a) => mockGetDiagnostics(...a),
  };

  const mockLSPClientService = {
    getLSPState: (...a) => mockGetLSPState(...a),
  };

  let _debouncedInstance = null;
  const createDebouncedDiagnosticsUpdater = vi.fn(() => {
    _debouncedInstance = {
      update: (...a) => mockDebouncedUpdate(...a),
      cancel: vi.fn(),
    };
    return _debouncedInstance;
  });

  return {
    mockResolveLanguageId,
    mockClearDiagnostics,
    mockDebouncedUpdate,
    mockGetClientForLanguageSync,
    mockGetLSPClientService,
    mockGetLSPState,
    mockGetInstance,
    mockGetSemanticDiagnostics,
    mockGetDiagnostics,
    mockApiService,
    mockLSPClientService,
    createDebouncedDiagnosticsUpdater,
    getDebouncedInstance: () => _debouncedInstance,
  };
});

vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

vi.mock('../extensions/languageRegistry', () => ({
  resolveLanguageId: (...a) => mocks.mockResolveLanguageId(...a),
}));

vi.mock('../extensions/lintDiagnostics', () => ({
  clearDiagnostics: (...a) => mocks.mockClearDiagnostics(...a),
  lintDiagnostics: () => [],
  createDebouncedDiagnosticsUpdater: mocks.createDebouncedDiagnosticsUpdater,
}));

vi.mock('../extensions/lspExtensions', () => ({
  getClientForLanguageSync: (...a) => mocks.mockGetClientForLanguageSync(...a),
  getLSPClientService: (...a) => mocks.mockGetLSPClientService(...a),
}));

vi.mock('../services/api', () => ({
  ApiService: mocks.mockApiService,
}));

// Destructure mock references for convenient use in test code
const {
  mockResolveLanguageId,
  mockClearDiagnostics,
  mockDebouncedUpdate,
  mockGetClientForLanguageSync,
  mockGetLSPClientService,
  mockGetLSPState,
  mockGetInstance,
  mockGetSemanticDiagnostics,
  mockGetDiagnostics,
  mockApiService,
  mockLSPClientService,
} = mocks;

// Static imports — Vitest hoists vi.mock above all imports automatically
import { useEditorDiagnostics, FETCH_DEBOUNCE_MS } from './useEditorDiagnostics';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container;
let root;

function createMockView() {
  return {
    state: {
      doc: {
        toString: () => 'console.log("hello");',
      },
    },
  };
}

/**
 * Advance fake timers past the fetch debounce window so any pending (debounced)
 * 'edit' fetch fires. Use after calling fetchDiagnostics to let the request run.
 */
async function flushFetches() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(FETCH_DEBOUNCE_MS + 50);
  });
}

beforeEach(() => {
  vi.useFakeTimers();

  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);

  // Reset non-hoisted mock defaults
  mockResolveLanguageId.mockReset();
  mockResolveLanguageId.mockImplementation((override, ext) => {
    if (override) return { languageId: override };
    const extMap = {
      ts: 'typescript',
      go: 'go',
      js: 'javascript',
      jsx: 'javascript-jsx',
      tsx: 'typescript-jsx',
      py: 'python',
    };
    return { languageId: extMap[ext] || 'plaintext' };
  });

  mockGetClientForLanguageSync.mockReset();
  mockGetClientForLanguageSync.mockReturnValue(null);

  mockGetLSPClientService.mockReset();
  mockGetLSPClientService.mockReturnValue(mockLSPClientService);
  mockGetLSPState.mockReset();
  mockGetLSPState.mockReturnValue('disconnected');

  mockClearDiagnostics.mockReset();
  mockDebouncedUpdate.mockReset();

  // Reset hoisted mocks (these must NOT be cleared, only reset)
  mockGetInstance.mockReset();
  mockGetInstance.mockReturnValue(mockApiService);
  mockGetSemanticDiagnostics.mockReset();
  mockGetSemanticDiagnostics.mockResolvedValue({
    capabilities: { diagnostics: true },
    diagnostics: [],
    duration_ms: 10,
  });
  mockGetDiagnostics.mockReset();
  mockGetDiagnostics.mockResolvedValue({ diagnostics: [] });
});

afterEach(() => {
  // Unmount first so the hook's cleanup clears any pending debounce timer.
  act(() => {
    root?.unmount();
  });
  vi.useRealTimers();
  container?.remove();
});

/**
 * Render the hook inside a minimal wrapper component so React effects fire.
 */
function renderTestHook(options = {}) {
  const { buffer = undefined, viewRef = { current: createMockView() } } = options;

  let hookReturn = null;

  function HookWrapper() {
    hookReturn = useEditorDiagnostics(viewRef, buffer);
    return null;
  }

  act(() => {
    flushSync(() => {
      root.render(createElement(HookWrapper));
    });
  });

  return {
    getReturn: () => hookReturn,
    viewRef,
    buffer,
  };
}

/**
 * Like renderTestHook, but supports re-rendering with a different buffer —
 * needed to simulate the user switching editor files while a debounced fetch
 * is pending.
 */
function renderTestHookWithBufferSwitch(options = {}) {
  const state = {
    buffer: options.buffer,
    viewRef: options.viewRef ?? { current: createMockView() },
  };

  let hookReturn = null;

  function HookWrapper() {
    hookReturn = useEditorDiagnostics(state.viewRef, state.buffer);
    return null;
  }

  const render = () => {
    act(() => {
      flushSync(() => {
        root.render(createElement(HookWrapper));
      });
    });
  };

  render();

  return {
    getReturn: () => hookReturn,
    viewRef: state.viewRef,
    setBuffer(nextBuffer) {
      state.buffer = nextBuffer;
      render();
    },
  };
}

// ---------------------------------------------------------------------------
// Tests: isSemanticLanguage helper
// ---------------------------------------------------------------------------

describe('isSemanticLanguage helper', () => {
  it('returns true for typescript', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('typescript')).toBe(true);
  });

  it('returns true for typescript-jsx', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('typescript-jsx')).toBe(true);
  });

  it('returns true for javascript', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('javascript')).toBe(true);
  });

  it('returns true for javascript-jsx', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('javascript-jsx')).toBe(true);
  });

  it('returns true for go', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('go')).toBe(true);
  });

  it('returns false for python', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('python')).toBe(false);
  });

  it('returns false for plaintext', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('plaintext')).toBe(false);
  });

  it('returns false for empty string', () => {
    const { getReturn } = renderTestHook();
    expect(getReturn().isSemanticLanguage('')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: early returns
// ---------------------------------------------------------------------------

describe('early returns', () => {
  it('does nothing when viewRef.current is null', async () => {
    const { getReturn } = renderTestHook({
      viewRef: { current: null },
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });

    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: LSP client active — skip semantic diagnostics
// ---------------------------------------------------------------------------

describe('LSP client active', () => {
  it('skips semantic diagnostics when LSP client is connected for semantic language', async () => {
    mockGetClientForLanguageSync.mockReturnValue({ isConnected: true });

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
    expect(mockDebouncedUpdate).not.toHaveBeenCalled();
    expect(mockClearDiagnostics).not.toHaveBeenCalled();
  });

  it('does NOT skip when LSP client is connected for non-semantic language', async () => {
    mockGetClientForLanguageSync.mockReturnValue({ isConnected: true });

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.py', name: 'test.py' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.py', 'x = 1');
    });
    await flushFetches();

    // For non-semantic languages, should fall through to basic diagnostics
    expect(mockGetDiagnostics).toHaveBeenCalled();
  });

  it('skips semantic diagnostics while LSP client is connecting', async () => {
    // File open races the LSP bootstrap: loadFile fires fetchDiagnostics
    // before the LSP client exists (getClientForLanguageSync → null) but
    // while getClientForLanguage is mid-flight. The LSP will push
    // diagnostics via serverDiagnostics() once installed, so the semantic
    // HTTP fallback must not fire (it would duplicate work and can paint
    // stale results over the LSP's fresher push).
    mockGetClientForLanguageSync.mockReturnValue(null);
    mockGetLSPState.mockReturnValue('connecting');

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.tsx', name: 'test.tsx' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.tsx', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
  });

  it('skips semantic diagnostics while LSP client is reconnecting', async () => {
    mockGetClientForLanguageSync.mockReturnValue(null);
    mockGetLSPState.mockReturnValue('reconnecting');

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
  });

  it('still uses semantic diagnostics when LSP is fully disconnected', async () => {
    // LSP unavailable entirely (no binary, status said not-supported):
    // semantic fallback must keep working.
    mockGetClientForLanguageSync.mockReturnValue(null);
    mockGetLSPState.mockReturnValue('disconnected');

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: semantic diagnostics success path
// ---------------------------------------------------------------------------

describe('semantic diagnostics success', () => {
  it('fetches and applies semantic diagnostics when available', async () => {
    mockGetSemanticDiagnostics.mockResolvedValue({
      capabilities: { diagnostics: true },
      diagnostics: [{ severity: 'error', message: 'Type error', from: 0, to: 10 }],
      duration_ms: 15,
    });

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const x = 1;', 'typescript', 'edit');
    expect(mockDebouncedUpdate).toHaveBeenCalledWith(viewRef.current, expect.any(Array));
  });

  it('clears diagnostics when semantic returns empty array', async () => {
    mockGetSemanticDiagnostics.mockResolvedValue({
      capabilities: { diagnostics: true },
      diagnostics: [],
      duration_ms: 5,
    });

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockClearDiagnostics).toHaveBeenCalledWith(viewRef.current);
    expect(mockDebouncedUpdate).not.toHaveBeenCalled();
  });

  it('falls back to basic diagnostics when capabilities.diagnostics is false', async () => {
    mockGetSemanticDiagnostics.mockResolvedValue({
      capabilities: { diagnostics: false },
    });

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetDiagnostics).toHaveBeenCalled();
  });

  it('falls back to basic diagnostics when capabilities is undefined', async () => {
    mockGetSemanticDiagnostics.mockResolvedValue({});

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetDiagnostics).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: semantic diagnostics error → fallback
// ---------------------------------------------------------------------------

describe('semantic diagnostics error -> fallback', () => {
  it('falls back to basic diagnostics when semantic throws', async () => {
    mockGetSemanticDiagnostics.mockRejectedValue(new Error('Semantic server unavailable'));
    mockGetDiagnostics.mockResolvedValue({
      diagnostics: [{ severity: 'warning', message: 'Basic lint warning', from: 0, to: 5 }],
    });

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).toHaveBeenCalled();
    expect(mockGetDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const x = 1;');
    expect(mockDebouncedUpdate).toHaveBeenCalledWith(viewRef.current, expect.any(Array));
  });
});

// ---------------------------------------------------------------------------
// Tests: basic diagnostics path
// ---------------------------------------------------------------------------

describe('basic diagnostics', () => {
  it('applies basic diagnostics when non-semantic language', async () => {
    mockGetDiagnostics.mockResolvedValue({
      diagnostics: [{ severity: 'warning', message: 'Lint warning', from: 0, to: 10 }],
    });

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.py', name: 'test.py' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.py', 'x = 1');
    });
    await flushFetches();

    expect(mockGetDiagnostics).toHaveBeenCalledWith('/test/file.py', 'x = 1');
    expect(mockDebouncedUpdate).toHaveBeenCalled();
  });

  it('clears diagnostics when basic returns empty', async () => {
    mockGetDiagnostics.mockResolvedValue({ diagnostics: [] });

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.py', name: 'test.py' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.py', 'x = 1');
    });
    await flushFetches();

    expect(mockClearDiagnostics).toHaveBeenCalledWith(viewRef.current);
  });

  it('clears diagnostics when basic fetch throws', async () => {
    mockGetDiagnostics.mockRejectedValue(new Error('Network error'));

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.py', name: 'test.py' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.py', 'x = 1');
    });
    await flushFetches();

    expect(mockClearDiagnostics).toHaveBeenCalledWith(viewRef.current);
  });

  it('clears diagnostics when basic returns no diagnostics property', async () => {
    mockGetDiagnostics.mockResolvedValue({});

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.py', name: 'test.py' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.py', 'x = 1');
    });
    await flushFetches();

    expect(mockClearDiagnostics).toHaveBeenCalledWith(viewRef.current);
  });
});

// ---------------------------------------------------------------------------
// Tests: trigger parameter
// ---------------------------------------------------------------------------

describe('trigger parameter', () => {
  it('passes "edit" as default trigger for semantic diagnostics', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const x = 1;', 'typescript', 'edit');
  });

  it('passes "save" trigger when specified', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;', 'save');
    });

    // Save bypasses the debounce — the request fires immediately.
    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const x = 1;', 'typescript', 'save');
  });
});

// ---------------------------------------------------------------------------
// Tests: fetch debounce / coalescing
// ---------------------------------------------------------------------------

describe('fetch debounce / coalescing', () => {
  it('coalesces rapid edit fetches into a single request with the latest content', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    // Three rapid edits inside one tick — only the last should reach the API.
    act(() => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const a = 1;');
      getReturn().fetchDiagnostics('/test/file.ts', 'const ab = 1;');
      getReturn().fetchDiagnostics('/test/file.ts', 'const abc = 1;');
    });
    await flushFetches();

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledTimes(1);
    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const abc = 1;', 'typescript', 'edit');
    // The earlier contents must never have reached the API.
    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalledWith('/test/file.ts', 'const a = 1;', 'typescript', 'edit');
    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalledWith('/test/file.ts', 'const ab = 1;', 'typescript', 'edit');
  });

  it('save bypasses a pending debounced edit fetch', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    act(() => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const a = 1;', 'edit'); // schedules
      getReturn().fetchDiagnostics('/test/file.ts', 'const b = 1;', 'save'); // cancels + fires now
    });

    // Save fires immediately — no timer advancement needed.
    expect(mockGetSemanticDiagnostics).toHaveBeenCalledTimes(1);
    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith('/test/file.ts', 'const b = 1;', 'typescript', 'save');

    // Advancing timers must NOT fire the cancelled edit fetch.
    await flushFetches();
    expect(mockGetSemanticDiagnostics).toHaveBeenCalledTimes(1);
  });

  it('unmount cancels a pending debounced fetch', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    act(() => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const a = 1;'); // schedules
    });

    act(() => {
      root.unmount();
    });

    await vi.advanceTimersByTimeAsync(FETCH_DEBOUNCE_MS + 50);

    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
  });

  it('does not apply a pending fetch for a file the user switched away from', async () => {
    mockGetSemanticDiagnostics.mockResolvedValue({
      capabilities: { diagnostics: true },
      diagnostics: [{ severity: 'error', message: 'stale', from: 0, to: 5 }],
    });

    const { getReturn, setBuffer } = renderTestHookWithBufferSwitch({
      buffer: { file: { path: '/test/a.ts', ext: '.ts', name: 'a.ts' } },
    });

    // Edit a.ts — schedules a debounced fetch.
    act(() => {
      getReturn().fetchDiagnostics('/test/a.ts', 'const a = 1;');
    });

    // Switch to another file before the debounce window elapses.
    act(() => {
      setBuffer({ file: { path: '/test/b.py', ext: '.py', name: 'b.py' } });
    });
    await flushFetches();

    // The stale fetch for a.ts must never reach the backend.
    expect(mockGetSemanticDiagnostics).not.toHaveBeenCalled();
    expect(mockGetDiagnostics).not.toHaveBeenCalled();
    expect(mockDebouncedUpdate).not.toHaveBeenCalled();
    expect(mockClearDiagnostics).not.toHaveBeenCalled();
  });

  it('a slow older response does not overwrite a newer save-triggered result', async () => {
    let resolveEditFetch;
    mockGetSemanticDiagnostics.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveEditFetch = resolve;
        }),
    );
    mockGetSemanticDiagnostics.mockResolvedValueOnce({
      capabilities: { diagnostics: true },
      diagnostics: [{ severity: 'warning', message: 'from save', from: 0, to: 5 }],
    });

    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    // Edit triggers a debounced fetch that stays in flight (held promise).
    act(() => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const a = 1;');
    });
    await flushFetches();

    // Save bypasses the debounce and resolves immediately with fresh content.
    act(() => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const b = 1;', 'save');
    });
    await act(async () => {});

    // Now the older edit response lands — it must NOT overwrite the save result.
    await act(async () => {
      resolveEditFetch({
        capabilities: { diagnostics: true },
        diagnostics: [{ severity: 'error', message: 'stale edit', from: 0, to: 5 }],
      });
    });

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledTimes(2);
    expect(mockDebouncedUpdate).toHaveBeenCalledTimes(1);
    expect(mockDebouncedUpdate).toHaveBeenCalledWith(expect.anything(), [
      expect.objectContaining({ message: 'from save' }),
    ]);
  });
});

// ---------------------------------------------------------------------------
// Tests: unmount guard during async
// ---------------------------------------------------------------------------

describe('unmount guard during async', () => {
  it('does not apply diagnostics if viewRef becomes null during semantic fetch', async () => {
    let resolveSemantic;
    mockGetSemanticDiagnostics.mockReturnValue(
      new Promise((resolve) => {
        resolveSemantic = resolve;
      }),
    );

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });
    // Start the (debounced) fetch; the semantic promise is held until below.
    await flushFetches();

    viewRef.current = null;

    await act(async () => {
      resolveSemantic({
        capabilities: { diagnostics: true },
        diagnostics: [{ severity: 'error', message: 'err' }],
      });
    });

    expect(mockDebouncedUpdate).not.toHaveBeenCalled();
    expect(mockClearDiagnostics).not.toHaveBeenCalled();
  });
});
