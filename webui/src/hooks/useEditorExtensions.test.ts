/**
 * useEditorExtensions.test.ts — Unit tests for the useEditorExtensions hook.
 *
 * Covers:
 * - Compartment creation (all 13 compartments)
 * - Extension array composition
 * - Tab size handling (TAB_SIZE_TABS_MODE vs numeric values)
 * - Line wrapping toggle (enabled/disabled)
 * - Minimap toggle (enabled/disabled)
 * - Inlay hints toggle (enabled/disabled)
 * - Signature help toggle (enabled/disabled)
 * - Whitespace rendering mode
 * - Relative line numbers toggle
 * - Theme configuration (one-dark vs default vs custom)
 * - Language extensions with null/non-null languageId
 * - Emmet and auto-close tag extensions
 * - LSP compartment starts empty
 * - Extra keymaps are included
 * - Action callbacks are wired correctly
 * - Exported constants (TAB_SIZE_TABS_MODE, TAB_SIZE_DEFAULT)
 */
// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mock functions — ALL references in vi.mock factories must use wrapper
// functions (e.g. (...a) => fn(...a)) to avoid TDZ since factories are
// hoisted above const declarations.
// ---------------------------------------------------------------------------

// CodeMirror mocks
const mockAutocompletion = vi.fn(() => 'cm-autocompletion');
const mockCloseBrackets = vi.fn(() => 'cm-closeBrackets');
const mockHistory = vi.fn(() => 'cm-history');
const mockSyntaxHighlighting = vi.fn((s) => `cm-syntaxHighlighting(${s})`);
const mockCodeFolding = vi.fn(() => 'cm-codeFolding');
const mockFoldGutter = vi.fn(() => 'cm-foldGutter');
const mockIndentOnInput = vi.fn(() => 'cm-indentOnInput');
const mockBracketMatching = vi.fn(() => 'cm-bracketMatching');
const mockIndentUnit = vi.fn((v) => `cm-indentUnit(${v})`);
const mockAllowMultipleOf = vi.fn((v) => `cm-allowMultiple(${v})`);
const mockTabSizeOf = vi.fn((v) => `cm-tabSize(${v})`);
const mockEditorViewTheme = vi.fn(() => 'cm-editorView-theme');
const mockKeymapOf = vi.fn(() => 'cm-keymap');
const mockLineNumbers = vi.fn(() => 'cm-lineNumbers');
const mockHighlightSpecialChars = vi.fn(() => 'cm-highlightSpecialChars');
const mockHighlightActiveLine = vi.fn(() => 'cm-highlightActiveLine');
const mockHighlightActiveLineGutter = vi.fn(() => 'cm-highlightActiveLineGutter');
const mockRectangularSelection = vi.fn(() => 'cm-rectangularSelection');
const mockCrosshairCursor = vi.fn(() => 'cm-crosshairCursor');
const mockDropCursor = vi.fn(() => 'cm-dropCursor');
const mockDrawSelection = vi.fn(() => 'cm-drawSelection');
const mockScrollPastEnd = vi.fn(() => 'cm-scrollPastEnd');

// Compartment must be a real constructor (not vi.fn) because the hook uses `new Compartment()`
function MockCompartment() {
  this.of = (ext) => `compartment-of(${typeof ext === 'string' ? ext : Array.isArray(ext) ? (ext.length ? ext[0] : 'empty-array') : typeof ext})`;
  this.reconfigure = vi.fn();
}
const mockCompartment = MockCompartment;

