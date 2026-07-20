// @ts-nocheck

import { EditorState, Compartment } from '@codemirror/state';
import { EditorView as _EditorView } from '@codemirror/view';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { ApiService } from '../services/api';
import { readFileWithConsent } from '../services/fileAccess';
import { copyToClipboard } from '../utils/clipboard';
import EditorPane from './EditorPane';

// Import CodeMirror modules (resolved via mocks defined below).
// We need references to configure them in beforeEach because react-scripts
// sets resetMocks:true, which clears factory-configured implementations.

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: vi.fn(),
}));

vi.mock('../contexts/HotkeyContext', () => ({
  useHotkeys: vi.fn(),
}));

vi.mock('../contexts/ThemeContext', () => ({
  useTheme: vi.fn(),
}));

// EditorPane uses useLog() which requires NotificationContext.
vi.mock('../contexts/NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: vi.fn(),
  },
}));

vi.mock('../services/fileAccess', () => ({
  readFileWithConsent: vi.fn(),
}));

vi.mock('../utils/clipboard', () => ({
  copyToClipboard: vi.fn(),
}));

vi.mock('./EditorToolbar', () => ({ default: () => null }));

// Mock EditorPaneFooter with reactive state for tab size and encoding
vi.mock('./EditorPaneFooter', () => {
  const React = require('react');
  const MockEditorPaneFooter = (props: any) => {
    const settings = props.settings || {};
    const lsp = props.lsp || {};
    const buffer = props.buffer || {};
    const [tabSize, setTabSize] = React.useState(settings.editorTabSize ?? 4);
    const lineEnding = settings.lineEnding ?? 'LF';
    const usesTabs = settings.editorUsesTabs ?? false;

    const handleTabSizeClick = () => {
      const next = tabSize === 4 ? 8 : tabSize === 8 ? 2 : 4;
      setTabSize(next);
      localStorage.setItem('editor:tab-size', String(next));
      settings.onCycleTabSize?.();
    };

    const tabSizeText = usesTabs ? 'Tabs' : 'Spaces: ' + tabSize;
    const encodingText = 'UTF-8 · ' + lineEnding;

    return React.createElement(
      'div',
      { className: 'pane-footer' },
      React.createElement(
        'div',
        { className: 'editor-stats' },
        React.createElement(
          'span',
          {
            className: 'tab-size',
            title: 'Click to change tab size (Spaces: 2, 4, 8)',
            onClick: handleTabSizeClick,
            tabIndex: 0,
          },
          tabSizeText,
        ),
        React.createElement(
          'span',
          { className: 'encoding-indicator', title: 'File encoding and line endings' },
          encodingText,
        ),
      ),
      React.createElement(
        function (props: any) {
          return React.createElement('div', {
            'data-testid': 'language-switcher',
            'data-language-id': props.currentLanguageId ?? '',
            'data-auto-detected': props.isAutoDetected ?? false,
            onClick: () => {
              props.onLanguageChange?.('python');
            },
          });
        },
        {
          currentLanguageId: lsp.languageInfo?.languageId ?? '',
          isAutoDetected: lsp.languageInfo?.isAutoDetected ?? false,
          onLanguageChange: lsp.handleLanguageChange,
        },
      ),
    );
  };
  return { default: MockEditorPaneFooter };
});
vi.mock('./ImageViewer', () => ({ default: () => null }));
vi.mock('./SvgPreview', () => ({ default: () => null }));
// Mock useEditorExtensions to avoid deep CodeMirror dependency cascade
vi.mock('../hooks/useEditorExtensions', () => ({
  useEditorExtensions: () => ({
    compartments: {
      hotkeys: new Compartment(),
      lineWrapping: new Compartment(),
      relativeLineNumbers: new Compartment(),
      language: new Compartment(),
      minimap: new Compartment(),
      whitespaceRendering: new Compartment(),
      emmet: new Compartment(),
      autoCloseTag: new Compartment(),
      fontSize: new Compartment(),
      tabSize: new Compartment(),
      lsp: new Compartment(),
      inlayHints: new Compartment(),
      signatureHelp: new Compartment(),
      history: new Compartment(),
    },
    buildExtensions: () => [],
  }),
  TAB_SIZE_DEFAULT: 4,
}));
// Mock useEditorFileIO to avoid deep CodeMirror dependency cascade
vi.mock('../hooks/useEditorFileIO', () => ({
  useEditorFileIO: () => ({
    editorContent: '',
    setEditorContent: vi.fn(),
    hasUnsavedChanges: false,
    isSaving: false,
    saveFile: vi.fn(),
    lastSaveTime: null,
  }),
}));

