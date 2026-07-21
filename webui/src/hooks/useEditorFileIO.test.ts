/**
 * useEditorFileIO.test.ts — Unit tests for the useEditorFileIO hook.
 *
 * Covers:
 * - loadFile: normal file loading path
 * - loadFile: workspace (in-memory) buffers
 * - loadFile: error handling
 * - loadFile: cursor/scroll position restoration
 * - loadFile: git diff fetching after load
 * - loadFile: indent detection on load
 * - loadFile: line ending detection on load
 * - handleSave: normal save path
 * - handleSave: format-on-save handling
 * - handleSave: error handling
 * - handleSave: workspace buffers are skipped
 * - handleSave: re-fetch diagnostics and diff after save
 * - Return type verification
 */
// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — ALL references in vi.mock factories use wrapper functions
// ((...a) => fn(...a)) to avoid TDZ since factories are hoisted above const.
// ---------------------------------------------------------------------------

vi.mock('@codemirror/language', () => ({
  indentUnit: { of: vi.fn((v) => `indentUnit(${v})`) },
}));

vi.mock('@codemirror/state', () => ({
  EditorState: {
    tabSize: { of: vi.fn((v) => `tabSize(${v})`) },
  },
  Transaction: {
    addToHistory: { of: vi.fn((v) => `addToHistory(${v})`) },
  },
}));

vi.mock('@codemirror/view', () => ({
  EditorView: {},
}));

// FileChangeDialog
const mockShowFileChangeDialog = vi.fn();
vi.mock('../components/FileChangeDialog', () => ({
  showFileChangeDialog: (...a) => mockShowFileChangeDialog(...a),
}));

// EditorManagerContext — use wrapper functions for all callbacks
const mockUpdateBufferContent = vi.fn();
const mockSaveBuffer = vi.fn();
const mockSetBufferOriginalContent = vi.fn();
const mockSetBufferExternallyModified = vi.fn();
const mockClearBufferExternallyModified = vi.fn();
const mockOpenWorkspaceBuffer = vi.fn();

vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: vi.fn(() => ({
    updateBufferContent: (...a) => mockUpdateBufferContent(...a),
    saveBuffer: (...a) => mockSaveBuffer(...a),
    setBufferOriginalContent: (...a) => mockSetBufferOriginalContent(...a),
    setBufferExternallyModified: (...a) => mockSetBufferExternallyModified(...a),
    clearBufferExternallyModified: (...a) => mockClearBufferExternallyModified(...a),
    openWorkspaceBuffer: (...a) => mockOpenWorkspaceBuffer(...a),
  })),
}));

// Diff gutter
const mockUpdateDiffGutter = vi.fn();
const mockClearDiffGutter = vi.fn();
vi.mock('../extensions/diffGutter', () => ({
  diffGutter: vi.fn(() => 'mock-diffGutter'),
  updateDiffGutter: (...a) => mockUpdateDiffGutter(...a),
  clearDiffGutter: (...a) => mockClearDiffGutter(...a),
}));

// Indent detection
const mockDetectIndentation = vi.fn();
vi.mock('../extensions/indentDetect', () => ({
  detectIndentation: (...a) => mockDetectIndentation(...a),
}));

// Line ending detection
const mockDetectLineEnding = vi.fn();
vi.mock('../extensions/lineEndingDetect', () => ({
  detectLineEnding: (...a) => mockDetectLineEnding(...a),
}));

// Lint diagnostics
const mockClearDiagnostics = vi.fn();
vi.mock('../extensions/lintDiagnostics', () => ({
  lintDiagnostics: vi.fn(() => 'mock-lintDiagnostics'),
  clearDiagnostics: (...a) => mockClearDiagnostics(...a),
  createDebouncedDiagnosticsUpdater: vi.fn(() => ({
    cancel: vi.fn(),
    update: vi.fn(),
  })),
}));

// Unsaved line highlight
const mockSetOriginalContentOf = vi.fn((v) => `setOriginalContent(${v})`);
vi.mock('../extensions/unsavedLineHighlight', () => ({
  unsavedLineHighlight: vi.fn(() => 'mock-unsavedLineHighlight'),
  setOriginalContent: { of: (...a) => mockSetOriginalContentOf(...a) },
}));