// Local extension mocks
const mockCreateAutoCloseTagCompartment = vi.fn(() => ({ of: (...a) => mockGetInitialAutoCloseTagExtensions(...a), reconfigure: vi.fn() }));
const mockGetInitialAutoCloseTagExtensions = vi.fn((id) => id ? `autoCloseTag(${id})` : 'autoCloseTag-none');
const mockBracketColorizationPlugin = vi.fn(() => 'mock-bracketColorization');
const mockCreateCodeActionsExtension = vi.fn(() => 'mock-codeActions');
const mockCodeLensPlugin = vi.fn(() => 'mock-codeLens');
const mockCreateEmmetCompartment = vi.fn(() => ({ of: (...a) => mockGetInitialEmmetExtensions(...a), reconfigure: vi.fn() }));
const mockGetInitialEmmetExtensions = vi.fn((id) => id ? `emmet(${id})` : 'emmet-none');
const mockErrorLensPlugin = vi.fn(() => 'mock-errorLens');
const mockCreateHoverTooltipExtension = vi.fn(() => 'mock-hoverTooltip');
const mockIndentGuidesPlugin = vi.fn(() => 'mock-indentGuides');
const mockInlayHintsExtension = vi.fn((fp, gc, id) => `mock-inlayHints(${id})`);
const mockGetLanguageExtensions = vi.fn((id) => `language-extensions(${id})`);
const mockLinkedScrollExtension = vi.fn((pid) => `mock-linkedScroll(${pid})`);
const mockLintDiagnostics = vi.fn(() => 'mock-lintDiagnostics');
const mockMinimapExtension = vi.fn(() => 'mock-minimap');
const mockCustomSearchExtension = vi.fn(() => 'mock-searchExtension');
const mockSignatureHelpExtension = vi.fn((fp, gc, id) => `mock-signatureHelp(${id})`);
const mockTabExpandSnippets = vi.fn(() => 'mock-tabExpandSnippets');
const mockStickyScrollPlugin = vi.fn(() => 'mock-stickyScroll');
const mockTrailingWhitespacePlugin = vi.fn(() => 'mock-trailingWhitespace');
const mockUnsavedLineHighlight = vi.fn(() => 'mock-unsavedLineHighlight');
const mockSetOriginalContentOf = vi.fn((v) => `mock-setOriginalContent(${v})`);
const mockWhitespaceRenderingPlugin = vi.fn((m) => `mock-whitespaceRendering(${m})`);
const mockWordHighlightsExtension = vi.fn(() => 'mock-wordHighlights');

// ---------------------------------------------------------------------------
// vi.mock factories — all references use wrapper functions to avoid TDZ
// ---------------------------------------------------------------------------

vi.mock('@codemirror/autocomplete', () => ({
  autocompletion: (...a) => mockAutocompletion(...a),
  closeBrackets: (...a) => mockCloseBrackets(...a),
}));
vi.mock('@codemirror/commands', () => ({
  defaultKeymap: ['cm-defaultKeymap'],
  indentWithTab: 'cm-indentWithTab',
  history: (...a) => mockHistory(...a),
}));
vi.mock('@codemirror/language', () => ({
  syntaxHighlighting: (...a) => mockSyntaxHighlighting(...a),
  defaultHighlightStyle: 'cm-defaultHighlightStyle',
  codeFolding: (...a) => mockCodeFolding(...a),
  foldGutter: (...a) => mockFoldGutter(...a),
  indentOnInput: (...a) => mockIndentOnInput(...a),
  bracketMatching: (...a) => mockBracketMatching(...a),
  indentUnit: { of: (...a) => mockIndentUnit(...a) },
}));
vi.mock('@codemirror/state', () => ({
  EditorState: {
    allowMultipleSelections: { of: (...a) => mockAllowMultipleOf(...a) },
    tabSize: { of: (...a) => mockTabSizeOf(...a) },
  },
  Compartment: MockCompartment,
}));
vi.mock('@codemirror/theme-one-dark', () => ({
  oneDarkHighlightStyle: 'cm-oneDarkHighlightStyle',
}));
vi.mock('@codemirror/view', () => ({
  EditorView: {
    theme: (...a) => mockEditorViewTheme(...a),
    lineWrapping: 'cm-lineWrapping',
  },
  keymap: { of: (...a) => mockKeymapOf(...a) },
  lineNumbers: (...a) => mockLineNumbers(...a),
  highlightSpecialChars: (...a) => mockHighlightSpecialChars(...a),
  highlightActiveLine: (...a) => mockHighlightActiveLine(...a),
  highlightActiveLineGutter: (...a) => mockHighlightActiveLineGutter(...a),
  rectangularSelection: (...a) => mockRectangularSelection(...a),
  crosshairCursor: (...a) => mockCrosshairCursor(...a),
  dropCursor: (...a) => mockDropCursor(...a),
  drawSelection: (...a) => mockDrawSelection(...a),
  scrollPastEnd: (...a) => mockScrollPastEnd(...a),
}));
vi.mock('@uiw/codemirror-extensions-color', () => ({ color: 'uiw-color' }));
vi.mock('@uiw/codemirror-extensions-hyper-link', () => ({ hyperLink: 'uiw-hyperLink' }));
vi.mock('@uiw/codemirror-extensions-line-numbers-relative', () => ({ lineNumbersRelative: 'uiw-lineNumbersRelative' }));