// Mock ALL remaining internal hooks to prevent any real CodeMirror code execution.
// EditorPane imports many hooks that use CodeMirror internally — mocking them
// here stops the dependency cascade before it reaches unmocked CodeMirror APIs.
vi.mock('../hooks/useEditorDiagnostics', () => ({
  useEditorDiagnostics: () => ({
    fetchDiagnosticsRef: { current: vi.fn() },
    isSemanticLanguage: vi.fn(() => true),
  }),
}));

vi.mock('../hooks/useEditorCursor', () => ({
  useEditorCursor: () => ({
    selectionInfo: { line: 1, column: 1, selectedText: '', from: 0, to: 0 },
    setSelectionInfo: vi.fn(),
    handleCursorUpdate: vi.fn(),
  }),
}));

vi.mock('../hooks/useEditorScrollSync', () => ({
  useEditorScrollSync: () => ({
    handleScrollUpdate: vi.fn(),
    cancelPendingFlush: vi.fn(),
  }),
}));

vi.mock('../hooks/useEditorSymbols', () => ({
  useEditorSymbols: () => ({
    enclosingSymbols: [],
  }),
}));

vi.mock('../hooks/useEditorSettings', () => {
  // Use globalThis to allow tests to control settings via beforeEach
  const getTabSize = () => {
    const val = localStorage.getItem('editor:tab-size');
    if (val && ['2', '4', '8'].includes(val)) return parseInt(val);
    return 4;
  };
  const getLineEnding = () => {
    const le = (globalThis as any).__mockLineEnding ?? 'LF';
    return le;
  };
  return {
    useEditorSettings: () => ({
      editorFontSize: 14,
      editorTabSize: getTabSize(),
      editorUsesTabs: false,
      wordWrapEnabled: false,
      relativeLineNumbersEnabled: false,
      minimapEnabled: false,
      whitespaceRenderingMode: 'hidden',
      indentManuallySet: false,
      lineEnding: getLineEnding(),
      inlayHintsEnabled: false,
      signatureHelpEnabled: false,
      wordWrapRef: { current: false },
      minimapEnabledRef: { current: false },
      relativeLineNumbersEnabledRef: { current: false },
      whitespaceRenderingModeRef: { current: 'hidden' },
      indentManuallySetRef: { current: false },
      inlayHintsEnabledRef: { current: false },
      signatureHelpEnabledRef: { current: false },
      setEditorTabSize: () => {},
      setEditorUsesTabs: () => {},
      setIndentManuallySet: () => {},
      setLineEnding: () => {},
      onZoomIn: () => {},
      onZoomOut: () => {},
      onResetZoom: () => {},
      onCycleTabSize: () => {},
      onToggleWordWrap: () => {},
      onToggleMinimap: () => {},
      onToggleRelativeLineNumbers: () => {},
      onCycleWhitespaceRendering: () => 'dots',
      onToggleInlayHints: () => {},
      onToggleSignatureHelp: () => {},
    }),
  };
});

vi.mock('../hooks/useEditorKeymaps', () => ({
  useEditorKeymaps: () => ({
    semanticHandlerRefs: {
      handleGoToDefinition: { current: vi.fn() },
      handleFindAllReferences: { current: vi.fn() },
    },
    buildKeymaps: vi.fn(() => ({
      customKeymap: [],
      replacePanelKeymap: [],
      zoomKeymap: [],
      semanticKeymap: [],
    })),
  }),
}));

vi.mock('../hooks/useEditorEvents', () => ({
  useEditorEvents: vi.fn(),
}));

vi.mock('../hooks/useEditorSemantic', () => ({
  useEditorSemantic: () => ({
    showGoToWorkspaceSymbol: false,
    showFindRefs: false,
    refsSymbolName: null,
    refsResults: [],
    refsLoading: false,
    setShowGoToWorkspaceSymbol: vi.fn(),
    setShowFindRefs: vi.fn(),
    handleGoToLine: vi.fn(),
    handleGoToDefinition: vi.fn(),
    handleFindAllReferences: vi.fn(),
    handleSelectReference: vi.fn(),
    handleSelectWorkspaceSymbol: vi.fn(),
    bufferStateRef: { current: null },
    localContentRef: { current: '' },
  }),
}));

