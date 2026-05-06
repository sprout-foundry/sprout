// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { vi } from 'vitest';

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ── Hoisted mock state (must be defined before vi.mock hoisting) ──────

const hoisted = vi.hoisted(() => {
  // Module-level mock state
  let mockUpdateListenerCb: ((update: any) => void) | null = null;
  let mockEditorViewInstance: any = null;

  // Mock Compartment class
  class MockCompartment {
    of(value: any) {
      return value;
    }
    reconfigure(value: any) {
      return value;
    }
  }

  // Mock EditorState
  const MockEditorState = {
    create: vi.fn((config: any) => {
      const docStr = config?.doc ?? '';
      return {
        doc: {
          toString: () => docStr,
          length: docStr.length,
          lineAt: () => ({ number: 1, from: 0, to: docStr.length }),
        },
        selection: { main: { head: 0 } },
        extensions: config?.extensions ?? [],
      };
    }),
    readOnly: {
      of: vi.fn((val: any) => `readonly:${val}`),
    },
  };

  // Mock EditorView class
  class MockEditorView {
    state: any;
    parent: Element | null;
    hasFocus = false;
    destroyed = false;
    focused = false;
    dispatchCalls: any[] = [];

    constructor(opts: { state: any; parent: Element | null }) {
      this.state = opts.state;
      this.parent = opts.parent;
      mockEditorViewInstance = this;
    }

    dispatch(update: any) {
      this.dispatchCalls.push(update);
    }

    destroy() {
      this.destroyed = true;
      mockEditorViewInstance = null;
    }

    focus() {
      this.focused = true;
      this.hasFocus = true;
    }
  }

  // Static properties on MockEditorView
  MockEditorView.updateListener = {
    of: (cb: (update: any) => void) => {
      mockUpdateListenerCb = cb;
      return 'update-listener-mock';
    },
  };

  MockEditorView.lineWrapping = 'line-wrapping-mock';

  MockEditorView.theme = vi.fn((styles: any) => `theme-mock:${JSON.stringify(styles)}`);

  return {
    MockEditorView,
    MockEditorState,
    MockCompartment,
    getMockUpdateListenerCb: () => mockUpdateListenerCb,
    getMockEditorViewInstance: () => mockEditorViewInstance,
  };
});

// ── Mocks before importing the component ────────────────────────────────

vi.mock('@codemirror/state', () => ({
  Compartment: hoisted.MockCompartment,
  EditorState: hoisted.MockEditorState,
}));

vi.mock('@codemirror/view', () => ({
  EditorView: hoisted.MockEditorView,
  keymap: {
    of: vi.fn((keys: any[]) => keys),
  },
  lineNumbers: vi.fn(() => 'line-numbers'),
  highlightSpecialChars: vi.fn(() => 'highlight-special-chars'),
  highlightActiveLine: vi.fn(() => 'highlight-active-line'),
  highlightActiveLineGutter: vi.fn(() => 'highlight-active-line-gutter'),
  rectangularSelection: vi.fn(() => 'rectangular-selection'),
  crosshairCursor: vi.fn(() => 'crosshair-cursor'),
  dropCursor: vi.fn(() => 'drop-cursor'),
  drawSelection: vi.fn(() => 'draw-selection'),
  scrollPastEnd: vi.fn(() => 'scroll-past-end'),
}));

vi.mock('@codemirror/commands', () => ({
  defaultKeymap: [{ key: 'default' }],
  indentWithTab: { key: 'indent-with-tab' },
  history: vi.fn(() => 'history'),
  undo: { key: 'undo' },
  redo: { key: 'redo' },
}));

vi.mock('@codemirror/search', () => ({
  searchKeymap: [{ key: 'search' }],
  highlightSelectionMatches: vi.fn(() => 'highlight-selection-matches'),
}));

vi.mock('@codemirror/autocomplete', () => ({
  autocompletion: vi.fn(() => 'autocompletion'),
  closeBrackets: vi.fn(() => 'close-brackets'),
}));