vi.mock('../extensions/autoCloseTag', () => ({
  createAutoCloseTagCompartment: (...a) => mockCreateAutoCloseTagCompartment(...a),
  getInitialAutoCloseTagExtensions: (...a) => mockGetInitialAutoCloseTagExtensions(...a),
}));
vi.mock('../extensions/bracketColorization', () => ({
  bracketColorizationPlugin: (...a) => mockBracketColorizationPlugin(...a),
}));
vi.mock('../extensions/codeActions', () => ({
  createCodeActionsExtension: (...a) => mockCreateCodeActionsExtension(...a),
}));
vi.mock('../extensions/codeLens', () => ({
  codeLensPlugin: (...a) => mockCodeLensPlugin(...a),
}));
vi.mock('../extensions/cursorHistory', () => ({
  cursorHistoryPlugin: 'mock-cursorHistory',
}));
vi.mock('../extensions/diffGutter', () => ({
  diffGutter: vi.fn(() => 'mock-diffGutter'),
}));
vi.mock('../extensions/dragDropMove', () => ({
  dragDropMovePlugin: 'mock-dragDropMove',
}));
vi.mock('../extensions/emmet', () => ({
  createEmmetCompartment: (...a) => mockCreateEmmetCompartment(...a),
  getInitialEmmetExtensions: (...a) => mockGetInitialEmmetExtensions(...a),
}));
vi.mock('../extensions/errorLens', () => ({
  errorLensPlugin: (...a) => mockErrorLensPlugin(...a),
}));
vi.mock('../extensions/hoverTooltip', () => ({
  createHoverTooltipExtension: (...a) => mockCreateHoverTooltipExtension(...a),
}));
vi.mock('../extensions/indentGuides', () => ({
  indentGuidesPlugin: (...a) => mockIndentGuidesPlugin(...a),
}));
vi.mock('../extensions/inlayHints', () => ({
  inlayHintsExtension: (...a) => mockInlayHintsExtension(...a),
}));
vi.mock('../extensions/languageRegistry', () => ({
  getLanguageExtensions: (...a) => mockGetLanguageExtensions(...a),
  resolveLanguageId: vi.fn(() => ({ languageId: 'plaintext' })),
}));
vi.mock('../extensions/linkedScroll', () => ({
  linkedScrollExtension: (...a) => mockLinkedScrollExtension(...a),
  setLinkedScrollEnabled: vi.fn(),
  suppressScrollSync: vi.fn(),
}));
vi.mock('../extensions/lintDiagnostics', () => ({
  lintDiagnostics: (...a) => mockLintDiagnostics(...a),
  clearDiagnostics: vi.fn(),
  createDebouncedDiagnosticsUpdater: vi.fn(() => ({ cancel: vi.fn(), update: vi.fn() })),
}));
vi.mock('../extensions/minimap', () => ({
  minimapExtension: (...a) => mockMinimapExtension(...a),
}));
vi.mock('../extensions/renameOverlay', () => ({
  renameHighlightField: 'mock-renameHighlightField',
}));
vi.mock('../extensions/searchPanel', () => ({
  customSearchExtension: (...a) => mockCustomSearchExtension(...a),
}));
vi.mock('../extensions/signatureHelp', () => ({
  signatureHelpExtension: (...a) => mockSignatureHelpExtension(...a),
}));
vi.mock('../extensions/snippets', () => ({
  tabExpandSnippets: (...a) => mockTabExpandSnippets(...a),
}));
vi.mock('../extensions/stickyScroll', () => ({
  stickyScrollPlugin: (...a) => mockStickyScrollPlugin(...a),
}));
vi.mock('../extensions/trailingWhitespace', () => ({
  trailingWhitespacePlugin: (...a) => mockTrailingWhitespacePlugin(...a),
}));
vi.mock('../extensions/unsavedLineHighlight', () => ({
  unsavedLineHighlight: (...a) => mockUnsavedLineHighlight(...a),
  setOriginalContent: { of: (...a) => mockSetOriginalContentOf(...a) },
}));
vi.mock('../extensions/whitespaceRendering', () => ({
  whitespaceRenderingPlugin: (...a) => mockWhitespaceRenderingPlugin(...a),
}));
vi.mock('../extensions/wordHighlights', () => ({
  wordHighlightsExtension: (...a) => mockWordHighlightsExtension(...a),
}));
vi.mock('../themes/themePacks', () => ({
  defaultThemePacks: {
    dark: { mode: 'dark', editorSyntaxStyle: 'default' },
    'one-dark': { mode: 'dark', editorSyntaxStyle: 'one-dark' },
    light: { mode: 'light', editorSyntaxStyle: 'default' },
  },
}));

