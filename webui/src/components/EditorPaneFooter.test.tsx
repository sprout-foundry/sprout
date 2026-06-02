/**
 * Tests for EditorPaneFooter memoization and rendering.
 *
 * - Custom comparator: areEditorPaneFooterPropsEqual
 * - Render behavior: footer stats, zoom controls, whitespace, LSP badge
 */

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import {
  EditorPaneFooter,
  areEditorPaneFooterPropsEqual,
  type EditorPaneFooterProps,
} from './EditorPaneFooter';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('./LanguageSwitcher', () => ({
  default: ({ currentLanguageId, isAutoDetected }: any) => (
    <div data-testid="language-switcher">
      {currentLanguageId || 'none'}
      {isAutoDetected ? ' (auto)' : ''}
    </div>
  ),
}));

// ---------------------------------------------------------------------------
// Shared function references (so paired props share the same functions)
// ---------------------------------------------------------------------------

const sharedOnCycleTabSize = () => {};
const sharedOnCycleWhitespaceRendering = () => 'all' as const;
const sharedOnZoomIn = () => {};
const sharedOnZoomOut = () => {};
const sharedOnResetZoom = () => {};
const sharedHandleLanguageChange = () => {};
const sharedSetWhitespaceRenderingMode = () => {};

// ---------------------------------------------------------------------------
// Test factories
// ---------------------------------------------------------------------------

function makeSettings(overrides: Partial<NonNullable<EditorPaneFooterProps['settings']>> = {}) {
  return {
    editorFontSize: 14,
    editorTabSize: 4,
    editorUsesTabs: false,
    lineEnding: 'LF',
    onCycleTabSize: sharedOnCycleTabSize,
    onCycleWhitespaceRendering: sharedOnCycleWhitespaceRendering,
    onZoomIn: sharedOnZoomIn,
    onZoomOut: sharedOnZoomOut,
    onResetZoom: sharedOnResetZoom,
    ...overrides,
  };
}

function makeLsp(overrides: Partial<NonNullable<EditorPaneFooterProps['lsp']>> = {}) {
  return {
    lspLanguage: null as string | null,
    lspState: 'disconnected',
    languageInfo: {
      languageId: null as string | null,
      isAutoDetected: false,
    },
    handleLanguageChange: sharedHandleLanguageChange,
    ...overrides,
  };
}

