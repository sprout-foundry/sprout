// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import EditorPane from './EditorPane';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { ApiService } from '../services/api';
import { readFileWithConsent } from '../services/fileAccess';
import { copyToClipboard } from '../utils/clipboard';

// Import CodeMirror modules (resolved via mocks defined below).
// We need references to configure them in beforeEach because react-scripts
// sets resetMocks:true, which clears factory-configured implementations.
import { EditorState, Compartment } from '@codemirror/state';
import { EditorView as _EditorView } from '@codemirror/view';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: jest.fn(),
}));

jest.mock('../contexts/HotkeyContext', () => ({
  useHotkeys: jest.fn(),
}));

jest.mock('../contexts/ThemeContext', () => ({
  useTheme: jest.fn(),
}));

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: jest.fn(),
  },
}));

jest.mock('../services/fileAccess', () => ({
  readFileWithConsent: jest.fn(),
}));

jest.mock('../utils/clipboard', () => ({
  copyToClipboard: jest.fn(),
}));

jest.mock('./EditorToolbar', () => () => <div data-testid="editor-toolbar" />);
jest.mock('./ImageViewer', () => () => <div data-testid="image-viewer" />);
jest.mock('./SvgPreview', () => () => <div data-testid="svg-preview" />);
jest.mock('./GoToSymbolOverlay', () => {
  const MockComponent = () => null;
  MockComponent.getEnclosingSymbols = () => [];
  return MockComponent;
});
jest.mock('./LanguageSwitcher', () => {
  return function MockLanguageSwitcher(props: any) {
    return (
      <button
        data-testid="language-switcher"
        data-language-id={props.currentLanguageId ?? ''}
        data-auto-detected={props.isAutoDetected ? 'true' : 'false'}
        onClick={() => props.onLanguageChange('python')}
      >
        MockLanguageSwitcher
      </button>
    );
  };
});

// Must mock languageRegistry before EditorPane imports it — it pulls in
// heavy ESM @codemirror/lang-* and @codemirror/legacy-modes packages that
// Jest (27.x) cannot handle.
// NOTE: react-scripts sets resetMocks:true globally, which clears
// jest.fn() implementations before each test.  Use plain arrow functions
// for module-level mocks so they survive the reset.
jest.mock('../extensions/languageRegistry', () => {
  const entries = [
    { id: 'typescript', name: 'TypeScript', extensions: ['ts', 'tsx'] },
    { id: 'javascript', name: 'JavaScript', extensions: ['js', 'jsx'] },
    { id: 'python', name: 'Python', extensions: ['py'] },
    { id: 'css', name: 'CSS', extensions: ['css'] },
    { id: 'json', name: 'JSON', extensions: ['json'] },
  ];
  return {
    allLanguageEntries: entries,
    getLanguageExtensions: () => [],
    resolveLanguageId: (override, ext, _name) => {
      if (override) return { languageId: override, isAutoDetected: false };
      // Mimic real behaviour: return languageId + sub-variant for known extensions
      const extensionMap: Record<string, string> = {
        ts: 'typescript',
        tsx: 'typescript',
        js: 'javascript',
        jsx: 'javascript',
        py: 'python',
        css: 'css',
        json: 'json',
      };
      const base = extensionMap[ext];
      if (!base) return { languageId: null, isAutoDetected: false };
      // .tsx → typescript-jsx, .jsx → javascript-jsx
      const sub = ext === 'tsx' ? '-jsx' : ext === 'jsx' ? '-jsx' : '';
      return { languageId: base + sub, isAutoDetected: true };
    },
    detectLanguage: (ext) => {
      const map: Record<string, string> = {
        ts: 'typescript',
        tsx: 'typescript',
        js: 'javascript',
        jsx: 'javascript',
        py: 'python',
        css: 'css',
        json: 'json',
      };
      return map[ext] ?? null;
    },
  };
});

jest.mock('../extensions/diffGutter', () => ({
  diffGutter: () => [],
  updateDiffGutter: () => {},
  clearDiffGutter: () => {},
}));

jest.mock('../extensions/lintDiagnostics', () => ({
  lintDiagnostics: () => [],
  clearDiagnostics: () => {},
  createDebouncedDiagnosticsUpdater: () => ({ update: () => {}, cancel: () => {} }),
}));

jest.mock('../extensions/cursorHistory', () => ({
  cursorHistoryPlugin: () => [],
  navigateCursorBack: () => false,
  navigateCursorForward: () => false,
}));

jest.mock('../extensions/indentGuides', () => ({
  indentGuidesPlugin: () => [],
}));

jest.mock('../extensions/minimap', () => ({
  minimapExtension: () => [],
  showMinimap: { compute: () => null },
}));