// Static import
import { useEditorExtensions, TAB_SIZE_TABS_MODE, TAB_SIZE_DEFAULT } from './useEditorExtensions';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container, root;
const mockGetFilePath = vi.fn(() => '/test/file.ts');
const mockGetFileExt = vi.fn(() => '.ts');
const mockGetContent = vi.fn(() => 'const x = 1;');
const mockGetSaveFn = vi.fn(() => vi.fn());

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => root?.unmount());
  container?.remove();
});

function renderHook() {
  let ret = null;
  function W() { ret = useEditorExtensions(); return null; }
  act(() => root.render(createElement(W)));
  return ret;
}

function buildOpts(opts = {}) {
  const o = {
    paneId: 'pane-1', wordWrapEnabled: false, relativeLineNumbersEnabled: false,
    minimapEnabled: false, editorFontSize: 14, editorTabSize: 4, editorUsesTabs: false,
    whitespaceRenderingMode: 'none', inlayHintsEnabled: false, signatureHelpEnabled: false,
    languageId: 'typescript',
    themePack: { mode: 'dark', editorSyntaxStyle: 'default' },
    customHighlightStyle: null,
    hotkeysCompartmentExtension: 'hotkeys-extensions',
    extraKeymaps: [],
    ...opts,
  };
  return {
    paneId: o.paneId,
    settings: {
      wordWrapEnabled: o.wordWrapEnabled, relativeLineNumbersEnabled: o.relativeLineNumbersEnabled,
      minimapEnabled: o.minimapEnabled, editorFontSize: o.editorFontSize,
      editorTabSize: o.editorTabSize, editorUsesTabs: o.editorUsesTabs,
      whitespaceRenderingMode: o.whitespaceRenderingMode,
      inlayHintsEnabled: o.inlayHintsEnabled, signatureHelpEnabled: o.signatureHelpEnabled,
    },
    theme: { themePack: o.themePack, customHighlightStyle: o.customHighlightStyle },
    buffer: {
      languageId: o.languageId,
      getFilePath: mockGetFilePath, getFileExt: mockGetFileExt, getContent: mockGetContent,
    },
    actions: { getSaveFn: mockGetSaveFn },
    hotkeysCompartmentExtension: o.hotkeysCompartmentExtension,
    extraKeymaps: o.extraKeymaps,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('exported constants', () => {
  it('TAB_SIZE_TABS_MODE equals 0', () => { expect(TAB_SIZE_TABS_MODE).toBe(0); });
  it('TAB_SIZE_DEFAULT equals 4', () => { expect(TAB_SIZE_DEFAULT).toBe(4); });
});

describe('compartment creation', () => {
  it('returns all 13 compartment handles', () => {
    const { compartments } = renderHook();
    expect(compartments.hotkeys).toBeDefined();
    expect(compartments.lineWrapping).toBeDefined();
    expect(compartments.relativeLineNumbers).toBeDefined();
    expect(compartments.language).toBeDefined();
    expect(compartments.minimap).toBeDefined();
    expect(compartments.whitespaceRendering).toBeDefined();
    expect(compartments.emmet).toBeDefined();
    expect(compartments.autoCloseTag).toBeDefined();
    expect(compartments.fontSize).toBeDefined();
    expect(compartments.tabSize).toBeDefined();
    expect(compartments.lsp).toBeDefined();
    expect(compartments.inlayHints).toBeDefined();
    expect(compartments.signatureHelp).toBeDefined();
  });

  it('returns exactly 13 compartment properties', () => {
    const { compartments } = renderHook();
    expect(Object.keys(compartments).length).toBe(13);
  });

  it('uses createEmmetCompartment and createAutoCloseTagCompartment helpers', () => {
    renderHook();
    expect(mockCreateEmmetCompartment).toHaveBeenCalled();
    expect(mockCreateAutoCloseTagCompartment).toHaveBeenCalled();
  });
});

describe('buildExtensions — array structure', () => {
  it('returns a non-empty array', () => {
    const ext = renderHook().buildExtensions(buildOpts());
    expect(Array.isArray(ext)).toBe(true);
    expect(ext.length).toBeGreaterThan(0);
  });

  it('includes base editor extensions', () => {
    const ext = renderHook().buildExtensions(buildOpts());
    expect(ext).toContain('cm-allowMultiple(true)');
    expect(ext).toContain('cm-lineNumbers');
    expect(ext).toContain('cm-indentOnInput');
    expect(ext).toContain('cm-bracketMatching');
    expect(ext).toContain('cm-history');
  });

  it('includes custom editor extensions', () => {
    const ext = renderHook().buildExtensions(buildOpts());
    expect(ext).toContain('mock-bracketColorization');
    expect(ext).toContain('mock-cursorHistory');
    expect(ext).toContain('mock-diffGutter');
    expect(ext).toContain('mock-dragDropMove');
    expect(ext).toContain('mock-indentGuides');
    expect(ext).toContain('mock-lintDiagnostics');
    expect(ext).toContain('mock-searchExtension');
    expect(ext).toContain('mock-trailingWhitespace');
    expect(ext).toContain('mock-unsavedLineHighlight');
    expect(ext).toContain('mock-wordHighlights');
  });

  it('includes extraKeymaps in the extension array', () => {
    const ext = renderHook().buildExtensions(buildOpts({ extraKeymaps: ['km-1', 'km-2'] }));
    expect(ext).toContain('km-1');
    expect(ext).toContain('km-2');
  });

  it('includes hotkeysCompartmentExtension', () => {
    const ext = renderHook().buildExtensions(buildOpts({ hotkeysCompartmentExtension: 'my-hotkeys' }));
    expect(ext).toContain('my-hotkeys');
  });

  it('includes the CodeMirror theme', () => {
    const ext = renderHook().buildExtensions(buildOpts());
    expect(ext).toContain('cm-editorView-theme');
  });
});

describe('tab size handling', () => {
  it('uses spaces when editorUsesTabs is false', () => {
    const ext = renderHook().buildExtensions(buildOpts({ editorTabSize: 2, editorUsesTabs: false }));
    // indentUnit is wrapped by compartments.tabSize.of()
    expect(ext.some(e => typeof e === 'string' && e.includes('indentUnit') && e.includes('  '))).toBe(true);
  });

  it('uses tabs when editorUsesTabs is true', () => {
    const ext = renderHook().buildExtensions(buildOpts({ editorTabSize: 4, editorUsesTabs: true }));
    // indentUnit is wrapped by compartments.tabSize.of()
    expect(ext.some(e => typeof e === 'string' && e.includes('indentUnit') && e.includes('\\t'))).toBe(true);
  });

  it('converts TAB_SIZE_TABS_MODE (0) to TAB_SIZE_DEFAULT', () => {
    const ext = renderHook().buildExtensions(buildOpts({ editorTabSize: 0 }));
    expect(ext.some(e => typeof e === 'string' && e.includes('tabSize(4)'))).toBe(true);
  });

  it('uses numeric tabSize as-is for positive numbers', () => {
    const ext = renderHook().buildExtensions(buildOpts({ editorTabSize: 2 }));
    expect(ext.some(e => typeof e === 'string' && e.includes('tabSize(2)'))).toBe(true);
  });
});

describe('line wrapping', () => {
  it('includes lineWrapping when enabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ wordWrapEnabled: true }));
    expect(ext).toContain('cm-lineWrapping');
  });

  it('excludes lineWrapping when disabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ wordWrapEnabled: false }));
    expect(ext).not.toContain('cm-lineWrapping');
  });
});