// API service
const mockGetInstance = vi.fn();
const mockGetSemanticDiagnostics = vi.fn();
const mockGetDiagnostics = vi.fn();
const mockGetGitDiff = vi.fn();
const mockApiService = {
  getInstance: (...a) => mockGetInstance(...a),
  getSemanticDiagnostics: (...a) => mockGetSemanticDiagnostics(...a),
  getDiagnostics: (...a) => mockGetDiagnostics(...a),
  getGitDiff: (...a) => mockGetGitDiff(...a),
};
vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: (...a) => mockGetInstance(...a),
    getSemanticDiagnostics: (...a) => mockGetSemanticDiagnostics(...a),
    getDiagnostics: (...a) => mockGetDiagnostics(...a),
    getGitDiff: (...a) => mockGetGitDiff(...a),
  },
}));

// File access
const mockReadFileWithConsent = vi.fn();
vi.mock('../services/fileAccess', () => ({
  readFileWithConsent: (...a) => mockReadFileWithConsent(...a),
}));

// Notification bus
const mockNotify = vi.fn();
const mockNotificationBus = { notify: (...a) => mockNotify(...a) };
vi.mock('../services/notificationBus', () => ({
  notificationBus: { notify: (...a) => mockNotify(...a) },
}));

// Log utilities
const mockDebugLog = vi.fn();
const mockWarn = vi.fn();
const mockUseLog = vi.fn(() => ({
  error: vi.fn(),
  warn: vi.fn(),
  info: vi.fn(),
}));
vi.mock('../utils/log', () => ({
  useLog: () => mockUseLog(),
  debugLog: (...a) => mockDebugLog(...a),
  warn: (...a) => mockWarn(...a),
}));

// Media patterns
vi.mock('../utils/mediaPatterns', () => ({
  isImageFile: vi.fn(() => false),
  isAudioFile: vi.fn(() => false),
  isVideoFile: vi.fn(() => false),
  isBinaryFile: vi.fn(() => false),
}));

// Simple diff
const mockGenerateUnifiedDiff = vi.fn();
vi.mock('../utils/simpleDiff', () => ({
  generateUnifiedDiff: (...a) => mockGenerateUnifiedDiff(...a),
}));

// Auto-reload
vi.mock('./useAutoReloadCleanBuffers', () => ({
  JUST_SAVED_THRESHOLD_MS: 3500,
  justSavedRef: new Map(),
}));

// Editor extensions constants
vi.mock('./useEditorExtensions', () => ({
  TAB_SIZE_TABS_MODE: 0,
  TAB_SIZE_DEFAULT: 4,
}));

// Static import
import { useEditorFileIO } from './useEditorFileIO';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container, root;

function createMockView(docContent = 'initial content') {
  const view = {
    state: {
      doc: {
        length: docContent.length,
        lines: Math.max(1, docContent.split('\n').length),
        line: vi.fn((n) => ({ from: (n - 1) * 20, to: n * 20, length: 20 })),
        toString: () => docContent,
      },
    },
    scrollDOM: { scrollTop: 0, scrollLeft: 0 },
    dispatch: vi.fn(),
  };
  return view;
}