// Mock CodeMirror packages — their ESM internals break Jest 27.
// Factories create stub jest.fn()s; the actual implementations are
// configured in beforeEach (after resetMocks runs).
jest.mock('@codemirror/view', () => ({
  EditorView: class MockEditorView {
    state: any;
    dom: any;
    constructor({ state, _parent }: any) {
      this.state = state;
      this.dom = { querySelector: () => null, classList: { add: () => {} } };
    }
    dispatch() {}
    focus() {}
    destroy() {}
    static lineWrapping: any = [];
    static theme = (spec: any) => spec;
    static updateListener: { of: (fn: any) => any } = { of: (fn: any) => fn };
    static baseTheme = (spec: any) => spec;
  },
  ViewPlugin: { fromClass: (cls: any) => cls },
  keymap: { of: (bindings: any[]) => bindings },
  KeyBinding: {} as any,
  lineNumbers: () => [],
  highlightSpecialChars: () => [],
  highlightActiveLine: () => [],
  rectangularSelection: () => [],
  crosshairCursor: () => [],
  Decoration: {
    mark: jest.fn(() => ({ range: jest.fn() })),
    set: jest.fn(),
    none: [],
    widget: jest.fn(),
  },
}));

jest.mock('@codemirror/state', () => {
  const mockCompartment = {
    of: jest.fn((ext: any) => ext),
    reconfigure: jest.fn((ext: any) => ({ reconfigure: ext })),
  };
  return {
    EditorState: {
      create: jest.fn(),
      allowMultipleSelections: { of: (v: any) => v },
    },
    Compartment: jest.fn(() => mockCompartment),
    Facet: {
      define: jest.fn(() => ({
        of: jest.fn((v: any) => ({ facetOf: v })),
      })),
    },
    EditorSelection: {
      create: jest.fn(),
      range: jest.fn(),
    },
  };
});

jest.mock('@codemirror/commands', () => ({
  defaultKeymap: [],
  indentWithTab: {},
  history: () => [],
}));

jest.mock('@codemirror/search', () => ({
  search: () => [],
  searchKeymap: [],
  openSearchPanel: jest.fn(),
  replaceAll: jest.fn(),
  selectNextOccurrence: jest.fn(),
  selectSelectionMatches: jest.fn(),
}));

jest.mock('@codemirror/autocomplete', () => ({
  autocompletion: () => [],
  closeBrackets: () => [],
  snippet: (template: string) => () => template,
  hasNextSnippetField: () => false,
  hasPrevSnippetField: () => false,
}));

jest.mock('@codemirror/language', () => ({
  syntaxHighlighting: (s: any) => s,
  defaultHighlightStyle: [],
  codeFolding: () => [],
  foldGutter: (_opts: any) => [],
  indentOnInput: () => [],
  bracketMatching: () => [],
  highlightSpecialChars: () => [],
  highlightActiveLine: () => [],
}));

jest.mock('@codemirror/theme-one-dark', () => ({
  oneDarkHighlightStyle: [],
}));

// ---------------------------------------------------------------------------
// Mock helpers for CodeMirror internals
// ---------------------------------------------------------------------------

/**
 * Build a lightweight mock for a CodeMirror Text-like doc object.
 * Not a jest.mock — just a plain helper so resetMocks won't touch it.
 */
function createMockDoc(text = '') {
  const lines = text.split('\n');
  const lineData = lines.map((l, i) => {
    return { number: i + 1, text: l, from: 0, to: 0, length: l.length };
  });
  let pos = 0;
  for (const ld of lineData) {
    ld.from = pos;
    ld.to = pos + ld.length;
    pos = ld.to + 1;
  }
  return {
    toString: () => text,
    length: text.length,
    lines: lines.length,
    lineAt: (p) => {
      for (let i = lineData.length - 1; i >= 0; i--) {
        if (p >= lineData[i].from) return lineData[i];
      }
      return lineData[0];
    },
    line: (n) => lineData[n - 1] || lineData[lineData.length - 1],
  };
}

/** Create a mock state object as EditorState.create would. */
function createMockState(text = '') {
  return {
    doc: createMockDoc(text),
    selection: { main: { head: 0 }, ranges: [{ from: 0, to: 0 }], mainIndex: 0 },
  };
}

// ---------------------------------------------------------------------------
// Constants & Helpers
// ---------------------------------------------------------------------------

const mockBuffer = {
  id: 'buffer-1',
  kind: 'file',
  file: {
    path: 'src/components/EditorPane.tsx',
    name: 'EditorPane.tsx',
    ext: '.tsx',
    isDir: false,
    size: 12345,
    modified: 0,
  },
  content: 'line1\nline2\nline3',
  originalContent: 'line1\nline2\nline3',
  cursorPosition: { line: 0, column: 0 },
  scrollPosition: { top: 0, left: 0 },
  isModified: false,
  isActive: true,
  isClosable: true,
  metadata: {},
};