describe('relative line numbers', () => {
  it('uses lineNumbersRelative when enabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ relativeLineNumbersEnabled: true }));
    expect(ext.some(e => typeof e === 'string' && e.includes('uiw-lineNumbersRelative'))).toBe(true);
  });

  it('uses regular lineNumbers when disabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ relativeLineNumbersEnabled: false }));
    expect(ext.some(e => typeof e === 'string' && e.includes('cm-lineNumbers'))).toBe(true);
  });
});

describe('minimap', () => {
  it('includes minimap when enabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ minimapEnabled: true }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-minimap'))).toBe(true);
  });

  it('excludes minimap when disabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ minimapEnabled: false }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-minimap'))).toBe(false);
  });
});

describe('inlay hints', () => {
  it('includes inlay hints when enabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ inlayHintsEnabled: true, languageId: 'typescript' }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-inlayHints(typescript)'))).toBe(true);
  });

  it('excludes inlay hints when disabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ inlayHintsEnabled: false }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-inlayHints'))).toBe(false);
  });
});

describe('signature help', () => {
  it('includes signature help when enabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ signatureHelpEnabled: true, languageId: 'typescript' }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-signatureHelp(typescript)'))).toBe(true);
  });

  it('excludes signature help when disabled', () => {
    const ext = renderHook().buildExtensions(buildOpts({ signatureHelpEnabled: false }));
    expect(ext.some(e => typeof e === 'string' && e.includes('mock-signatureHelp'))).toBe(false);
  });
});

