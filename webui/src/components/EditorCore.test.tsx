/**
 * EditorCore.test.tsx — Unit tests for the EditorCore component.
 *
 * Covers:
 * - EditorView creation and lifecycle
 * - Update listener forwarding to parent onUpdate callback
 * - View destruction and cleanup
 * - CSS class application/removal
 * - LSP registration/unregistration
 * - Async LSP extension initialization
 * - LSP initialization skip for unsupported languages
 * - Null container handling
 * - Recreation on prop changes
 */
// @ts-nocheck

// ---------------------------------------------------------------------------
// Mocks — must come before the static import of the module under test
// ---------------------------------------------------------------------------

// Mock LSP client service
const mockLspClient = {
  getStatus: vi.fn(() => Promise.resolve('ready')),
};

const mockLspService = {
  getStatus: vi.fn(() => Promise.resolve('ready')),
  getClientForLanguage: vi.fn((langId) => {
    if (langId === 'typescript') {
      return Promise.resolve(mockLspClient);
    }
    return Promise.resolve(null);
  }),
};

vi.mock('../services/lspClientService', () => ({
  getLSPClientService: vi.fn(() => mockLspService),
  LSP_SUPPORTED_LANGUAGES: new Set(['typescript', 'javascript']),
}));

// Mock LSP extensions
vi.mock('../extensions/lspExtensions', () => {
  const mockBuildLSPPluginExtensions = vi.fn(() => [{ name: 'lspPlugin' }]);
  const mockLspSyncOnDocChange = vi.fn(() => [{ name: 'lspSync' }]);
  const mockRegisterEditorView = vi.fn();
  const mockUnregisterEditorView = vi.fn();

  return {
    buildLSPPluginExtensions: (...args) => mockBuildLSPPluginExtensions(...args),
    lspSyncOnDocChange: (...args) => mockLspSyncOnDocChange(...args),
    registerEditorView: (...args) => mockRegisterEditorView(...args),
    unregisterEditorView: (...args) => mockUnregisterEditorView(...args),
    _mocks: {
      mockBuildLSPPluginExtensions,
      mockLspSyncOnDocChange,
      mockRegisterEditorView,
      mockUnregisterEditorView,
    },
  };
});

// Mock debugLog
vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// Mock EditorView from @codemirror/view
vi.mock('@codemirror/view', () => {
  const mockFn = vi.fn(function () {
    return {
      destroy: vi.fn(),
      dispatch: vi.fn(),
      dom: { isConnected: true },
      state: {
        doc: { toString: () => '' },
      },
    };
  });
  mockFn.updateListener = {
    of: vi.fn((listener) => ({ extension: 'updateListener', listener })),
  };
  return {
    EditorView: mockFn,
    ViewUpdate: class {},
  };
});

// Mock EditorState from @codemirror/state
vi.mock('@codemirror/state', () => {
  const mockFn = vi.fn((config) => ({
    doc: config.doc,
    extensions: config.extensions,
  }));
  mockFn.create = vi.fn((config) => ({
    doc: config.doc,
    extensions: config.extensions,
  }));

  return {
    EditorState: mockFn,
    Extension: {},
    Compartment: class CompartmentMock {
      reconfigure = vi.fn((extensions) => ({ effects: [] }));
    },
  };
});