const mockUseEditorManager = useEditorManager as jest.MockedFunction<typeof useEditorManager>;

const defaultMockEditorManager = {
  panes: [{ id: 'pane-1', bufferId: 'buffer-1', isActive: true }],
  buffers: new Map([['buffer-1', { ...mockBuffer }]]),
  updateBufferContent: jest.fn(),
  updateBufferCursor: jest.fn(),
  saveBuffer: jest.fn(),
  setBufferModified: jest.fn(),
  setBufferOriginalContent: jest.fn(),
  splitPane: jest.fn(),
  openWorkspaceBuffer: jest.fn(),
  setBufferLanguageOverride: jest.fn(),
};

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/**
 * Flush any pending requestAnimationFrame callbacks.
 * The shared ContextMenu component registers its close listeners (mousedown,
 * keydown, scroll, blur) inside a requestAnimationFrame callback, so after
 * opening the menu we must flush the RAF before those listeners are active.
 */
const flushRAF = () =>
  act(async () => {
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await Promise.resolve();
  });

function fireContextMenu(element: Element, x = 100, y = 100) {
  const event = new MouseEvent('contextmenu', {
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
  });
  element.dispatchEvent(event);
  return event;
}

/**
 * Helper to find the context menu. Since it's rendered via a portal to
 * document.body, we must query the body (not the container).
 */
function getMenu() {
  return document.body.querySelector('.context-menu');
}

function getMenuItems() {
  const menu = getMenu();
  return menu ? Array.from(menu.querySelectorAll('.context-menu-item')) : [];
}

// ---------------------------------------------------------------------------
// Shared test parent — provides beforeAll/beforeEach/afterEach for all
// EditorPane describe blocks.  Variables are scoped to this shared describe
// so nested describe blocks can reference them without re-declaration.
// ---------------------------------------------------------------------------