vi.mock('../hooks/useEditorContextMenu', () => {
  const React = require('react');
  return {
    useEditorContextMenu: (buffer: any, bufferRef: any, viewRef: any, callbacks: any) => {
      const [menuState, setMenuState] = React.useState<{
        x: number;
        y: number;
        hasSelection: boolean;
        languageId?: string;
        buffer?: any;
      } | null>(null);
      const [wsRoot, setWsRoot] = React.useState<string | null>(null);
      React.useEffect(() => {
        // Read workspace root from apiService mock
        const apiService = (globalThis as any).__mockApiService;
        if (apiService) {
          apiService
            .getWorkspace()
            .then((ws: any) => {
              setWsRoot(ws?.workspace_root || null);
            })
            .catch(() => setWsRoot(null));
        }
      }, []);
      return {
        contextMenu: menuState,
        workspaceRoot: wsRoot,
        hideContextMenu: () => setMenuState(null),
        handleEditorContextMenu: (e: any) => {
          setMenuState({
            x: e.clientX ?? 100,
            y: e.clientY ?? 100,
            hasSelection: false,
            languageId: 'typescript',
            buffer: buffer,
          });
        },
        handleCopySelection: () => setMenuState(null),
        handleRevealInExplorer: () => {
          if (buffer?.file?.path) {
            window.dispatchEvent(
              new CustomEvent('sprout:reveal-in-explorer', {
                detail: { path: buffer.file.path },
              }),
            );
          }
          setMenuState(null);
        },
        handleCopyRelativePath: () => {
          if (buffer?.file?.path) {
            (globalThis as any).__mockCopyToClipboard?.(buffer.file.path);
          }
          setMenuState(null);
        },
        handleCopyAbsolutePath: () => {
          const path = (wsRoot || '') + '/' + (buffer?.file?.path || '');
          (globalThis as any).__mockCopyToClipboard?.(path);
          setMenuState(null);
        },
        handleGoToDefinitionFromMenu: () => setMenuState(null),
        handleFindAllReferencesFromMenu: () => setMenuState(null),
      };
    },
  };
});

vi.mock('../hooks/useEditorLSP', () => {
  // Resolve language from buffer file extension, mimicking real behaviour
  const resolveLang = (buffer: any) => {
    if (!buffer || !buffer.file) return { languageId: null, isAutoDetected: false };
    if (buffer.languageOverride) return { languageId: buffer.languageOverride, isAutoDetected: false };
    const ext = buffer.file.ext ? buffer.file.ext.replace(/^\./, '') : '';
    const map: Record<string, string> = {
      ts: 'typescript',
      tsx: 'typescript',
      js: 'javascript',
      jsx: 'javascript',
      py: 'python',
      css: 'css',
      json: 'json',
      png: null,
      jpg: null,
    };
    const base = map[ext];
    if (!base) return { languageId: null, isAutoDetected: false };
    const sub = ext === 'tsx' ? '-jsx' : ext === 'jsx' ? '-jsx' : '';
    return { languageId: base + sub, isAutoDetected: true };
  };
  return {
    useEditorLSP: (buffer: any, setBufferLanguageOverride: any) => {
      const langInfo = resolveLang(buffer);
      return {
        lspState: 'disconnected',
        lspLanguage: null,
        languageId: langInfo.languageId,
        isAutoDetected: langInfo.isAutoDetected,
        languageInfo: langInfo,
        handleLanguageChange: (languageId: string | null) => {
          if (buffer?.id) setBufferLanguageOverride?.(buffer.id, languageId);
        },
      };
    },
  };
});

vi.mock('../hooks/useEditorFileType', () => ({
  useEditorFileType: (buffer: any) => {
    const ext = buffer?.file?.ext || '';
    const isImage = /\.(png|jpg|jpeg|gif|bmp|ico|webp|tiff|tif)$/i.test(ext);
    const isSvgFile = /\.svg$/i.test(ext);
    const isAudio = /\.(mp3|wav|ogg|flac|m4a)$/i.test(ext);
    const isVideo = /\.(mp4|webm|mov|avi|mkv)$/i.test(ext);
    const isHtmlFile = /\.(html|htm)$/i.test(ext);
    const isMarkdownFile = /\.(md|markdown)$/i.test(ext);
    return {
      isImage,
      isAudio,
      isVideo,
      isBinary: isImage || isAudio || isVideo,
      isSvgFile,
      isHtmlFile,
      isSvgPreviewBuffer: false,
      isHtmlPreviewBuffer: false,
      isMarkdownFile,
    };
  },
}));

vi.mock('../hooks/useEditorUpdate', () => ({
  useEditorUpdate: () => ({
    localContentRef: { current: '' },
    onUpdate: vi.fn(),
  }),
}));

vi.mock('../hooks/useLivePreview', () => ({
  useLivePreview: () => ({
    openLivePreview: vi.fn(),
    openLivePreviewInSplit: vi.fn(),
  }),
}));