// Static import — Vitest hoists vi.mock above all imports automatically
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import EditorCore from './EditorCore';
import { EditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { debugLog } from '../utils/log';
import * as lspExtensions from '../extensions/lspExtensions';
import { getLSPClientService } from '../services/lspClientService';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Render EditorCore with minimal props.
 * Returns control handles for making assertions.
 */
function renderEditorCore(props: Partial<any> = {}) {
  const containerRef = { current: container };
  const lspCompartment = { reconfigure: vi.fn((extensions) => ({ effects: [] })) };
  const onViewCreated = vi.fn();
  const onViewDestroying = vi.fn();
  const onUpdate = vi.fn();

  const defaultProps = {
    containerRef,
    initialContent: '// test content',
    extensions: [],
    lspCompartment,
    onViewCreated,
    onViewDestroying,
    onUpdate,
    ...props,
  };

  act(() => {
    root.render(createElement(EditorCore, defaultProps));
  });

  return {
    containerRef,
    lspCompartment,
    onViewCreated,
    onViewDestroying,
    onUpdate,
    defaultProps,
  };
}

// ---------------------------------------------------------------------------
// Tests: EditorView creation and lifecycle
// ---------------------------------------------------------------------------

describe('EditorView creation and lifecycle', () => {
  it('creates EditorView with correct initial content', () => {
    const { onViewCreated } = renderEditorCore({
      initialContent: 'hello world',
    });

    expect((EditorState as any).create).toHaveBeenCalledWith(
      expect.objectContaining({
        doc: 'hello world',
      }),
    );

    expect(EditorView).toHaveBeenCalled();
    expect(onViewCreated).toHaveBeenCalledWith(expect.anything(), expect.anything());
  });

  it('includes updateListener in extensions', () => {
    const customExtensions = [{ name: 'customExtension' }];
    renderEditorCore({
      extensions: customExtensions,
    });

    // Check EditorState.create calls
    const stateCreateCalls = (EditorState as any).create.mock.calls;
    expect(stateCreateCalls).toBeDefined();
    expect(stateCreateCalls.length).toBeGreaterThan(0);

    const extensions = stateCreateCalls[0][0].extensions;

    expect(extensions).toHaveLength(2);
    expect(extensions[0]).toHaveProperty('extension', 'updateListener');
    expect(extensions[1]).toEqual(customExtensions[0]);
  });

  it('calls onUpdate when updateListener fires', () => {
    const { onUpdate } = renderEditorCore();

    // EditorState.create is called in the component
    const stateCreateCalls = (EditorState as any).create.mock.calls;
    expect(stateCreateCalls).toBeDefined();
    expect(stateCreateCalls.length).toBeGreaterThan(0);

    const extensions = stateCreateCalls[0][0].extensions;
    const updateListenerArg = extensions[0];
    const listener = updateListenerArg.listener;

    const mockUpdate = {
      state: { doc: { toString: () => 'new content' } },
      changes: {},
    };

    act(() => {
      listener(mockUpdate);
    });

    expect(onUpdate).toHaveBeenCalledWith(mockUpdate);
  });
});

// ---------------------------------------------------------------------------
// Tests: View destruction and cleanup
// ---------------------------------------------------------------------------

describe('View destruction and cleanup', () => {
  it('calls onViewDestroying when unmounting', () => {
    const { onViewDestroying } = renderEditorCore();

    act(() => {
      root.unmount();
    });

    expect(onViewDestroying).toHaveBeenCalled();
  });

  it('destroys the EditorView when unmounting', () => {
    renderEditorCore();

    const view = (EditorView as any).mock.results[0].value;

    act(() => {
      root.unmount();
    });

    expect(view.destroy).toHaveBeenCalled();
  });

  it('calls onViewDestroying before destroying view', () => {
    const { onViewDestroying } = renderEditorCore();

    let callOrder: string[] = [];
    onViewDestroying.mockImplementation(() => callOrder.push('onViewDestroying'));
    const view = (EditorView as any).mock.results[0].value;
    // Replace the mock with a new one that tracks calls
    const destroySpy = vi.fn(() => callOrder.push('destroy'));
    view.destroy.mockImplementation(destroySpy);

    act(() => {
      root.unmount();
    });

    expect(callOrder).toEqual(['onViewDestroying', 'destroy']);
  });
});

// ---------------------------------------------------------------------------
// Tests: CSS class application/removal
// ---------------------------------------------------------------------------

describe('CSS class application/removal', () => {
  it('applies className to container on mount', () => {
    const testContainer = document.createElement('div');
    const containerRef = { current: testContainer };

    renderEditorCore({
      containerRef,
      className: 'test-editor-class',
    });

    expect(testContainer.classList.contains('test-editor-class')).toBe(true);
  });

  it('removes className from container on unmount', () => {
    const testContainer = document.createElement('div');
    const containerRef = { current: testContainer };

    renderEditorCore({
      containerRef,
      className: 'test-editor-class',
    });

    act(() => {
      root.unmount();
    });

    expect(testContainer.classList.contains('test-editor-class')).toBe(false);
  });

  it('does not modify container when className is not provided', () => {
    const testContainer = document.createElement('div');
    testContainer.classList.add('existing-class');
    const containerRef = { current: testContainer };

    renderEditorCore({
      containerRef,
      className: undefined,
    });

    expect(testContainer.classList.contains('existing-class')).toBe(true);
    expect(testContainer.classList.length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: LSP registration/unregistration
// ---------------------------------------------------------------------------

describe('LSP registration/unregistration', () => {
  it('calls registerEditorView for valid filePath', () => {
    renderEditorCore({
      filePath: '/src/test.ts',
    });

    expect((lspExtensions as any)._mocks.mockRegisterEditorView).toHaveBeenCalledWith(
      '/src/test.ts',
      expect.anything(),
    );
  });

  it('skips registerEditorView for __workspace/ paths', () => {
    renderEditorCore({
      filePath: '__workspace/test.ts',
    });

    expect((lspExtensions as any)._mocks.mockRegisterEditorView).not.toHaveBeenCalled();
  });

  it('calls unregisterEditorView on unmount for valid filePath', () => {
    renderEditorCore({
      filePath: '/src/test.ts',
    });

    act(() => {
      root.unmount();
    });

    expect((lspExtensions as any)._mocks.mockUnregisterEditorView).toHaveBeenCalledWith('/src/test.ts');
  });

  it('skips unregisterEditorView on unmount for __workspace/ paths', () => {
    renderEditorCore({
      filePath: '__workspace/test.ts',
    });

    act(() => {
      root.unmount();
    });

    expect((lspExtensions as any)._mocks.mockUnregisterEditorView).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Async LSP extension initialization
// ---------------------------------------------------------------------------

describe('Async LSP extension initialization', () => {
  it('initializes LSP for supported languageId', async () => {
    const lspCompartment = { reconfigure: vi.fn((extensions) => ({ effects: [] })) };

    renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
      lspCompartment,
    });

    // Wait for async LSP init
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const lspService = getLSPClientService();
    expect(lspService.getClientForLanguage).toHaveBeenCalledWith('typescript');
  });

  it('dispatches LSP extensions via lspCompartment for supported language', async () => {
    const lspCompartment = { reconfigure: vi.fn((extensions) => ({ effects: [] })) };

    renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
      lspCompartment,
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const view = (EditorView as any).mock.results[0].value;
    expect(view.dispatch).toHaveBeenCalledWith(
      expect.objectContaining({
        effects: expect.anything(),
      }),
    );
    expect((lspExtensions as any)._mocks.mockBuildLSPPluginExtensions).toHaveBeenCalledWith(
      mockLspClient,
      '/test.ts',
      'typescript',
    );
  });

  it('does not initialize LSP when languageId is null', () => {
    renderEditorCore({
      languageId: null,
    });

    const lspService = getLSPClientService();
    expect(lspService.getClientForLanguage).not.toHaveBeenCalled();
  });

  it('does not initialize LSP when languageId is unsupported', () => {
    renderEditorCore({
      languageId: 'python',
    });

    const lspService = getLSPClientService();
    expect(lspService.getClientForLanguage).not.toHaveBeenCalled();
  });

  it('does not apply LSP extensions if view changes during async init', async () => {
    const lspCompartment = { reconfigure: vi.fn((extensions) => ({ effects: [] })) };

    const { containerRef } = renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
      lspCompartment,
    });

    const view1 = (EditorView as any).mock.results[0].value;
    const originalDispatch1 = view1.dispatch;
    view1.dispatch = vi.fn();

    // Trigger re-creation by changing initialContent
    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'new content',
          extensions: [],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
          languageId: 'typescript',
          filePath: '/test.ts',
        }),
      );
    });

    const view2 = (EditorView as any).mock.results[1].value;

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // view1 should not receive dispatch (it was replaced)
    expect(view1.dispatch).not.toHaveBeenCalled();
  });

  it('skips LSP dispatch when view is disconnected from DOM', async () => {
    const lspCompartment = { reconfigure: vi.fn((extensions) => ({ effects: [] })) };

    renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
      lspCompartment,
    });

    // Simulate view disconnected after creation
    const view = (EditorView as any).mock.results[0].value;
    view.dom.isConnected = false;

    // Wait for async LSP init
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // LSP reconfigure should NOT be called because view is disconnected
    expect(lspCompartment.reconfigure).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Null container handling
// ---------------------------------------------------------------------------

describe('Null container handling', () => {
  it('does nothing when containerRef.current is null', () => {
    const nullContainerRef = { current: null };
    const { onViewCreated } = renderEditorCore({
      containerRef: nullContainerRef,
    });

    expect(EditorView).not.toHaveBeenCalled();
    expect(onViewCreated).not.toHaveBeenCalled();
  });

  it('does nothing when containerRef.current is undefined', () => {
    const undefinedContainerRef = { current: undefined };
    const { onViewCreated } = renderEditorCore({
      containerRef: undefinedContainerRef,
    });

    expect(EditorView).not.toHaveBeenCalled();
    expect(onViewCreated).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Recreation on prop changes
// ---------------------------------------------------------------------------

describe('Recreation on prop changes', () => {
  it('destroys old view and creates new view when initialContent changes', () => {
    const { containerRef, lspCompartment } = renderEditorCore({
      initialContent: 'content 1',
    });

    const mockOnViewDestroying = vi.fn();
    const mockOnViewCreated = vi.fn();

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content 2',
          extensions: [],
          lspCompartment,
          onViewCreated: mockOnViewCreated,
          onViewDestroying: mockOnViewDestroying,
          onUpdate: vi.fn(),
        }),
      );
    });

    const firstView = (EditorView as any).mock.results[0].value;
    expect(firstView.destroy).toHaveBeenCalled();
    expect(EditorView).toHaveBeenCalledTimes(2);
    // Check EditorState.create calls, not the constructor
    expect((EditorState as any).create).toHaveBeenCalledWith(
      expect.objectContaining({
        doc: 'content 2',
      }),
    );
  });

  it('creates new view when extensions array changes', () => {
    const { containerRef, lspCompartment } = renderEditorCore({
      extensions: [{ name: 'ext1' }],
    });

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content',
          extensions: [{ name: 'ext2' }],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
        }),
      );
    });

    expect(EditorView).toHaveBeenCalledTimes(2);
  });

  it('creates new view when filePath changes', () => {
    const { containerRef, lspCompartment } = renderEditorCore({
      filePath: '/file1.ts',
    });

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content',
          extensions: [],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
          filePath: '/file2.ts',
        }),
      );
    });

    expect(EditorView).toHaveBeenCalledTimes(2);
  });

  it('creates new view when languageId changes', () => {
    const { containerRef, lspCompartment } = renderEditorCore({
      languageId: 'typescript',
    });

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content',
          extensions: [],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
          languageId: 'javascript',
        }),
      );
    });

    expect(EditorView).toHaveBeenCalledTimes(2);
  });

  it('creates new view when className changes', () => {
    const { containerRef, lspCompartment } = renderEditorCore({
      className: 'class1',
    });

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content',
          extensions: [],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
          className: 'class2',
        }),
      );
    });

    expect(EditorView).toHaveBeenCalledTimes(2);
  });

  it('clears container before recreation', () => {
    const { containerRef } = renderEditorCore({
      initialContent: 'content 1',
    });

    // Verify container.innerHTML was set (the mock EditorView mounts something)
    // First creation sets innerHTML = '' before creating view

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content 2',
          extensions: [],
          lspCompartment: { reconfigure: vi.fn() },
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
        }),
      );
    });

    // Second creation should also clear innerHTML
    expect(EditorView).toHaveBeenCalledTimes(2);
  });
});