describe('EditorPane', () => {
  let container: HTMLDivElement;
  let root: any;
  let apiServiceMock: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // ── CodeMirror mock setup (resetMocks clears before each test) ──
    EditorState.create.mockImplementation(({ doc }) => createMockState(typeof doc === 'string' ? doc : ''));
    (Compartment as jest.Mock).mockImplementation(() => ({
      of: jest.fn().mockReturnValue([]),
      reconfigure: jest.fn().mockReturnValue({}),
    }));

    // ── copyToClipboard — tests assert on .toHaveBeenCalledWith ──
    (copyToClipboard as jest.Mock).mockResolvedValue(undefined);

    // ── Context & service mocks ──
    apiServiceMock = {
      getWorkspace: jest.fn().mockResolvedValue({
        workspace_root: '/home/user/project',
        daemon_root: '/home/user/project/.ledit',
      }),
      getGitDiff: jest.fn().mockResolvedValue({ diff: '' }),
    };
    (ApiService.getInstance as jest.Mock).mockReturnValue(apiServiceMock);

    mockUseEditorManager.mockReturnValue({ ...defaultMockEditorManager });

    (useHotkeys as jest.MockedFunction<typeof useHotkeys>).mockReturnValue({ hotkeys: [] });

    (useTheme as jest.MockedFunction<typeof useTheme>).mockReturnValue({
      theme: 'dark',
      themePack: { id: 'dark', mode: 'dark', editorSyntaxStyle: 'one-dark' },
      customHighlightStyle: undefined,
    });

    (readFileWithConsent as jest.Mock).mockResolvedValue({
      ok: true,
      statusText: 'OK',
      text: () => Promise.resolve('line1\nline2\nline3'),
    });
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    jest.clearAllMocks();
  });

  // ── Context menu tests ──

  describe('context menu', () => {
    it('renders without crashing', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();
      expect(container.querySelector('.editor-pane')).toBeTruthy();
    });

    it('context menu appears on right-click in the editor area', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      expect(paneContent).toBeTruthy();

      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const menu = getMenu();
      expect(menu).toBeTruthy();
    });

    it('context menu shows the three expected items when workspace root is available', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const items = getMenuItems();
      const texts = items.map((el) => el.textContent?.trim());

      expect(texts).toContain('Reveal in File Explorer');
      expect(texts).toContain('Copy relative path');
      expect(texts).toContain('Copy absolute path');
    });

    it('"Reveal in File Explorer" dispatches ledit:reveal-in-explorer event', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const listener = jest.fn();
      window.addEventListener('ledit:reveal-in-explorer', listener);

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const items = getMenuItems();
      expect(items[0]).toBeTruthy();

      await act(async () => {
        items[0].click();
      });
      await flushPromises();

      expect(listener).toHaveBeenCalledTimes(1);
      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          detail: { path: 'src/components/EditorPane.tsx' },
        }),
      );

      window.removeEventListener('ledit:reveal-in-explorer', listener);
    });

    it('"Copy relative path" calls copyToClipboard with the file path', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const items = getMenuItems();
      // Second item = "Copy relative path"
      expect(items[1]).toBeTruthy();

      await act(async () => {
        items[1].click();
      });
      await flushPromises();

      expect(copyToClipboard).toHaveBeenCalledWith('src/components/EditorPane.tsx');
    });

    it('"Copy absolute path" calls copyToClipboard with workspaceRoot + file path', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const items = getMenuItems();
      // Third item = "Copy absolute path"
      expect(items[2]).toBeTruthy();

      await act(async () => {
        items[2].click();
      });
      await flushPromises();

      expect(copyToClipboard).toHaveBeenCalledWith('/home/user/project/src/components/EditorPane.tsx');
    });

    it('context menu closes after clicking an item', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      expect(getMenu()).toBeTruthy();

      const items = getMenuItems();
      await act(async () => {
        items[0].click();
      });
      await flushPromises();

      expect(getMenu()).toBeFalsy();
    });

    it('"Copy absolute path" item is NOT present when workspace root is empty', async () => {
      apiServiceMock.getWorkspace.mockResolvedValueOnce({
        workspace_root: '',
        daemon_root: '/home/user/project/.ledit',
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      const texts = getMenuItems().map((el) => el.textContent?.trim());

      expect(texts).toContain('Reveal in File Explorer');
      expect(texts).toContain('Copy relative path');
      expect(texts).not.toContain('Copy absolute path');
    });

    it('context menu closes when clicking outside it', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      // Flush RAF so the menu registers its mousedown listener
      await flushRAF();

      expect(getMenu()).toBeTruthy();

      // Click outside the menu (on the body, not the menu itself)
      await act(async () => {
        document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      });
      await flushPromises();

      expect(getMenu()).toBeFalsy();
    });

    it('context menu closes when pressing Escape', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });

      // Flush RAF so the menu registers its keydown listener
      await flushRAF();

      expect(getMenu()).toBeTruthy();

      await act(async () => {
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      });
      await flushPromises();

      expect(getMenu()).toBeFalsy();
    });

    it('context menu does NOT appear when there is no buffer (empty state)', async () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        panes: [{ id: 'pane-1', bufferId: null, isActive: true }],
        buffers: new Map(),
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      // Should show empty state
      const noFileEl = container.querySelector('.no-file-selected');
      expect(noFileEl).toBeTruthy();
      // No pane-content should exist for empty state
      const paneContent = container.querySelector('.pane-content');
      expect(paneContent).toBeFalsy();
    });

    it('context menu does NOT appear for image files', async () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          [
            'buffer-1',
            {
              ...mockBuffer,
              file: { ...mockBuffer.file, ext: '.png', path: 'images/test.png', name: 'test.png' },
            },
          ],
        ]),
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      // Should show ImageViewer
      const imageView = container.querySelector('[data-testid="image-viewer"]');
      expect(imageView).toBeTruthy();
      // No pane-content div for images
      const paneContent = container.querySelector('.pane-content');
      expect(paneContent).toBeFalsy();
    });
  }); // context menu

  // ── Language override tests ──

  describe('language override', () => {
    it('renders the LanguageSwitcher in the toolbar zone', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher).toBeTruthy();
    });

    it('passes auto-detected language info when no override is set', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher?.getAttribute('data-language-id')).toBe('typescript-jsx');
      expect(languageSwitcher?.getAttribute('data-auto-detected')).toBe('true');
    });

    it('passes the language override when set', async () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([['buffer-1', { ...mockBuffer, languageOverride: 'python' }]]),
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher?.getAttribute('data-language-id')).toBe('python');
      expect(languageSwitcher?.getAttribute('data-auto-detected')).toBe('false');
    });

    it('calls setBufferLanguageOverride when language is changed from the switcher', async () => {
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');

      await act(async () => {
        (languageSwitcher as HTMLElement).click();
      });
      await flushPromises();

      expect(defaultMockEditorManager.setBufferLanguageOverride).toHaveBeenCalledWith(
        'buffer-1',
        'python', // The mock calls onLanguageChange('python') on click
      );
    });

    it('shows "Auto" when no language is detected for unknown extension', async () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          [
            'buffer-1',
            {
              ...mockBuffer,
              file: { ...mockBuffer.file, ext: '.xyz', name: 'file.xyz' },
            },
          ],
        ]),
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher?.getAttribute('data-language-id')).toBe('');
      expect(languageSwitcher?.getAttribute('data-auto-detected')).toBe('false');
    });

    it('does not render LanguageSwitcher in empty pane state', async () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        panes: [{ id: 'pane-1', bufferId: null, isActive: true }],
        buffers: new Map(),
      });

      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher).toBeFalsy();
    });
  }); // language override
}); // EditorPane