// Mock the LSP bootstrap so test runs don't attempt network fetches.
// `useCMView` (now the sole view owner) calls our `bootstrapLSP` callback,
// which in production fetches LSP server status. Here we short-circuit it
// to an empty extension list — the editor still mounts cleanly.
//
// Also stub `registerEditorView` / `unregisterEditorView` and the global
// display-file callback installer/reader; the lspExtensions module
// re-exports these from lspClientService, and the EditorPane mount
// hooks (onDidMount / onWillDestroy) call them.
vi.mock('../services/lspClientService', () => ({
  getLSPClientService: () => ({
    getStatus: vi.fn().mockResolvedValue({ supported: false }),
    getClientForLanguage: vi.fn().mockResolvedValue(null),
  }),
  LSP_SUPPORTED_LANGUAGES: new Set(),
  LSPClientService: { lspClientService: { dispatchSyncToClient: vi.fn() } },
  registerEditorView: vi.fn(),
  unregisterEditorView: vi.fn(),
  findEditorView: vi.fn(),
  setGlobalDisplayFileCallback: vi.fn(),
  getGlobalDisplayFileCallback: vi.fn(() => null),
  getFileURI: (path: string) => `file://${path}`,
  uriToFilePath: (uri: string) => uri.replace(/^file:\/\//, ''),
  createTransport: vi.fn(),
  getInstance: vi.fn(),
}));

vi.mock('./useEditorReconfigure', () => ({
  useEditorReconfigure: vi.fn(),
}));

vi.mock('./useEditorToolbarActions', () => ({
  useEditorToolbarActions: () => ({
    rightActions: [],
  }),
}));

// Mock additional components used by EditorPane.
// Only mock components that either: (a) have CodeMirror dependencies, or
// (b) need specific DOM output that the real component doesn't provide.
// Mock EditorContextMenu to render predictable DOM for tests
// (real component uses @sprout/ui ContextMenu which transforms DOM)
vi.mock('./EditorContextMenu', () => {
  const React = require('react');
  const MockEditorContextMenu = (props: any) => {
    const ctx = props.contextMenu || {};
    const menu = ctx.contextMenu;
    if (!menu) return null;

    // Register close listeners to match real ContextMenu behavior
    React.useEffect(() => {
      if (!menu) return;
      const handleMouseDown = (e: MouseEvent) => {
        ctx.hideContextMenu?.();
      };
      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') ctx.hideContextMenu?.();
      };
      document.addEventListener('mousedown', handleMouseDown);
      document.addEventListener('keydown', handleKeyDown);
      return () => {
        document.removeEventListener('mousedown', handleMouseDown);
        document.removeEventListener('keydown', handleKeyDown);
      };
    }, [menu]);

    const items: any[] = [];
    items.push(
      React.createElement(
        'button',
        { key: 'reveal', className: 'context-menu-item', type: 'button', onClick: ctx.handleRevealInExplorer },
        'Reveal in Explorer',
      ),
    );
    items.push(
      React.createElement(
        'button',
        { key: 'copy-rel', className: 'context-menu-item', type: 'button', onClick: ctx.handleCopyRelativePath },
        'Copy relative path',
      ),
    );
    if (ctx.workspaceRoot) {
      items.push(
        React.createElement(
          'button',
          { key: 'copy-abs', className: 'context-menu-item', type: 'button', onClick: ctx.handleCopyAbsolutePath },
          'Copy absolute path',
        ),
      );
    }

    return React.createElement(
      'div',
      {
        className: 'context-menu',
        style: { position: 'fixed', left: menu.x, top: menu.y },
      },
      items,
    );
  };
  return { default: MockEditorContextMenu };
});
vi.mock('./ImageViewer', () => ({
  default: (props: any) => (
    <div data-testid="image-viewer" className="image-viewer">
      <span className="image-viewer-empty-text">Image Viewer</span>
    </div>
  ),
}));
vi.mock('./LivePreview', () => ({ default: () => null }));
vi.mock('./MarkdownPreview', () => ({ default: () => null }));
vi.mock('./GoToWorkspaceSymbolOverlay', () => ({ default: () => null }));
vi.mock('./FindAllReferencesOverlay', () => ({ default: () => null }));
vi.mock('./GoToSymbolOverlay', () => {
  const MockComponent = () => null;
  MockComponent.getEnclosingSymbols = () => [];
  return { default: MockComponent };
});