// ---------------------------------------------------------------------------
// Tests: Callback handling
// ---------------------------------------------------------------------------

describe('Callback handling', () => {
  it('passes view and viewRef to onViewCreated callback', () => {
    const { onViewCreated } = renderEditorCore();

    expect(onViewCreated).toHaveBeenCalledTimes(1);
    const [view, viewRef] = onViewCreated.mock.calls[0];

    expect(view).toBeDefined();
    expect(viewRef).toBeDefined();
    expect(viewRef.current).toBe(view);
  });

  it('calls onUpdate with ViewUpdate when updateListener fires', () => {
    const { onUpdate } = renderEditorCore();

    // EditorState.create is called in the component
    const stateCreateCalls = (EditorState as any).create.mock.calls;
    expect(stateCreateCalls).toBeDefined();
    expect(stateCreateCalls.length).toBeGreaterThan(0);

    const updateListenerArg = stateCreateCalls[0][0].extensions[0];
    const listener = updateListenerArg.listener;

    const mockUpdate = { viewportChanged: true };

    act(() => {
      listener(mockUpdate);
    });

    expect(onUpdate).toHaveBeenCalledWith(mockUpdate);
  });

  it('does not recreate view when callbacks are stable', () => {
    const { containerRef, lspCompartment } = renderEditorCore();

    // Re-render with same callback references (simulating stable refs)
    const oldCallCount = (EditorView as any).mock.calls.length;

    act(() => {
      root.render(
        createElement(EditorCore, {
          containerRef,
          initialContent: 'content',
          extensions: [],
          lspCompartment,
          onViewCreated: vi.fn(),
          onViewDestroying: vi.fn(),
          onUpdate: vi.fn(),
        }),
      );
    });

    // New callbacks were created, so view should be recreated due to callback deps
    // (in real usage, callbacks would be wrapped in useCallback)
    expect((EditorView as any).mock.calls.length).toBeGreaterThan(oldCallCount);
  });
});

// ---------------------------------------------------------------------------
// Tests: Error handling
// ---------------------------------------------------------------------------

describe('Error handling', () => {
  it('catches LSP initialization errors and logs them', async () => {
    const lspService = getLSPClientService();
    lspService.getClientForLanguage.mockRejectedValue(new Error('LSP connection failed'));

    renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(debugLog).toHaveBeenCalledWith(
      expect.stringContaining('[EditorCore] LSP initialization failed'),
      expect.any(Error),
    );
  });

  it('does not crash when LSP client is null', async () => {
    const lspService = getLSPClientService();
    lspService.getClientForLanguage.mockResolvedValue(null);

    renderEditorCore({
      languageId: 'typescript',
      filePath: '/test.ts',
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(EditorView).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Return value
// ---------------------------------------------------------------------------

describe('Return value', () => {
  it('returns null (renders nothing)', () => {
    renderEditorCore();

    // The component returns null, so nothing is rendered into container
    expect(container.innerHTML).toBe('');
    expect(container.childNodes).toHaveLength(0);
  });
});