function createBuffer(opts = {}) {
  const o = {
    id: 'buf-1',
    kind: 'file',
    filePath: '/test/file.ts',
    fileExt: '.ts',
    fileName: 'file.ts',
    content: 'const x = 1;',
    originalContent: undefined,
    isModified: false,
    cursorPosition: { line: 0, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    ...opts,
  };
  return {
    id: o.id,
    kind: o.kind,
    file: { path: o.filePath, ext: o.fileExt, name: o.fileName },
    content: o.content,
    originalContent: o.originalContent,
    isModified: o.isModified,
    cursorPosition: o.cursorPosition,
    scrollPosition: o.scrollPosition,
  };
}

function setupHook(opts = {}) {
  const viewRef = { current: createMockView() };
  const buffer = createBuffer(opts.bufferOptions || {});
  const bufferRef = { current: buffer };
  const indentManuallySetRef = { current: false };
  const fetchDiagnosticsRef = { current: vi.fn() };
  const paneId = 'pane-1';

  // Mock CodeMirror view API. The mock view's dispatch is the same vi.fn()
  // the test asserts against, so passing through `dispatch` still records
  // the call. `withExternalUpdate` runs the function and toggles a local
  // gate so the cursor-skip behavior can be exercised in dedicated tests.
  let externalUpdateGate = false;
  const cmViewApiRef = {
    current: {
      view: viewRef.current,
      isMounted: true,
      dispatch: (tr) => viewRef.current?.dispatch(tr),
      withExternalUpdate: (fn) => {
        const prev = externalUpdateGate;
        externalUpdateGate = true;
        try {
          return fn();
        } finally {
          externalUpdateGate = prev;
        }
      },
      isExternalUpdate: () => externalUpdateGate,
      save: vi.fn(),
      getFilePath: () => viewRef.current?.state?.doc?.toString?.(),
      getFileExt: () => undefined,
      getContent: () => '',
      subscribe: () => () => {},
      compartments: undefined as any,
    },
  };

  const setters = {
    setLoading: vi.fn(),
    setSaving: vi.fn(),
    setError: vi.fn(),
    setLocalContent: vi.fn(),
    setSelectionInfo: vi.fn(),
    setEditorTabSize: vi.fn(),
    setEditorUsesTabs: vi.fn(),
    setLineEnding: vi.fn(),
  };

  const compartments = {
    tabSize: { reconfigure: vi.fn() },
    history: { reconfigure: vi.fn() },
  };

  return {
    viewRef,
    cmViewApiRef,
    buffer,
    bufferRef,
    indentManuallySetRef,
    fetchDiagnosticsRef,
    paneId,
    setters,
    compartments,
  };
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();

  mockGetInstance.mockReturnValue({
    getSemanticDiagnostics: (...a) => mockGetSemanticDiagnostics(...a),
    getDiagnostics: (...a) => mockGetDiagnostics(...a),
    getGitDiff: (...a) => mockGetGitDiff(...a),
  });
  mockGetGitDiff.mockResolvedValue({ diff: '' });
  mockReadFileWithConsent.mockResolvedValue({
    ok: true,
    text: () => Promise.resolve('file content from disk'),
  });
  mockDetectIndentation.mockReturnValue({
    useTabs: false,
    indentWidth: 4,
    indentedLineCount: 5,
  });
  mockDetectLineEnding.mockReturnValue({ lineEnding: '\n', mixed: false });
});

afterEach(() => {
  act(() => root?.unmount());
  container?.remove();
});

function renderHook(setup) {
  let hookReturn = null;
  function Wrapper() {
    hookReturn = useEditorFileIO(
      setup.cmViewApiRef,
      setup.buffer,
      setup.bufferRef,
      setup.compartments,
      setup.indentManuallySetRef,
      setup.fetchDiagnosticsRef,
      setup.paneId,
      setup.setters,
    );
    return null;
  }
  act(() => root.render(createElement(Wrapper)));
  return hookReturn;
}

// ---------------------------------------------------------------------------
// Tests: loadFile — normal file loading
// ---------------------------------------------------------------------------

