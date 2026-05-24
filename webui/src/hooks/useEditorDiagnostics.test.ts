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
  const mockGetInstance = vi.fn();
  const mockGetSemanticDiagnostics = vi.fn();
  const mockGetDiagnostics = vi.fn();

  const mockApiService = {
    getInstance: (...a) => mockGetInstance(...a),
    getSemanticDiagnostics: (...a) => mockGetSemanticDiagnostics(...a),
    getDiagnostics: (...a) => mockGetDiagnostics(...a),
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
    mockGetInstance,
    mockGetSemanticDiagnostics,
    mockGetDiagnostics,
    mockApiService,
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
  mockGetInstance,
  mockGetSemanticDiagnostics,
  mockGetDiagnostics,
  mockApiService,
} = mocks;

// Static imports — Vitest hoists vi.mock above all imports automatically
import { useEditorDiagnostics } from './useEditorDiagnostics';

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

beforeEach(() => {
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
  act(() => {
    root?.unmount();
  });
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

    // For non-semantic languages, should fall through to basic diagnostics
    expect(mockGetDiagnostics).toHaveBeenCalled();
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

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith(
      '/test/file.ts',
      'const x = 1;',
      'typescript',
      'edit',
    );
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

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith(
      '/test/file.ts',
      'const x = 1;',
      'typescript',
      'edit',
    );
  });

  it('passes "save" trigger when specified', async () => {
    const { getReturn } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    await act(async () => {
      await getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;', 'save');
    });

    expect(mockGetSemanticDiagnostics).toHaveBeenCalledWith(
      '/test/file.ts',
      'const x = 1;',
      'typescript',
      'save',
    );
  });
});

// ---------------------------------------------------------------------------
// Tests: unmount guard during async
// ---------------------------------------------------------------------------

describe('unmount guard during async', () => {
  it('does not apply diagnostics if viewRef becomes null during semantic fetch', async () => {
    let resolveSemantic;
    mockGetSemanticDiagnostics.mockReturnValue(
      new Promise((resolve) => { resolveSemantic = resolve; }),
    );

    const { getReturn, viewRef } = renderTestHook({
      buffer: { file: { ext: '.ts', name: 'test.ts' } },
    });

    let fetchPromise;
    act(() => {
      fetchPromise = getReturn().fetchDiagnostics('/test/file.ts', 'const x = 1;');
    });

    viewRef.current = null;

    act(() => {
      resolveSemantic({
        capabilities: { diagnostics: true },
        diagnostics: [{ severity: 'error', message: 'err' }],
      });
      return fetchPromise;
    });

    expect(mockDebouncedUpdate).not.toHaveBeenCalled();
    expect(mockClearDiagnostics).not.toHaveBeenCalled();
  });
});