function makeProps(overrides: Partial<EditorPaneFooterProps> = {}): EditorPaneFooterProps {
  return {
    buffer: {
      content: 'line1\nline2\nline3',
      cursorPosition: { line: 1, column: 5 },
      path: 'src/file.ts',
      name: 'file.ts',
    } as any,
    selectionInfo: null,
    whitespaceRenderingMode: 'none',
    settings: makeSettings(),
    lsp: makeLsp(),
    setWhitespaceRenderingMode: sharedSetWhitespaceRenderingMode,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Comparator tests
// ---------------------------------------------------------------------------

describe('areEditorPaneFooterPropsEqual', () => {
  describe('returns true when props are equivalent', () => {
    it('identical props objects', () => {
      const props = makeProps();
      expect(areEditorPaneFooterPropsEqual(props, props)).toBe(true);
    });

    it('same buffer reference (key memoization assumption)', () => {
      const buffer = { content: 'a', cursorPosition: { line: 0, column: 0 }, path: 'a.ts', name: 'a.ts' } as any;
      const prev = makeProps({ buffer, selectionInfo: null });
      const next = makeProps({ buffer, selectionInfo: null });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(true);
    });

    it('new settings/lsp wrapper objects with same inner function refs', () => {
      // Buffer and selectionInfo must be same ref for this to pass
      // (comparator uses !== for buffer/selectionInfo)
      const buffer = { content: 'x', cursorPosition: { line: 0, column: 0 }, path: 'x.ts', name: 'x.ts' } as any;
      const selectionInfo: any = null;
      const prev = makeProps({
        buffer,
        selectionInfo,
        settings: makeSettings(),
        lsp: makeLsp(),
      });
      const next = makeProps({
        buffer,
        selectionInfo,
        settings: makeSettings(),
        lsp: makeLsp(),
      });
      // Even though settings/lsp are new objects, the inner values/funcs
      // are all the same shared refs, so the comparator should return true
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(true);
    });

    it('same setWhitespaceRenderingMode function reference', () => {
      const buffer = { content: 'x', path: 'x.ts', name: 'x.ts' } as any;
      const prev = makeProps({ buffer });
      const next = makeProps({ buffer });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(true);
    });
  });

  describe('returns false when relevant props differ', () => {
    it('different buffer reference', () => {
      const prev = makeProps({ buffer: { content: 'a', path: 'a.ts', name: 'a.ts' } as any });
      const next = makeProps({ buffer: { content: 'b', path: 'b.ts', name: 'b.ts' } as any });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different selectionInfo reference', () => {
      const prev = makeProps({ selectionInfo: { selectionCount: 1, charCount: 5 } });
      const next = makeProps({ selectionInfo: { selectionCount: 2, charCount: 10 } });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different whitespaceRenderingMode', () => {
      const prev = makeProps({ whitespaceRenderingMode: 'none' });
      const next = makeProps({ whitespaceRenderingMode: 'all' });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different setWhitespaceRenderingMode function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ setWhitespaceRenderingMode: fn1 });
      const next = makeProps({ setWhitespaceRenderingMode: fn2 });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different editorFontSize in settings', () => {
      const prev = makeProps({ settings: makeSettings({ editorFontSize: 14 }) });
      const next = makeProps({ settings: makeSettings({ editorFontSize: 16 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different editorTabSize in settings', () => {
      const prev = makeProps({ settings: makeSettings({ editorTabSize: 4 }) });
      const next = makeProps({ settings: makeSettings({ editorTabSize: 2 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different editorUsesTabs in settings', () => {
      const prev = makeProps({ settings: makeSettings({ editorUsesTabs: false }) });
      const next = makeProps({ settings: makeSettings({ editorUsesTabs: true }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different lineEnding in settings', () => {
      const prev = makeProps({ settings: makeSettings({ lineEnding: 'LF' }) });
      const next = makeProps({ settings: makeSettings({ lineEnding: 'CRLF' }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different onCycleTabSize function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ settings: makeSettings({ onCycleTabSize: fn1 }) });
      const next = makeProps({ settings: makeSettings({ onCycleTabSize: fn2 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different onCycleWhitespaceRendering function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ settings: makeSettings({ onCycleWhitespaceRendering: fn1 }) });
      const next = makeProps({ settings: makeSettings({ onCycleWhitespaceRendering: fn2 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different onZoomIn function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ settings: makeSettings({ onZoomIn: fn1 }) });
      const next = makeProps({ settings: makeSettings({ onZoomIn: fn2 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different lspLanguage', () => {
      const prev = makeProps({ lsp: makeLsp({ lspLanguage: 'go' }) });
      const next = makeProps({ lsp: makeLsp({ lspLanguage: 'typescript' }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different lspState', () => {
      const prev = makeProps({ lsp: makeLsp({ lspState: 'connected' }) });
      const next = makeProps({ lsp: makeLsp({ lspState: 'disconnected' }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('shallow comparison on languageInfo: same values in different objects', () => {
      // Buffer must be shared because comparator uses !== on buffer
      const sharedBuffer = { content: 'x', path: 'x.ts', name: 'x.ts' } as any;
      const prev = makeProps({
        buffer: sharedBuffer,
        lsp: makeLsp({ languageInfo: { languageId: 'go', isAutoDetected: false } }),
      });
      const next = makeProps({
        buffer: sharedBuffer,
        lsp: makeLsp({ languageInfo: { languageId: 'go', isAutoDetected: false } }),
      });
      // Comparator compares languageInfo.languageId and languageInfo.isAutoDetected by value,
      // so different object references with same values should be equal.
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(true);
    });

    it('different languageInfo.languageId values', () => {
      const prev = makeProps({ lsp: makeLsp({ languageInfo: { languageId: 'go', isAutoDetected: false } }) });
      const next = makeProps({ lsp: makeLsp({ languageInfo: { languageId: 'python', isAutoDetected: true } }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different languageInfo.isAutoDetected', () => {
      const prev = makeProps({ lsp: makeLsp({ languageInfo: { languageId: 'go', isAutoDetected: false } }) });
      const next = makeProps({ lsp: makeLsp({ languageInfo: { languageId: 'go', isAutoDetected: true } }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });

    it('different handleLanguageChange function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ lsp: makeLsp({ handleLanguageChange: fn1 }) });
      const next = makeProps({ lsp: makeLsp({ handleLanguageChange: fn2 }) });
      expect(areEditorPaneFooterPropsEqual(prev, next)).toBe(false);
    });
  });
});

// ---------------------------------------------------------------------------
// Render tests
// ---------------------------------------------------------------------------

describe('EditorPaneFooter rendering', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  function renderFooter(props: Partial<EditorPaneFooterProps> = {}) {
    const p = makeProps(props);
    act(() => {
      root = createRoot(container);
      root.render(<EditorPaneFooter {...p} />);
    });
  }

  it('renders footer container with pane-footer class', () => {
    renderFooter();
    expect(container.querySelector('.pane-footer')).toBeTruthy();
  });

  it('renders line count from buffer content', () => {
    renderFooter({ buffer: { content: 'a\nb\nc', path: 't.ts', name: 't.ts' } as any });
    expect(container.querySelector('.line-count')?.textContent).toContain('Lines: 3');
  });

  it('renders character count from buffer content', () => {
    renderFooter({ buffer: { content: 'abc', path: 't.ts', name: 't.ts' } as any });
    expect(container.querySelector('.char-count')?.textContent).toContain('Chars: 3');
  });

  it('renders cursor position', () => {
    renderFooter({
      buffer: { content: 'hello', cursorPosition: { line: 2, column: 3 }, path: 't.ts', name: 't.ts' } as any,
    });
    const cursorEl = container.querySelector('.cursor-position');
    expect(cursorEl?.textContent).toContain('Ln 2');
    expect(cursorEl?.textContent).toContain('Col 4');
  });

  it('shows default cursor position 0,0 when buffer is null', () => {
    renderFooter({ buffer: null });
    const cursorEl = container.querySelector('.cursor-position');
    expect(cursorEl?.textContent).toContain('Ln 0');
    expect(cursorEl?.textContent).toContain('Col 0');
  });

  it('shows single selection char count', () => {
    renderFooter({ selectionInfo: { selectionCount: 1, charCount: 12 } });
    const cursorEl = container.querySelector('.cursor-position');
    expect(cursorEl?.textContent).toContain('12 selected');
  });

  it('shows multi-selection count', () => {
    renderFooter({ selectionInfo: { selectionCount: 3, charCount: 10 } });
    const cursorEl = container.querySelector('.cursor-position');
    expect(cursorEl?.textContent).toContain('3 selections');
  });

  it('renders zoom controls and default percentage', () => {
    renderFooter();
    expect(container.querySelectorAll('.zoom-control').length).toBe(2);
    expect(container.querySelector('.zoom-level')?.textContent).toContain('100%');
  });

  it('renders zoom percentage for non-default font size', () => {
    renderFooter({ settings: makeSettings({ editorFontSize: 16 }) });
    expect(container.querySelector('.zoom-level')?.textContent).toContain('114%');
  });

  it('renders tab size display for spaces mode', () => {
    renderFooter({ settings: makeSettings({ editorUsesTabs: false, editorTabSize: 4 }) });
    expect(container.querySelector('.tab-size')?.textContent).toContain('Spaces: 4');
  });

  it('renders tab size display for tabs mode', () => {
    renderFooter({ settings: makeSettings({ editorUsesTabs: true }) });
    expect(container.querySelector('.tab-size')?.textContent).toContain('Tabs');
  });

  it('renders encoding (line-ending) indicator', () => {
    renderFooter({ settings: makeSettings({ lineEnding: 'CRLF' }) });
    // The "UTF-8 ·" prefix was removed — we don't actually detect encoding,
    // so the indicator now shows just the line-ending convention.
    expect(container.querySelector('.encoding-indicator')?.textContent).toContain('CRLF');
  });

  it('shows whitespace mode indicator as "off" when mode is none', () => {
    renderFooter({ whitespaceRenderingMode: 'none' });
    // The indicator is now always rendered so users can click to enable
    // WS rendering from the footer without going through the omnibox or
    // toolbar.
    const ws = container.querySelector('.whitespace-mode');
    expect(ws).toBeTruthy();
    expect(ws?.textContent).toContain('off');
  });

  it('shows whitespace mode indicator when mode is boundary', () => {
    renderFooter({ whitespaceRenderingMode: 'boundary' });
    const ws = container.querySelector('.whitespace-mode');
    expect(ws).toBeTruthy();
    expect(ws?.textContent).toContain('boundary');
  });

  it('shows whitespace mode indicator when mode is all', () => {
    renderFooter({ whitespaceRenderingMode: 'all' });
    const ws = container.querySelector('.whitespace-mode');
    expect(ws).toBeTruthy();
    expect(ws?.textContent).toContain('all');
  });

  it('hides LSP badge when lspLanguage is null', () => {
    renderFooter({ lsp: makeLsp({ lspLanguage: null }) });
    expect(container.querySelector('.cm-footer-lsp')).toBeFalsy();
  });

  it('shows LSP connected badge', () => {
    renderFooter({ lsp: makeLsp({ lspLanguage: 'go', lspState: 'connected' }) });
    const lsp = container.querySelector('.cm-footer-lsp');
    expect(lsp).toBeTruthy();
    // Text glyphs (✓ / … / ✗) were replaced with lucide icons. The state is
    // surfaced via a `cm-footer-lsp--<state>` modifier class on the wrapper.
    expect(lsp?.classList.contains('cm-footer-lsp--connected')).toBe(true);
  });

  it('shows LSP disconnected badge', () => {
    renderFooter({ lsp: makeLsp({ lspLanguage: 'go', lspState: 'disconnected' }) });
    const lsp = container.querySelector('.cm-footer-lsp');
    expect(lsp).toBeTruthy();
    expect(lsp?.classList.contains('cm-footer-lsp--disconnected')).toBe(true);
  });

  it('shows LSP connecting badge', () => {
    renderFooter({ lsp: makeLsp({ lspLanguage: 'go', lspState: 'connecting' }) });
    const lsp = container.querySelector('.cm-footer-lsp');
    expect(lsp).toBeTruthy();
    expect(lsp?.classList.contains('cm-footer-lsp--connecting')).toBe(true);
  });

  it('renders LanguageSwitcher with correct props', () => {
    renderFooter({
      lsp: makeLsp({ languageInfo: { languageId: 'typescript', isAutoDetected: true } }),
    });
    const ls = container.querySelector('[data-testid="language-switcher"]');
    expect(ls?.textContent).toContain('typescript');
    expect(ls?.textContent).toContain('auto');
  });

  it('calls onZoomOut when zoom-out button is clicked', () => {
    const onZoomOut = vi.fn();
    renderFooter({ settings: makeSettings({ onZoomOut }) });
    const zoomOutBtn = container.querySelectorAll('.zoom-control')[0];
    act(() => {
      zoomOutBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onZoomOut).toHaveBeenCalled();
  });

  it('calls onZoomIn when zoom-in button is clicked', () => {
    const onZoomIn = vi.fn();
    renderFooter({ settings: makeSettings({ onZoomIn }) });
    const zoomInBtn = container.querySelectorAll('.zoom-control')[1];
    act(() => {
      zoomInBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onZoomIn).toHaveBeenCalled();
  });

  it('calls onResetZoom when zoom-level is clicked', () => {
    const onResetZoom = vi.fn();
    renderFooter({ settings: makeSettings({ onResetZoom }) });
    act(() => {
      container.querySelector('.zoom-level')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onResetZoom).toHaveBeenCalled();
  });

  it('calls onCycleTabSize when tab-size is clicked', () => {
    const onCycleTabSize = vi.fn();
    renderFooter({ settings: makeSettings({ onCycleTabSize }) });
    act(() => {
      container.querySelector('.tab-size')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onCycleTabSize).toHaveBeenCalled();
  });
});