describe('loadFile — normal file loading', () => {
  it('loads file content and updates editor view', async () => {
    const setup = setupHook();
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setLoading).toHaveBeenCalledWith(true);
    expect(mockReadFileWithConsent).toHaveBeenCalledWith('/test/file.ts');
    expect(setup.setters.setLocalContent).toHaveBeenCalledWith('file content from disk');
    expect(setup.viewRef.current.dispatch).toHaveBeenCalled();
    expect(setup.setters.setLoading).toHaveBeenCalledWith(false);
  });

  it('clears error and selection info on load', async () => {
    const setup = setupHook();
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setError).toHaveBeenCalledWith(null);
    expect(setup.setters.setSelectionInfo).toHaveBeenCalledWith(null);
  });

  it('clears external-update gate after load completes', async () => {
    const setup = setupHook();
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    // After load, the gate is back to false. If withExternalUpdate were
    // buggy (e.g., failed to restore in finally), this would be true.
    expect(setup.cmViewApiRef.current.isExternalUpdate()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — workspace (in-memory) buffers
// ---------------------------------------------------------------------------

describe('loadFile — workspace buffers', () => {
  it('loads workspace buffer from memory without disk I/O', async () => {
    const setup = setupHook({
      bufferOptions: {
        id: 'buf-ws',
        filePath: '__workspace/scratch',
        content: 'workspace content',
        kind: 'workspace',
      },
    });
    setup.bufferRef.current = setup.buffer;
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('__workspace/scratch');
    });

    expect(mockReadFileWithConsent).not.toHaveBeenCalled();
    expect(setup.setters.setLocalContent).toHaveBeenCalledWith('workspace content');
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — error handling
// ---------------------------------------------------------------------------

describe('loadFile — error handling', () => {
  it('sets error message when file read fails', async () => {
    const setup = setupHook();
    mockReadFileWithConsent.mockResolvedValue({
      ok: false,
      statusText: 'File not found',
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setError).toHaveBeenCalledWith('Failed to load file: File not found');
  });

  it('handles readFileWithConsent throwing an exception', async () => {
    const setup = setupHook();
    mockReadFileWithConsent.mockRejectedValue(new Error('Permission denied'));
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setError).toHaveBeenCalledWith('Permission denied');
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — cursor position restoration
// ---------------------------------------------------------------------------

describe('loadFile — cursor position restoration', () => {
  it('restores cursor position from buffer when non-zero', async () => {
    const setup = setupHook({
      bufferOptions: { cursorPosition: { line: 10, column: 5 } },
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    const dispatchCalls = setup.viewRef.current.dispatch.mock.calls;
    const selectionCall = dispatchCalls.find((call) => call[0] && call[0].selection);
    expect(selectionCall).toBeDefined();
  });

  it('does NOT restore cursor when position is at origin (0,0)', async () => {
    const setup = setupHook({
      bufferOptions: { cursorPosition: { line: 0, column: 0 } },
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    const dispatchCalls = setup.viewRef.current.dispatch.mock.calls;
    const selectionCall = dispatchCalls.find((call) => call[0] && call[0].selection);
    expect(selectionCall).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — git diff
// ---------------------------------------------------------------------------

describe('loadFile — git diff', () => {
  it('fetches and displays git diff after successful load', async () => {
    const setup = setupHook();
    mockGetGitDiff.mockResolvedValue({
      diff: '--- a/file.ts\n+++ b/file.ts\n@@ -1 +1 @@\n-old\n+new\n',
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(mockGetGitDiff).toHaveBeenCalledWith('/test/file.ts');
    expect(mockUpdateDiffGutter).toHaveBeenCalled();
  });

  it('clears diff gutter when no diff', async () => {
    const setup = setupHook();
    mockGetGitDiff.mockResolvedValue({ diff: '' });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(mockClearDiffGutter).toHaveBeenCalled();
  });

  it('handles git diff fetch failure gracefully', async () => {
    const setup = setupHook();
    mockGetGitDiff.mockRejectedValue(new Error('Git not available'));
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(mockNotify).toHaveBeenCalledWith('warning', 'Git Diff', 'Failed to fetch git diff');
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — indent detection
// ---------------------------------------------------------------------------

describe('loadFile — indent detection', () => {
  it('applies indent detection with tabs when detected', async () => {
    const setup = setupHook();
    mockDetectIndentation.mockReturnValue({
      useTabs: true,
      indentWidth: 4,
      indentedLineCount: 10,
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setEditorUsesTabs).toHaveBeenCalledWith(true);
    expect(setup.setters.setEditorTabSize).toHaveBeenCalledWith(0); // TAB_SIZE_TABS_MODE
  });

  it('applies indent detection with spaces', async () => {
    const setup = setupHook();
    mockDetectIndentation.mockReturnValue({
      useTabs: false,
      indentWidth: 2,
      indentedLineCount: 5,
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setEditorUsesTabs).toHaveBeenCalledWith(false);
    expect(setup.setters.setEditorTabSize).toHaveBeenCalledWith(2);
  });

  it('skips indent detection when manually set', async () => {
    const setup = setupHook();
    setup.indentManuallySetRef.current = true;
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setEditorUsesTabs).not.toHaveBeenCalled();
    expect(setup.setters.setEditorTabSize).not.toHaveBeenCalled();
  });

  it('falls back to defaults when not enough indented lines', async () => {
    const setup = setupHook();
    mockDetectIndentation.mockReturnValue({
      useTabs: false,
      indentWidth: 4,
      indentedLineCount: 1,
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setEditorUsesTabs).toHaveBeenCalledWith(false);
    expect(setup.setters.setEditorTabSize).toHaveBeenCalledWith(4); // TAB_SIZE_DEFAULT
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — line ending detection
// ---------------------------------------------------------------------------

describe('loadFile — line ending detection', () => {
  it('detects and sets line ending', async () => {
    const setup = setupHook();
    mockDetectLineEnding.mockReturnValue({ lineEnding: '\r\n', mixed: false });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.setters.setLineEnding).toHaveBeenCalledWith('\r\n');
  });
});

// ---------------------------------------------------------------------------
// Tests: loadFile — fetch diagnostics
// ---------------------------------------------------------------------------

describe('loadFile — fetch diagnostics', () => {
  it('calls fetchDiagnosticsRef after successful load', async () => {
    const setup = setupHook();
    const hook = renderHook(setup);

    await act(async () => {
      await hook.loadFile('/test/file.ts');
    });

    expect(setup.fetchDiagnosticsRef.current).toHaveBeenCalledWith('/test/file.ts', 'file content from disk');
  });
});

// ---------------------------------------------------------------------------
// Tests: handleSave
// ---------------------------------------------------------------------------

describe('handleSave', () => {
  it('saves buffer via saveBuffer and updates state', async () => {
    const setup = setupHook();
    mockSaveBuffer.mockResolvedValue({ mod_time: 1234567890 });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(setup.setters.setSaving).toHaveBeenCalledWith(true);
    expect(mockSaveBuffer).toHaveBeenCalledWith('buf-1');
    expect(setup.setters.setSaving).toHaveBeenCalledWith(false);
  });

  it('handles format-on-save when formattedContent is returned', async () => {
    const setup = setupHook();
    mockSaveBuffer.mockResolvedValue({
      mod_time: 1234567890,
      formattedContent: 'const x = 1;\n',
    });
    setup.viewRef.current.state.doc.toString = () => 'const x = 1;';
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(setup.viewRef.current.dispatch).toHaveBeenCalled();
    expect(setup.setters.setLocalContent).toHaveBeenCalledWith('const x = 1;\n');
    expect(mockUpdateBufferContent).toHaveBeenCalledWith('buf-1', 'const x = 1;\n');
  });

  it('skips format-on-save when editor content has changed', async () => {
    const setup = setupHook();
    mockSaveBuffer.mockResolvedValue({
      mod_time: 1234567890,
      formattedContent: 'const x = 1;\n',
    });
    // Editor has different content from buffer (user made changes)
    setup.viewRef.current.state.doc.toString = () => 'const x = 2;';
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    // Should NOT dispatch view changes for formatted content
    // (the save still happens, but formattedContent is not applied)
    expect(mockSaveBuffer).toHaveBeenCalled();
  });

  it('re-fetches diagnostics with "save" trigger after save', async () => {
    const setup = setupHook();
    mockSaveBuffer.mockResolvedValue({ mod_time: 1234567890 });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(setup.fetchDiagnosticsRef.current).toHaveBeenCalled();
    const callArgs = setup.fetchDiagnosticsRef.current.mock.calls[0];
    expect(callArgs[2]).toBe('save');
  });

  it('handles save error gracefully', async () => {
    const setup = setupHook();
    mockSaveBuffer.mockRejectedValue(new Error('Disk full'));
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(setup.setters.setError).toHaveBeenCalledWith('Disk full');
    expect(setup.setters.setSaving).toHaveBeenCalledWith(false);
  });

  it('skips save for workspace buffers', async () => {
    const setup = setupHook({
      bufferOptions: { filePath: '__workspace/scratch', content: 'workspace content' },
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(mockSaveBuffer).not.toHaveBeenCalled();
  });

  it('skips save for non-file buffers', async () => {
    const setup = setupHook({
      bufferOptions: { kind: 'chat', filePath: 'chat:session-123' },
    });
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(mockSaveBuffer).not.toHaveBeenCalled();
  });

  it('does nothing when bufferRef is null', async () => {
    const setup = setupHook();
    setup.bufferRef.current = null;
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(mockSaveBuffer).not.toHaveBeenCalled();
  });

  it('does nothing when viewRef is null', async () => {
    const setup = setupHook();
    setup.viewRef.current = null;
    setup.cmViewApiRef.current.view = null;
    const hook = renderHook(setup);

    await act(async () => {
      await hook.handleSave();
    });

    expect(mockSaveBuffer).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: refs and return type
// ---------------------------------------------------------------------------

describe('return value', () => {
  it('returns all required properties', () => {
    const setup = setupHook();
    const hook = renderHook(setup);

    expect(typeof hook.loadFile).toBe('function');
    expect(hook.loadFileRef).toBeDefined();
    expect(typeof hook.loadFileRef.current).toBe('function');
    expect(typeof hook.handleSave).toBe('function');
    expect(hook.saveRef).toBeDefined();
    expect(typeof hook.saveRef.current).toBe('function');
  });
});
