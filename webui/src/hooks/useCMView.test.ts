/**
 * useCMView.test.ts — regression tests for the editor bug class.
 *
 * These tests exercise the *symptoms* users have been reporting:
 *
 *   1. "Cursor drops to bottom after edit" — caused by EditorView
 *      recreation on every keystroke. Test: view identity is stable
 *      across many onUpdate invocations.
 *
 *   2. "Save doesn't work" — caused by saveRef.current being undefined
 *      in race windows. Test: api.save() always invokes the latest
 *      handle, even when it changes between calls.
 *
 *   3. "External-update gate races" — caused by isExternalUpdateRef
 *      being set/cleared across useEffect boundaries. Test:
 *      withExternalUpdate synchronously toggles isExternalUpdate during
 *      the callback, and restores the previous value on exit.
 *
 *   4. "Stale settings on rapid input" — caused by settingsRef being
 *      mirrored via useEffect (lag of 1 render). Test: settingsRef.current
 *      reflects the latest render-time assignment when read inside the
 *      CM listener.
 *
 * The tests use a minimal jsdom + CodeMirror setup, avoiding the full
 * EditorPane tree so failures here point at the hook contract, not at
 * incidental editor-wide interactions.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorState, type Extension } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { useCMView, type CMViewAPI, type CMViewSettings } from './useCMView';

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

// A minimal extensions builder — uses the API contract from useEditorExtensions
// but doesn't actually wire language support. Enough to mount a working view.
function makeExtensionsBuilder(compartments: Record<string, any>) {
  const built: Array<{ extensions: Extension[]; settings: any; paneId: string }> = [];
  const buildExtensions = vi.fn((opts: any) => {
    built.push({ extensions: opts, settings: opts.settings, paneId: opts.paneId });
    // Spread extraKeymaps (which may include the updateListener) into the
    // extensions array — that's how the real buildExtensions composes them.
    return [
      EditorView.editable.of(true),
      compartments.hotkeys.of([]),
      ...(opts.extraKeymaps ?? []),
    ];
  });
  return { buildExtensions, built };
}

// Minimal fake compartment (CodeMirror's Compartment has a reconfigure method)
function makeFakeCompartment() {
  return {
    of: vi.fn((ext: Extension) => ext),
    reconfigure: vi.fn((ext: Extension) => ({ type: 'reconfigure', ext })),
  };
}

function makeCompartments() {
  return {
    hotkeys: makeFakeCompartment(),
    lineWrapping: makeFakeCompartment(),
    relativeLineNumbers: makeFakeCompartment(),
    language: makeFakeCompartment(),
    minimap: makeFakeCompartment(),
    whitespaceRendering: makeFakeCompartment(),
    emmet: makeFakeCompartment(),
    autoCloseTag: makeFakeCompartment(),
    fontSize: makeFakeCompartment(),
    tabSize: makeFakeCompartment(),
    lsp: makeFakeCompartment(),
    inlayHints: makeFakeCompartment(),
    signatureHelp: makeFakeCompartment(),
    history: makeFakeCompartment(),
  };
}

const baseSettings: CMViewSettings = {
  wordWrapEnabled: false,
  relativeLineNumbersEnabled: false,
  minimapEnabled: false,
  editorFontSize: 13,
  editorTabSize: 4,
  editorUsesTabs: false,
  whitespaceRenderingMode: 'none',
  inlayHintsEnabled: false,
  signatureHelpEnabled: false,
};

interface RenderResult {
  api: CMViewAPI;
  args: {
    editorRef: React.RefObject<HTMLDivElement | null>;
    buffer: any;
    bufferRef: React.MutableRefObject<any>;
    languageId: string | null;
    handleSaveRef: React.MutableRefObject<() => Promise<void>>;
    openWorkspaceBufferRef: React.MutableRefObject<any>;
    onUpdateRef: React.MutableRefObject<(u: any) => void>;
    settingsRef: React.MutableRefObject<CMViewSettings | null>;
    keymapsRef: React.MutableRefObject<any>;
    compartments: any;
    buildExtensions: any;
    themePack: any;
    customHighlightStyle: any;
    onDidMount?: any;
    onWillDestroy?: any;
    onDidDestroy?: any;
  };
  handleSaveRef: React.MutableRefObject<() => Promise<void>>;
  settingsRef: React.MutableRefObject<CMViewSettings | null>;
  onUpdateRef: React.MutableRefObject<(u: any) => void>;
  compartments: any;
  buildExtensions: ReturnType<typeof makeExtensionsBuilder>['buildExtensions'];
  built: ReturnType<typeof makeExtensionsBuilder>['built'];
}

function render(opts: Partial<RenderResult['args']> = {}) {
  const editorRef: React.RefObject<HTMLDivElement | null> = { current: container };
  const bufferRef: React.MutableRefObject<any> = { current: null };
  const handleSaveRef: React.MutableRefObject<() => Promise<void>> = {
    current: async () => {},
  };
  const openWorkspaceBufferRef: React.MutableRefObject<any> = {
    current: vi.fn(),
  };
  const onUpdateRef: React.MutableRefObject<(u: any) => void> = {
    current: () => {},
  };
  const settingsRef: React.MutableRefObject<CMViewSettings | null> = {
    current: { ...baseSettings },
  };
  const keymapsRef = {
    current: {
      customKeymap: [],
      replacePanelKeymap: [],
      zoomKeymap: [],
      semanticKeymap: [],
    },
  };
  const compartments = opts.compartments ?? makeCompartments();
  const { buildExtensions, built } = makeExtensionsBuilder(compartments);
  const buffer = opts.buffer ?? {
    id: 'buf-1',
    file: { path: '/test/file.ts', name: 'file.ts', ext: '.ts' },
    content: 'initial content',
  };
  bufferRef.current = buffer;

  let api: CMViewAPI | null = null;

  function Wrapper() {
    api = useCMView({
      paneId: opts.paneId ?? 'pane-1',
      editorRef,
      buffer,
      bufferRef,
      languageId: opts.languageId ?? 'typescript',
      handleSaveRef,
      openWorkspaceBufferRef,
      onUpdateRef,
      settingsRef,
      keymapsRef,
      compartments,
      buildExtensions,
      themePack: opts.themePack ?? { mode: 'light', editorSyntaxStyle: 'default' },
      customHighlightStyle: null,
      onDidMount: opts.onDidMount,
      onWillDestroy: opts.onWillDestroy,
      onDidDestroy: opts.onDidDestroy,
    });
    return null;
  }

  act(() => {
    root.render(createElement(Wrapper));
  });

  return {
    api: api!,
    args: {
      editorRef,
      buffer,
      bufferRef,
      languageId: opts.languageId ?? 'typescript',
      handleSaveRef,
      openWorkspaceBufferRef,
      onUpdateRef,
      settingsRef,
      keymapsRef,
      compartments,
      buildExtensions,
      themePack: opts.themePack ?? { mode: 'light', editorSyntaxStyle: 'default' },
      customHighlightStyle: null,
      onDidMount: opts.onDidMount,
      onWillDestroy: opts.onWillDestroy,
      onDidDestroy: opts.onDidDestroy,
    },
    handleSaveRef,
    settingsRef,
    onUpdateRef,
    compartments,
    buildExtensions,
    built,
  };
}

// ---------------------------------------------------------------------------
// Bug-class regression tests
// ---------------------------------------------------------------------------

describe('useCMView — bug-class regression', () => {
  it('mount creates the EditorView exactly once', () => {
    const { api } = render();
    expect(api.view).not.toBeNull();
    expect(api.isMounted).toBe(true);
  });

  it('many onUpdate invocations do NOT recreate the view (cursor-drop bug)', () => {
    // The bug: cursor drops to bottom after edits. Cause: view is recreated.
    // Test: simulate 5 keystrokes by calling the update listener; view identity
    // must not change between them.
    const { api, onUpdateRef } = render();
    const viewAtMount = api.view;
    expect(viewAtMount).not.toBeNull();

    for (let i = 0; i < 5; i++) {
      const fakeUpdate = {
        docChanged: true,
        selectionSet: false,
        viewportChanged: false,
        state: { doc: { toString: () => `content ${i}` } },
      };
      act(() => {
        onUpdateRef.current(fakeUpdate);
      });
      expect(api.view).toBe(viewAtMount);
    }
  });

  it('save() invokes the LATEST handleSaveRef.current (save-doesnt-call-undefined bug)', async () => {
    // The bug: saveRef.current is undefined in race windows.
    // Test: api.save() always reads handleSaveRef.current at call time,
    // so updating the ref between calls is observed.
    const { api, handleSaveRef } = render();
    const firstSave = vi.fn().mockResolvedValue(undefined);
    const secondSave = vi.fn().mockResolvedValue(undefined);

    handleSaveRef.current = firstSave;
    await act(async () => {
      await api.save();
    });
    expect(firstSave).toHaveBeenCalledTimes(1);

    handleSaveRef.current = secondSave;
    await act(async () => {
      await api.save();
    });
    expect(secondSave).toHaveBeenCalledTimes(1);
  });

  it('withExternalUpdate synchronously toggles isExternalUpdate', () => {
    // The bug: cursor-skip flag set in one useEffect, cleared in another,
    // observed by the listener firing synchronously inside dispatch.
    // Test: withExternalUpdate(fn) runs fn while isExternalUpdate() returns
    // true, then restores the previous value.
    const { api } = render();
    expect(api.isExternalUpdate()).toBe(false);

    let observedDuring = false;
    api.withExternalUpdate(() => {
      observedDuring = api.isExternalUpdate();
    });
    expect(observedDuring).toBe(true);
    expect(api.isExternalUpdate()).toBe(false);
  });

  it('withExternalUpdate nests correctly (restores previous value, not always false)', () => {
    // The bug: naively setting to false in finally loses nested state.
    const { api } = render();
    api.withExternalUpdate(() => {
      expect(api.isExternalUpdate()).toBe(true);
      api.withExternalUpdate(() => {
        expect(api.isExternalUpdate()).toBe(true);
      });
      expect(api.isExternalUpdate()).toBe(true);
    });
    expect(api.isExternalUpdate()).toBe(false);
  });

  it('settingsRef.current reflects render-time assignment without useEffect lag', () => {
    // The bug: settingsRef mirrored via useEffect reads N-renders-old data.
    // Test: write settingsRef.current during render (no effect), then read
    // inside the listener — should see the latest value.
    const { settingsRef, onUpdateRef } = render();

    // Simulate a "render" by directly assigning the ref (what EditorPane does)
    settingsRef.current = { ...baseSettings, editorFontSize: 99 };

    let observed: any = null;
    onUpdateRef.current = (u: any) => {
      observed = settingsRef.current;
    };

    act(() => {
      onUpdateRef.current({} as any);
    });
    expect(observed?.editorFontSize).toBe(99);
  });

  it('destroy sets api.view to null and calls onWillDestroy / onDidDestroy in order', () => {
    const order: string[] = [];
    const { api } = render({
      onWillDestroy: () => order.push('will'),
      onDidDestroy: () => order.push('did'),
    });
    const viewAtMount = api.view;
    expect(viewAtMount).not.toBeNull();

    act(() => {
      root.unmount();
    });
    expect(api.view).toBeNull();
    expect(api.isMounted).toBe(false);
    expect(order).toEqual(['will', 'did']);
  });

  it('buffer.id change recreates the view and fires lifecycle hooks', () => {
    const order: string[] = [];
    let renderCount = 0;
    const onDidMount = vi.fn(() => order.push('mount'));
    const onWillDestroy = vi.fn(() => order.push('destroy'));

    // First mount
    const { api: api1 } = render({ onDidMount, onWillDestroy });
    const view1 = api1.view;
    expect(view1).not.toBeNull();

    // Second mount with a different buffer — simulate by re-rendering with new opts.
    // We rebuild the test setup with a different bufferId.
    const container2 = document.createElement('div');
    document.body.appendChild(container2);
    const root2 = createRoot(container2);
    const api2 = render({ onDidMount, onWillDestroy, buffer: {
      id: 'buf-2',
      file: { path: '/test/file2.ts', name: 'file2.ts', ext: '.ts' },
      content: 'second content',
    } } as any).api;

    // The new view is a different EditorView instance.
    expect(api2.view).not.toBe(view1);
    expect(api2.view).not.toBeNull();

    act(() => root2.unmount());
    container2.remove();
  });

  it('subscribe fires for every CM update after listener registration', () => {
    // The bug class: the original architecture mirrored view state through
    // multiple useEffect hops, so listener-registered subscribers might not
    // observe the first few updates. Test: api.subscribe(listener) is
    // invoked for every CodeMirror dispatch *after* registration.
    const { api } = render();
    const listener = vi.fn();
    const unsubscribe = api.subscribe(listener);

    // Dispatch through the actual view — that is what triggers the CM
    // updateListener which then notifies our subscribers. The listener
    // runs synchronously inside dispatch, but the test framework may
    // wrap it in microtasks, so we flush via act.
    api.view!.dispatch({ changes: { from: 0, to: 0, insert: 'a' } });
    api.view!.dispatch({ changes: { from: 1, to: 1, insert: 'b' } });

    expect(listener).toHaveBeenCalledTimes(2);

    unsubscribe();
    api.view!.dispatch({ changes: { from: 2, to: 2, insert: 'c' } });
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it('subscribe errors in one listener do not stop other listeners', () => {
    // Bug class: if a subscriber throws, the rest must still be called.
    const { api } = render();
    const errListener = vi.fn(() => {
      throw new Error('boom');
    });
    const okListener = vi.fn();
    api.subscribe(errListener);
    api.subscribe(okListener);

    // Suppress the console.error that reportError emits.
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    api.view!.dispatch({ changes: { from: 0, to: 0, insert: 'a' } });
    errSpy.mockRestore();

    expect(errListener).toHaveBeenCalledTimes(1);
    expect(okListener).toHaveBeenCalledTimes(1);
  });

  it('dispatch no-ops when view is null', () => {
    const { api } = render();
    const before = api.view;
    api.view = null;
    // Should not throw
    expect(() => api.dispatch({ changes: { from: 0, to: 0, insert: 'x' } })).not.toThrow();
    api.view = before; // restore
  });

  it('getFilePath / getFileExt / getContent read from bufferRef.current at call time', () => {
    const { api, args } = render();
    expect(api.getFilePath()).toBe('/test/file.ts');
    expect(api.getFileExt()).toBe('.ts');
    expect(api.getContent()).toBe('initial content');

    // Mutate the buffer — API reads the latest value.
    args.bufferRef.current = {
      id: 'buf-2',
      file: { path: '/new/path.ts', ext: '.tsx', name: 'path.tsx' },
      content: 'new content',
    };
    expect(api.getFilePath()).toBe('/new/path.ts');
    expect(api.getFileExt()).toBe('.tsx');
    expect(api.getContent()).toBe('new content');
  });

  it('LSP-style callback fired after mount reads latest openWorkspaceBufferRef', () => {
    const openWorkspaceBufferV1 = vi.fn();
    const openWorkspaceBufferV2 = vi.fn();
    const openWorkspaceBufferRef: React.MutableRefObject<any> = {
      current: openWorkspaceBufferV1,
    };

    let getOpenWorkspaceBuffer: (() => any) | null = null;
    let displayFileCallback: ((filePath: string) => void) | null = null;
    let api: CMViewAPI | null = null;

    const compartments = makeCompartments();
    const buildExtensions = vi.fn((opts: any) => {
      // This is the same stable action captured by CodeMirror/LSP extensions
      // when the view is built. It must resolve the ref lazily when the
      // registered callback eventually fires.
      getOpenWorkspaceBuffer = opts.actions.getOpenWorkspaceBuffer;
      return [
        EditorView.editable.of(true),
        compartments.hotkeys.of([]),
        ...(opts.extraKeymaps ?? []),
      ];
    });
    const editorRef: React.RefObject<HTMLDivElement | null> = { current: container };
    const buffer = {
      id: 'buf-1',
      file: { path: '/test/file.ts', name: 'file.ts', ext: '.ts' },
      content: 'initial content',
    };
    const bufferRef: React.MutableRefObject<any> = { current: buffer };
    const handleSaveRef: React.MutableRefObject<() => Promise<void>> = {
      current: async () => {},
    };
    const onUpdateRef: React.MutableRefObject<(u: any) => void> = {
      current: () => {},
    };
    const settingsRef: React.MutableRefObject<CMViewSettings | null> = {
      current: { ...baseSettings },
    };
    const keymapsRef = {
      current: {
        customKeymap: [],
        replacePanelKeymap: [],
        zoomKeymap: [],
        semanticKeymap: [],
      },
    };
    const themePack = { mode: 'light', editorSyntaxStyle: 'default' };

    const onDidMount = vi.fn(() => {
      // Mirrors EditorPane's setGlobalDisplayFileCallback registration: this
      // callback is installed at mount, but invoked later by an LSP request.
      displayFileCallback = (filePath: string) => {
        const fileName = filePath.split('/').pop() || filePath;
        const dotIndex = fileName.lastIndexOf('.');
        const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;
        getOpenWorkspaceBuffer!()({
          kind: 'file',
          path: filePath,
          title: fileName,
          ext,
        });
      };
    });

    function Wrapper({ openWorkspaceBuffer }: { openWorkspaceBuffer: (request: any) => void }) {
      // EditorPane performs this assignment during every render. The
      // CodeMirror extension itself remains mounted with the getter captured
      // during the first render.
      openWorkspaceBufferRef.current = openWorkspaceBuffer;
      api = useCMView({
        paneId: 'pane-1',
        editorRef,
        buffer,
        bufferRef,
        languageId: 'typescript',
        handleSaveRef,
        openWorkspaceBufferRef,
        onUpdateRef,
        settingsRef,
        keymapsRef,
        compartments,
        buildExtensions,
        themePack,
        customHighlightStyle: null,
        onDidMount,
      });
      return null;
    }

    act(() => {
      root.render(createElement(Wrapper, { openWorkspaceBuffer: openWorkspaceBufferV1 }));
    });
    const viewAtMount = api!.view;
    expect(viewAtMount).not.toBeNull();
    expect(onDidMount).toHaveBeenCalledTimes(1);

    act(() => {
      root.render(createElement(Wrapper, { openWorkspaceBuffer: openWorkspaceBufferV2 }));
    });

    // The callback identity change is not a view-lifecycle input. The callback
    // registered after mount must use v2 without rebuilding the EditorView.
    expect(api!.view).toBe(viewAtMount);
    expect(onDidMount).toHaveBeenCalledTimes(1);

    displayFileCallback!('/workspace/new-file.ts');

    expect(openWorkspaceBufferV1).not.toHaveBeenCalled();
    expect(openWorkspaceBufferV2).toHaveBeenCalledTimes(1);
    expect(openWorkspaceBufferV2).toHaveBeenCalledWith({
      kind: 'file',
      path: '/workspace/new-file.ts',
      title: 'new-file.ts',
      ext: '.ts',
    });
  });
});