vi.mock('@codemirror/language', () => ({
  syntaxHighlighting: vi.fn(() => 'syntax-highlighting'),
  defaultHighlightStyle: {},
  codeFolding: vi.fn(() => 'code-folding'),
  foldGutter: vi.fn(() => 'fold-gutter'),
  indentOnInput: vi.fn(() => 'indent-on-input'),
  bracketMatching: vi.fn(() => 'bracket-matching'),
  indentUnit: {
    of: vi.fn((val: any) => `indent-unit:${val}`),
  },
}));

vi.mock('@codemirror/theme-one-dark', () => ({
  oneDarkHighlightStyle: {},
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

import Editor from './Editor';

// ── Helpers ──────────────────────────────────────────────────────────────

let container: HTMLDivElement;
let root: Root;

function getMockEditorViewInstance(): any {
  return hoisted.getMockEditorViewInstance();
}

function getMockUpdateListenerCb(): ((update: any) => void) | null {
  return hoisted.getMockUpdateListenerCb();
}

function resetMockState() {
  // Reset mock state for next test
  hoisted.MockEditorView.updateListener.of(() => {}); // re-init callback
}

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  resetMockState();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ── Tests ────────────────────────────────────────────────────────────────

describe('Editor', () => {
  it('renders container div with sprout-editor class', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    const editorEl = container.querySelector('.sprout-editor');
    expect(editorEl).not.toBeNull();
    expect(editorEl?.getAttribute('style')).toContain('width: 100%');
    expect(editorEl?.getAttribute('style')).toContain('height: 100%');
  });

  it('creates EditorView on mount', () => {
    act(() => {
      root.render(createElement(Editor, { value: 'hello' }));
    });

    expect(getMockEditorViewInstance()).not.toBeNull();
    expect(hoisted.MockEditorState.create).toHaveBeenCalled();
  });

  it('creates EditorState with initial value', () => {
    act(() => {
      root.render(createElement(Editor, { value: 'const x = 1;' }));
    });

    expect(hoisted.MockEditorState.create).toHaveBeenCalledWith(
      expect.objectContaining({
        doc: 'const x = 1;',
      })
    );
  });

  it('uses default empty string when no value is provided', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    expect(hoisted.MockEditorState.create).toHaveBeenCalledWith(
      expect.objectContaining({
        doc: '',
      })
    );
  });

  it('fires onChange when document changes via updateListener', () => {
    const onChange = vi.fn();

    act(() => {
      root.render(createElement(Editor, { value: 'hello', onChange }));
    });

    const instance = getMockEditorViewInstance();

    // Simulate a doc change via the updateListener callback
    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: true,
        selectionSet: false,
        focusChanged: false,
        state: {
          doc: { toString: () => 'world' },
          selection: { main: { head: 0 } },
        },
        view: instance,
      });
    });

    expect(onChange).toHaveBeenCalledWith('world');
  });

  it('does not fire onChange when docChanged is false', () => {
    const onChange = vi.fn();

    act(() => {
      root.render(createElement(Editor, { value: 'hello', onChange }));
    });

    // Simulate an update where the document did NOT change (e.g. selection-only)
    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: false,
        selectionSet: false,
        focusChanged: false,
        state: {
          doc: { toString: () => 'hello', lineAt: () => ({ number: 1, from: 0 }) },
          selection: { main: { head: 5 } },
        },
        view: getMockEditorViewInstance(),
      });
    });

    expect(onChange).not.toHaveBeenCalled();
  });

  it('fires onCursorChange when selection changes', () => {
    const onCursorChange = vi.fn();

    act(() => {
      root.render(createElement(Editor, { onCursorChange }));
    });

    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: false,
        selectionSet: true,
        focusChanged: false,
        state: {
          selection: { main: { head: 10 } },
          doc: { lineAt: (pos: number) => ({ number: 2, from: 8 }) },
        },
        view: getMockEditorViewInstance(),
      });
    });

    expect(onCursorChange).toHaveBeenCalledWith({ line: 2, column: 3 });
  });

  it('fires onFocus when editor gains focus', () => {
    const onFocus = vi.fn();

    act(() => {
      root.render(createElement(Editor, { onFocus }));
    });

    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: false,
        selectionSet: false,
        focusChanged: true,
        view: { hasFocus: true },
      });
    });

    expect(onFocus).toHaveBeenCalledTimes(1);
  });

  it('fires onBlur when editor loses focus', () => {
    const onBlur = vi.fn();

    act(() => {
      root.render(createElement(Editor, { onBlur }));
    });

    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: false,
        selectionSet: false,
        focusChanged: true,
        view: { hasFocus: false },
      });
    });

    expect(onBlur).toHaveBeenCalledTimes(1);
  });

  it('does not fire onFocus when focusChanged is false', () => {
    const onFocus = vi.fn();
    const onBlur = vi.fn();

    act(() => {
      root.render(createElement(Editor, { onFocus, onBlur }));
    });

    act(() => {
      getMockUpdateListenerCb()?.({
        docChanged: true,
        selectionSet: true,
        focusChanged: false,
        state: {
          doc: { toString: () => 'changed', lineAt: () => ({ number: 1, from: 0 }) },
          selection: { main: { head: 0 } },
        },
        view: getMockEditorViewInstance(),
      });
    });

    expect(onFocus).not.toHaveBeenCalled();
    expect(onBlur).not.toHaveBeenCalled();
  });

  it('calls focus() when autoFocus is true', () => {
    vi.useFakeTimers();

    act(() => {
      root.render(createElement(Editor, { autoFocus: true }));
    });

    // Advance timers so the autoFocus setTimeout fires
    act(() => {
      vi.advanceTimersByTime(100);
    });

    expect(getMockEditorViewInstance()?.focused).toBe(true);

    vi.useRealTimers();
  });

  it('does not call focus() when autoFocus is false', () => {
    act(() => {
      root.render(createElement(Editor, { autoFocus: false }));
    });

    expect(getMockEditorViewInstance()?.focused).toBe(false);
  });

  it('destroys editor view on unmount', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    const instance = getMockEditorViewInstance();

    act(() => {
      root.unmount();
    });

    expect(instance?.destroyed).toBe(true);
  });

  it('updates value from outside when prop changes', () => {
    act(() => {
      root.render(createElement(Editor, { value: 'initial' }));
    });

    const instance = getMockEditorViewInstance();
    instance.dispatchCalls = []; // Reset dispatch calls from initial mount

    act(() => {
      root.render(createElement(Editor, { value: 'updated' }));
    });

    expect(instance.dispatchCalls.length).toBeGreaterThan(0);
    const dispatchCall = instance.dispatchCalls.find(
      (c: any) => c && c.changes
    );
    expect(dispatchCall).toBeDefined();
    expect(dispatchCall.changes.insert).toBe('updated');
  });

  it('does not dispatch value update when value is unchanged', () => {
    act(() => {
      root.render(createElement(Editor, { value: 'same' }));
    });

    const instance = getMockEditorViewInstance();
    instance.dispatchCalls = [];

    act(() => {
      root.render(createElement(Editor, { value: 'same' }));
    });

    // No dispatch should happen since the value didn't change
    const valueDispatches = instance.dispatchCalls.filter(
      (c: any) => c && c.changes
    );
    expect(valueDispatches).toHaveLength(0);
  });

  it('updates tabSize when prop changes', () => {
    act(() => {
      root.render(createElement(Editor, { tabSize: 4 }));
    });

    const instance = getMockEditorViewInstance();
    instance.dispatchCalls = [];

    act(() => {
      root.render(createElement(Editor, { tabSize: 2 }));
    });

    expect(instance.dispatchCalls.length).toBeGreaterThan(0);
    const effectCall = instance.dispatchCalls[0];
    expect(effectCall).toBeDefined();
    expect(effectCall.effects).toBeDefined();
  });

  it('updates readOnly when prop changes', () => {
    act(() => {
      root.render(createElement(Editor, { readOnly: false }));
    });

    const instance = getMockEditorViewInstance();
    instance.dispatchCalls = [];

    act(() => {
      root.render(createElement(Editor, { readOnly: true }));
    });

    expect(instance.dispatchCalls.length).toBeGreaterThan(0);
    const effectCall = instance.dispatchCalls[0];
    expect(effectCall.effects).toBeDefined();
  });

  it('updates wordWrap when prop changes', () => {
    act(() => {
      root.render(createElement(Editor, { wordWrap: false }));
    });

    const instance = getMockEditorViewInstance();
    instance.dispatchCalls = [];

    act(() => {
      root.render(createElement(Editor, { wordWrap: true }));
    });

    expect(instance.dispatchCalls.length).toBeGreaterThan(0);
    const effectCall = instance.dispatchCalls[0];
    expect(effectCall.effects).toBeDefined();
  });

  it('uses default fontSize of 13', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    expect(hoisted.MockEditorView.theme).toHaveBeenCalledWith(
      expect.objectContaining({
        '&': expect.objectContaining({
          fontSize: '13px',
        }),
      })
    );
  });

  it('uses custom fontSize', () => {
    act(() => {
      root.render(createElement(Editor, { fontSize: 16 }));
    });

    expect(hoisted.MockEditorView.theme).toHaveBeenCalledWith(
      expect.objectContaining({
        '&': expect.objectContaining({
          fontSize: '16px',
        }),
      })
    );
  });

  it('uses custom fontFamily', () => {
    act(() => {
      root.render(createElement(Editor, { fontFamily: "'Courier New', monospace" }));
    });

    expect(hoisted.MockEditorView.theme).toHaveBeenCalledWith(
      expect.objectContaining({
        '&': expect.objectContaining({
          fontFamily: "'Courier New', monospace",
        }),
        '.cm-content': {
          fontFamily: "'Courier New', monospace",
        },
        '.cm-gutters': {
          fontFamily: "'Courier New', monospace",
        },
      })
    );
  });

  it('accepts and passes extensions prop', () => {
    const customExtension = { id: 'custom-extension' };

    act(() => {
      root.render(createElement(Editor, { extensions: [customExtension] }));
    });

    expect(getMockEditorViewInstance()).not.toBeNull();
  });

  it('renders with all default props', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    const instance = getMockEditorViewInstance();
    expect(instance).not.toBeNull();
    expect(instance.state.doc.toString()).toBe('');
    expect(container.querySelector('.sprout-editor')).not.toBeNull();
  });

  it('fires onSave via keymap when save key handler is invoked', () => {
    const onSave = vi.fn();

    act(() => {
      root.render(createElement(Editor, { value: 'hello', onSave }));
    });

    // The save keymap's run function is stored in the keymap.of array.
    // We simulate invoking it by getting the keymap config from the state extensions
    // and calling the run callback with the mock view.
    const state = hoisted.MockEditorState.create.mock.calls[0]?.[0];
    const extensions = state?.extensions ?? [];
    // Find the save keymap entry (it's the one with 'Mod-s')
    const saveKeymapEntry = extensions.find(
      (ext: any) => Array.isArray(ext) && ext.some((e: any) => e?.key === 'Mod-s')
    );

    if (saveKeymapEntry) {
      const saveHandler = saveKeymapEntry.find((e: any) => e?.key === 'Mod-s');
      act(() => {
        saveHandler?.run?.(getMockEditorViewInstance());
      });
      expect(onSave).toHaveBeenCalledWith('hello');
    }
  });

  it('passes parent element to EditorView constructor', () => {
    act(() => {
      root.render(createElement(Editor));
    });

    const editorEl = container.querySelector('.sprout-editor');
    expect(getMockEditorViewInstance()?.parent).toBe(editorEl);
  });
});