describe('whitespace rendering', () => {
  it('passes whitespaceRenderingMode to plugin', () => {
    renderHook().buildExtensions(buildOpts({ whitespaceRenderingMode: 'boundary' }));
    expect(mockWhitespaceRenderingPlugin).toHaveBeenCalledWith('boundary');
  });

  it('defaults to "none" mode', () => {
    renderHook().buildExtensions(buildOpts());
    expect(mockWhitespaceRenderingPlugin).toHaveBeenCalledWith('none');
  });
});

describe('theme configuration', () => {
  it('uses oneDarkHighlightStyle for one-dark syntax style', () => {
    renderHook().buildExtensions(buildOpts({ themePack: { mode: 'dark', editorSyntaxStyle: 'one-dark' } }));
    expect(mockSyntaxHighlighting).toHaveBeenCalledWith('cm-oneDarkHighlightStyle');
  });

  it('uses defaultHighlightStyle for default syntax style', () => {
    renderHook().buildExtensions(buildOpts({ themePack: { mode: 'dark', editorSyntaxStyle: 'default' } }));
    expect(mockSyntaxHighlighting).toHaveBeenCalledWith('cm-defaultHighlightStyle');
  });

  it('uses customHighlightStyle when provided', () => {
    renderHook().buildExtensions(buildOpts({ customHighlightStyle: 'my-style' }));
    expect(mockSyntaxHighlighting).toHaveBeenCalledWith('my-style');
  });
});

describe('language extensions', () => {
  it('calls getLanguageExtensions with buffer languageId', () => {
    renderHook().buildExtensions(buildOpts({ languageId: 'typescript' }));
    expect(mockGetLanguageExtensions).toHaveBeenCalledWith('typescript');
  });

  it('handles null languageId', () => {
    renderHook().buildExtensions(buildOpts({ languageId: null }));
    expect(mockGetLanguageExtensions).toHaveBeenCalledWith(null);
  });
});

describe('emmet and auto-close tag', () => {
  it('calls getInitialEmmetExtensions with languageId', () => {
    renderHook().buildExtensions(buildOpts({ languageId: 'typescript' }));
    expect(mockGetInitialEmmetExtensions).toHaveBeenCalledWith('typescript');
  });

  it('calls getInitialAutoCloseTagExtensions with languageId', () => {
    renderHook().buildExtensions(buildOpts({ languageId: 'typescript' }));
    expect(mockGetInitialAutoCloseTagExtensions).toHaveBeenCalledWith('typescript');
  });
});

describe('LSP compartment', () => {
  it('starts with empty array placeholder', () => {
    const ext = renderHook().buildExtensions(buildOpts());
    // LSP wraps [] initially, no LSP-specific extensions
    const hasLsp = ext.some(e => typeof e === 'string' && e.includes('lsp-'));
    expect(hasLsp).toBe(false);
  });
});

describe('action callbacks', () => {
  it('passes a callback to search extension', () => {
    renderHook().buildExtensions(buildOpts());
    expect(mockCustomSearchExtension).toHaveBeenCalled();
    expect(typeof mockCustomSearchExtension.mock.calls[0][0]).toBe('function');
  });
});

describe('pane-specific configuration', () => {
  it('passes paneId to linkedScrollExtension', () => {
    renderHook().buildExtensions(buildOpts({ paneId: 'pane-42' }));
    expect(mockLinkedScrollExtension).toHaveBeenCalledWith('pane-42', expect.any(Function));
  });

  it('passes callbacks to hover and code actions extensions', () => {
    renderHook().buildExtensions(buildOpts());
    expect(mockCreateHoverTooltipExtension).toHaveBeenCalled();
    expect(mockCreateCodeActionsExtension).toHaveBeenCalled();
  });
});

describe('buildExtensions stability', () => {
  it('buildExtensions is a function', () => {
    expect(typeof renderHook().buildExtensions).toBe('function');
  });
});