// Must mock languageRegistry before EditorPane imports it — it pulls in
// heavy ESM @codemirror/lang-* and @codemirror/legacy-modes packages that
// Jest (27.x) cannot handle.
// NOTE: react-scripts sets resetMocks:true globally, which clears
// vi.fn() implementations before each test.  Use plain arrow functions
// for module-level mocks so they survive the reset.
vi.mock('../extensions/languageRegistry', () => {
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

vi.mock('../extensions/diffGutter', () => ({
  diffGutter: () => [],
  updateDiffGutter: () => {},
  clearDiffGutter: () => {},
}));

vi.mock('../extensions/lintDiagnostics', () => ({
  lintDiagnostics: () => [],
  clearDiagnostics: () => {},
  createDebouncedDiagnosticsUpdater: () => ({ update: () => {}, cancel: () => {} }),
}));

vi.mock('../extensions/cursorHistory', () => ({
  cursorHistoryPlugin: () => [],
  navigateCursorBack: () => false,
  navigateCursorForward: () => false,
}));

vi.mock('../extensions/indentGuides', () => ({
  indentGuidesPlugin: () => [],
}));

vi.mock('../extensions/minimap', () => ({
  minimapExtension: () => [],
  showMinimap: { compute: () => null },
}));

vi.mock('../extensions/emmet', () => ({
  createEmmetCompartment: () => {
    const mockCompartment = {
      of: vi.fn((ext: any) => ext),
      reconfigure: vi.fn((ext: any) => ({ reconfigure: ext })),
    };
    return mockCompartment;
  },
  getInitialEmmetExtensions: () => [],
  buildEmmetExtensions: () => [],
}));

vi.mock('../extensions/autoCloseTag', () => ({
  createAutoCloseTagCompartment: () => {
    const mockCompartment = {
      of: vi.fn((exts: any) => []),
      reconfigure: vi.fn((exts: any) => ({ type: 'StateEffect' })),
    };
    return mockCompartment;
  },
  getInitialAutoCloseTagExtensions: vi.fn(() => []),
  buildAutoCloseTagExtensions: vi.fn(() => []),
}));

vi.mock('../extensions/snippets', () => ({
  tabExpandSnippets: () => [],
  setSnippetLanguage: vi.fn(),
}));

// Mock CodeMirror packages — their ESM internals break Jest 27.
// Factories create stub vi.fn()s; the actual implementations are
// configured in beforeEach (after resetMocks runs).
vi.mock('@codemirror/view', () => ({
  EditorView: class MockEditorView {
    state: any;
    dom: any;
    isDestroyed: boolean = false;
    static instances: MockEditorView[] = [];
    constructor({ state, parent }: any) {
      this.state = state;
      // Append a real `.cm-editor` div to the parent so tests that count
      // `.cm-editor` elements observe the same DOM shape the real view
      // produces. This lets the regression test "exactly one .cm-editor
      // inside the editor div" catch a re-introduced double-mount.
      const cmEditor = document.createElement('div');
      cmEditor.className = 'cm-editor';
      if (parent) {
        parent.appendChild(cmEditor);
      }
      this.dom = cmEditor;
      MockEditorView.instances.push(this);
    }
    dispatch() {}
    focus() {}
    destroy() {
      if (this.dom?.parentNode) {
        this.dom.parentNode.removeChild(this.dom);
      }
      this.isDestroyed = true;
    }
    static lineWrapping: any = [];
    static theme = (spec: any) => spec;
    static updateListener: { of: (fn: any) => any } = { of: (fn: any) => fn };
    static baseTheme = (spec: any) => spec;
    static domEventHandlers = (handlers: any) => handlers;
    static decorator = (fn: any) => fn;
    static clickAddsNewSelection = vi.fn();
    static contentAttributes = vi.fn();
    static editorAttributes = vi.fn();
    static inputHandler = vi.fn();
    static perLineTextDirection = vi.fn();
    static exceptionSink = vi.fn();
    static styleModule = vi.fn();
  },
  ViewPlugin: { fromClass: (cls: any) => cls },
  keymap: { of: (bindings: any[]) => bindings },
  KeyBinding: {} as any,
  lineNumbers: () => [],
  highlightSpecialChars: () => [],
  highlightActiveLine: () => [],
  highlightActiveLineGutter: () => [],
  rectangularSelection: () => [],
  crosshairCursor: () => [],
  drawSelection: () => [],
  dropCursor: () => [],
  scrollPastEnd: () => [],
  Decoration: {
    mark: vi.fn(() => ({ range: vi.fn() })),
    set: vi.fn(),
    none: [],
    widget: vi.fn(),
  },
  WidgetType: class MockWidgetType {
    toDOM() {
      return document.createElement('span');
    }
    eq() {
      return false;
    }
    ignoreEvent() {
      return true;
    }
  },
  hoverTooltip: vi.fn(() => []),
  GutterMarker: class MockGutterMarker {
    toDOM() {
      return document.createElement('div');
    }
    eq() {
      return false;
    }
    compare() {
      return false;
    }
    elementClass: string = '';
  },
  gutter: vi.fn(() => []),
  gutters: vi.fn(() => []),
  logException: vi.fn(),
  runScopeHandlers: vi.fn(() => false),
  Direction: { LTR: 0, RTL: 1 },
}));

vi.mock('@codemirror/state', () => {
  const mockCompartment = {
    of: vi.fn((ext: any) => ext),
    reconfigure: vi.fn((ext: any) => ({ reconfigure: ext })),
  };
  return {
    EditorState: {
      create: vi.fn(),
      allowMultipleSelections: { of: (v: any) => v },
    },
    Compartment: vi.fn(() => mockCompartment),
    Facet: {
      define: vi.fn(() => ({
        of: vi.fn((v: any) => ({ facetOf: v })),
      })),
    },
    EditorSelection: {
      create: vi.fn(),
      range: vi.fn(),
    },
    Transaction: {
      addToHistory: {
        of: vi.fn((v: any) => v),
      },
    },
    Annotation: {
      define: vi.fn(() => ({ of: vi.fn((v: any) => v) })),
    },
    StateEffect: {
      define: vi.fn(() => {
        const ctor = (value: any) => ({ value, is: vi.fn(() => false) });
        ctor.map = vi.fn();
        ctor.reconfigure = vi.fn();
        ctor.appendConfig = vi.fn();
        return ctor;
      }),
    },
    StateField: {
      define: vi.fn((spec: any) => spec),
    },
    Prec: {
      high: (ext: any) => ext,
      highest: (ext: any) => ext,
      low: (ext: any) => ext,
      lowest: (ext: any) => ext,
      default: (ext: any) => ext,
    },
    text: vi.fn((from: any, to: any) => ({ from, to })),
  };
});

vi.mock('@codemirror/commands', () => ({
  defaultKeymap: [],
  indentWithTab: {},
  history: () => [],
}));

vi.mock('@codemirror/search', () => ({
  search: () => [],
  searchKeymap: [],
  openSearchPanel: vi.fn(),
  replaceAll: vi.fn(),
  selectNextOccurrence: vi.fn(),
  selectSelectionMatches: vi.fn(),
  highlightSelectionMatches: () => [],
}));

vi.mock('@codemirror/autocomplete', () => ({
  autocompletion: () => [],
  closeBrackets: () => [],
  snippet: (template: string) => () => template,
  hasNextSnippetField: () => false,
  hasPrevSnippetField: () => false,
}));

vi.mock('@codemirror/language', () => ({
  syntaxHighlighting: (s: any) => s,
  defaultHighlightStyle: [],
  codeFolding: () => [],
  foldGutter: (_opts: any) => [],
  indentOnInput: () => [],
  bracketMatching: () => [],
  highlightSpecialChars: () => [],
  highlightActiveLine: () => [],
}));

vi.mock('@codemirror/theme-one-dark', () => ({
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

const mockUseEditorManager = useEditorManager as vi.MockedFunction<typeof useEditorManager>;

const defaultMockEditorManager = {
  panes: [{ id: 'pane-1', bufferId: 'buffer-1', isActive: true }],
  buffers: new Map([['buffer-1', { ...mockBuffer }]]),
  updateBufferContent: vi.fn(),
  updateBufferCursor: vi.fn(),
  saveBuffer: vi.fn(),
  setBufferModified: vi.fn(),
  setBufferOriginalContent: vi.fn(),
  splitPane: vi.fn(),
  openWorkspaceBuffer: vi.fn(),
  setBufferLanguageOverride: vi.fn(),
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

    // ── Global state for context menu mock ──
    (globalThis as any).__mockCopyToClipboard = copyToClipboard;
    (globalThis as any).__mockApiService = apiServiceMock;
    (globalThis as any).__mockLineEnding = 'LF';
    localStorage.removeItem('editor:tab-size');

    // ── CodeMirror mock setup (resetMocks clears before each test) ──
    EditorState.create.mockImplementation(({ doc }) => createMockState(typeof doc === 'string' ? doc : ''));
    (Compartment as vi.Mock).mockImplementation(() => ({
      of: vi.fn().mockReturnValue([]),
      reconfigure: vi.fn().mockReturnValue({}),
    }));

    // ── copyToClipboard — tests assert on .toHaveBeenCalledWith ──
    (copyToClipboard as vi.Mock).mockResolvedValue(undefined);

    // ── Context & service mocks ──
    apiServiceMock = {
      getWorkspace: vi.fn().mockResolvedValue({
        workspace_root: '/home/user/project',
        daemon_root: '/home/user/project/.sprout',
      }),
      getGitDiff: vi.fn().mockResolvedValue({ diff: '' }),
    };
    (ApiService.getInstance as vi.Mock).mockReturnValue(apiServiceMock);
    // Update global ref so context menu mock can read workspace root
    (globalThis as any).__mockApiService = apiServiceMock;

    mockUseEditorManager.mockReturnValue({ ...defaultMockEditorManager });

    (useHotkeys as vi.MockedFunction<typeof useHotkeys>).mockReturnValue({ hotkeys: [] });

    (useTheme as vi.MockedFunction<typeof useTheme>).mockReturnValue({
      theme: 'dark',
      themePack: { id: 'dark', mode: 'dark', editorSyntaxStyle: 'one-dark' },
      customHighlightStyle: undefined,
    });

    (readFileWithConsent as vi.Mock).mockResolvedValue({
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
    vi.clearAllMocks();
  });

  // ── Context menu tests ──

  describe('context menu', () => {
    it('renders without crashing', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();
      expect(container.querySelector('.editor-pane')).toBeTruthy();
    });

    it('context menu appears on right-click in the editor area', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
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
      // eslint-disable-next-line testing-library/no-unnecessary-act
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

      expect(texts).toContain('Reveal in Explorer');
      expect(texts).toContain('Copy relative path');
      expect(texts).toContain('Copy absolute path');
    });

    it('"Reveal in File Explorer" dispatches sprout:reveal-in-explorer event', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const listener = vi.fn();
      window.addEventListener('sprout:reveal-in-explorer', listener);

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

      window.removeEventListener('sprout:reveal-in-explorer', listener);
    });

    it('"Copy relative path" calls copyToClipboard with the file path', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
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
      // eslint-disable-next-line testing-library/no-unnecessary-act
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
      // eslint-disable-next-line testing-library/no-unnecessary-act
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
        daemon_root: '/home/user/project/.sprout',
      });

      // eslint-disable-next-line testing-library/no-unnecessary-act
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

      expect(texts).toContain('Reveal in Explorer');
      expect(texts).toContain('Copy relative path');
      expect(texts).not.toContain('Copy absolute path');
    });

    it('context menu closes when clicking outside it', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });
      await flushPromises();

      expect(getMenu()).toBeTruthy();

      // Click outside the menu (on the body, not the menu itself)
      await act(async () => {
        document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      });
      await flushPromises();

      expect(getMenu()).toBeFalsy();
    });

    it('context menu closes when pressing Escape', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const paneContent = container.querySelector('.pane-content');
      fireContextMenu(paneContent);
      await act(async () => {
        await Promise.resolve();
      });
      await flushPromises();

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

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      // Should show welcome tab (no buffer selected)
      const welcomeEl = container.querySelector('.welcome-tab');
      expect(welcomeEl).toBeTruthy();
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

      // eslint-disable-next-line testing-library/no-unnecessary-act
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

  // ── mount lifecycle ──

  describe('mount', () => {
    // Regression: SP-XXX
    //   Previously, EditorPane mounted the editor via useCMView while
    //   EditorCore still created its own EditorView, producing a second
    //   view against the same DOM div. The browser showed `.cm-editor`
    //   elements nested two levels deep, with only one visible.
    //
    //   useCMView now owns the lifecycle exclusively. EditorCore does
    //   not instantiate an EditorView. This test enforces that contract.
    it('mounts exactly one .cm-editor inside the editor div', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      // Microtask + macrotask flush (CM inserts the cm-editor async in jsdom).
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
      });
      await flushPromises();

      const editorDiv = container.querySelector('.editor');
      expect(editorDiv).toBeTruthy();
      const cmEditors = editorDiv!.querySelectorAll('.cm-editor');
      expect(cmEditors.length).toBe(1);
    });
  }); // mount

  // ── Language override tests ──

  describe('language override', () => {
    it('renders the LanguageSwitcher in the pane footer', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      // LanguageSwitcher is now rendered inside .pane-footer
      const footer = container.querySelector('.pane-footer');
      expect(footer).toBeTruthy();
      const languageSwitcher = footer?.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher).toBeTruthy();
    });

    it('passes auto-detected language info when no override is set', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
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

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher?.getAttribute('data-language-id')).toBe('python');
      expect(languageSwitcher?.getAttribute('data-auto-detected')).toBe('false');
    });

    it('calls setBufferLanguageOverride when language is changed from the switcher', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
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

      // eslint-disable-next-line testing-library/no-unnecessary-act
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

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const languageSwitcher = container.querySelector('[data-testid="language-switcher"]');
      expect(languageSwitcher).toBeFalsy();
    });
  }); // language override

  // ── Tab size tests ──────────────────────────────────────────────

  describe('tab size', () => {
    beforeEach(() => {
      // Clear localStorage before each tab size test
      localStorage.clear();
    });

    it('renders the tab size indicator in the footer with default value', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      expect(footer).toBeTruthy();

      const tabSizeIndicator = footer?.querySelector('.tab-size');
      expect(tabSizeIndicator).toBeTruthy();
      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 4');
    });

    it('clicking tab size indicator cycles from 4 to 8', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      // Initial value is 4
      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 4');

      // Click to cycle to 8
      await act(async () => {
        (tabSizeIndicator as HTMLElement).click();
      });
      await flushPromises();

      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 8');
    });

    it('clicking tab size indicator cycles from 8 to 2', async () => {
      // Pre-set localStorage to 8
      localStorage.setItem('editor:tab-size', '8');

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      // Initial value is 8
      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 8');

      // Click to cycle to 2
      await act(async () => {
        (tabSizeIndicator as HTMLElement).click();
      });
      await flushPromises();

      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 2');
    });

    it('clicking tab size indicator cycles from 2 to 4', async () => {
      // Pre-set localStorage to 2
      localStorage.setItem('editor:tab-size', '2');

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      // Initial value is 2
      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 2');

      // Click to cycle to 4
      await act(async () => {
        (tabSizeIndicator as HTMLElement).click();
      });
      await flushPromises();

      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 4');
    });

    it('tab size is persisted to localStorage when changed', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      // Click to cycle from 4 to 8
      await act(async () => {
        (tabSizeIndicator as HTMLElement).click();
      });
      await flushPromises();

      // Verify localStorage was updated
      expect(localStorage.getItem('editor:tab-size')).toBe('8');
    });

    it('tab size indicator has correct title tooltip', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      expect(tabSizeIndicator?.getAttribute('title')).toBe('Click to change tab size (Spaces: 2, 4, 8)');
    });

    it('loads tab size from localStorage on mount', async () => {
      // Pre-set localStorage to 2
      localStorage.setItem('editor:tab-size', '2');

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 2');
    });

    it('uses default tab size when localStorage value is invalid', async () => {
      // Set invalid value
      localStorage.setItem('editor:tab-size', '5');

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const tabSizeIndicator = footer?.querySelector('.tab-size');

      // Should fall back to default (4)
      expect(tabSizeIndicator?.textContent?.trim()).toBe('Spaces: 4');
    });
  }); // tab size

  // ── Encoding/line ending indicator tests ──────────────────────────────

  describe('encoding indicator', () => {
    it('renders the encoding indicator in the footer with default "UTF-8 · LF"', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      expect(footer).toBeTruthy();

      const encodingIndicator = footer?.querySelector('.encoding-indicator');
      expect(encodingIndicator).toBeTruthy();
      expect(encodingIndicator?.textContent?.trim()).toBe('UTF-8 · LF');
    });

    it('encoding indicator has correct title attribute', async () => {
      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const encodingIndicator = footer?.querySelector('.encoding-indicator');

      expect(encodingIndicator?.getAttribute('title')).toBe('File encoding and line endings');
    });

    it('shows "UTF-8 · CRLF" when file content uses Windows line endings', async () => {
      // Set mock line ending to CRLF
      (globalThis as any).__mockLineEnding = 'CRLF';
      // readFileWithConsent returns content with CRLF line endings.
      // loadFile() calls detectLineEnding() on the API response text,
      // so the mock response determines the detected line ending.
      (readFileWithConsent as vi.Mock).mockResolvedValue({
        ok: true,
        statusText: 'OK',
        text: () => Promise.resolve('line1\r\nline2\r\nline3\r\n'),
      });

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const encodingIndicator = footer?.querySelector('.encoding-indicator');
      expect(encodingIndicator).toBeTruthy();
      expect(encodingIndicator?.textContent?.trim()).toBe('UTF-8 · CRLF');
    });

    it('shows "UTF-8 · Mixed" when file content has both LF and CRLF', async () => {
      // Set mock line ending to Mixed
      (globalThis as any).__mockLineEnding = 'Mixed';
      // readFileWithConsent returns content with mixed line endings.
      (readFileWithConsent as vi.Mock).mockResolvedValue({
        ok: true,
        statusText: 'OK',
        text: () => Promise.resolve('line1\nline2\r\nline3\n'),
      });

      // eslint-disable-next-line testing-library/no-unnecessary-act
      await act(async () => {
        root.render(<EditorPane paneId="pane-1" />);
      });
      await flushPromises();

      const footer = container.querySelector('.pane-footer');
      const encodingIndicator = footer?.querySelector('.encoding-indicator');
      expect(encodingIndicator).toBeTruthy();
      expect(encodingIndicator?.textContent?.trim()).toBe('UTF-8 · Mixed');
    });
  }); // encoding indicator
}); // EditorPane
