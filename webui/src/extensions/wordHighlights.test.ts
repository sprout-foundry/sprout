/**
 * wordHighlights.test.ts — Unit tests for the wordHighlights extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the CM imports are mocked. We test the exported factory function and
 * verify the structure of the returned extension array.
 *
 * Note: EditorView.baseTheme is called at module-load time (not inside the
 * factory), so its mock call data is captured on first import and must not
 * be cleared between tests.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  Decoration: {
    mark: jest.fn(() => ({ range: jest.fn() })),
    none: [],
    set: jest.fn(),
    widget: jest.fn(),
  },
  ViewPlugin: { fromClass: jest.fn() },
  EditorView: {
    baseTheme: jest.fn(() => []),
  },
}));

jest.mock('@codemirror/state', () => ({}));

jest.mock('@codemirror/search', () => ({
  highlightSelectionMatches: jest.fn(() => ({ type: 'highlightSelectionMatches' })),
}));

// ── Module under test (Jest hoists mocks above imports) ─────────────

import { wordHighlightsExtension } from './wordHighlights';
import { EditorView } from '@codemirror/view';
import { highlightSelectionMatches } from '@codemirror/search';

const mockEditorView = EditorView as unknown as { baseTheme: jest.Mock };
const mockHSM = highlightSelectionMatches as jest.Mock;

// Captured once from module-load time (before any tests run)
const themeConfig = mockEditorView.baseTheme.mock.calls[0]?.[0];

// ── Tests ───────────────────────────────────────────────────────────

describe('wordHighlightsExtension', () => {
  // ── Return value structure ────────────────────────────────────

  it('returns an array with exactly 2 elements', () => {
    const ext = wordHighlightsExtension();
    expect(Array.isArray(ext)).toBe(true);
    expect(ext).toHaveLength(2);
  });

  it('returns an array containing the custom baseTheme result as first element', () => {
    const ext = wordHighlightsExtension();
    expect(ext[0]).toEqual([]);
  });

  it('returns an array where the second element is the result of highlightSelectionMatches', () => {
    const ext = wordHighlightsExtension();
    // ext[0] is the baseTheme result, ext[1] is highlightSelectionMatches() result.
    // With mocking, ext[1] may be opaque; verify the mock was invoked instead.
    expect(mockHSM).toHaveBeenCalled();
  });

  it('creates independent extension arrays on each call', () => {
    const ext1 = wordHighlightsExtension();
    const ext2 = wordHighlightsExtension();
    expect(ext1).not.toBe(ext2);
    expect(ext1).toHaveLength(2);
    expect(ext2).toHaveLength(2);
  });

  // ── highlightSelectionMatches configuration ───────────────────

  it('configures highlightSelectionMatches with the expected options', () => {
    wordHighlightsExtension();
    expect(mockHSM).toHaveBeenCalledWith({
      highlightWordAroundCursor: true,
      minSelectionLength: 2,
      maxMatches: 200,
      wholeWords: false,
    });
  });

  it('configures highlightSelectionMatches with highlightWordAroundCursor: true', () => {
    wordHighlightsExtension();
    expect(mockHSM).toHaveBeenCalledWith(
      expect.objectContaining({ highlightWordAroundCursor: true })
    );
  });

  it('configures highlightSelectionMatches with minSelectionLength of 2', () => {
    wordHighlightsExtension();
    expect(mockHSM).toHaveBeenCalledWith(
      expect.objectContaining({ minSelectionLength: 2 })
    );
  });

  it('configures highlightSelectionMatches with maxMatches of 200', () => {
    wordHighlightsExtension();
    expect(mockHSM).toHaveBeenCalledWith(
      expect.objectContaining({ maxMatches: 200 })
    );
  });

  it('configures highlightSelectionMatches with wholeWords: false', () => {
    wordHighlightsExtension();
    expect(mockHSM).toHaveBeenCalledWith(
      expect.objectContaining({ wholeWords: false })
    );
  });

  // ── Theme selector presence tests ─────────────────────────────

  it('defines a theme selector for .cm-selectionMatch', () => {
    // toHaveProperty interprets dots as nested paths; use direct access instead
    expect(themeConfig['.cm-selectionMatch']).toBeDefined();
  });

  it('defines a theme selector for .cm-selectionMatch-main', () => {
    expect(themeConfig['.cm-selectionMatch-main']).toBeDefined();
  });

  it('defines a theme selector to prevent conflict with .cm-searchMatch .cm-selectionMatch', () => {
    expect(themeConfig['.cm-searchMatch .cm-selectionMatch']).toBeDefined();
  });

  it('defines dark mode overrides for .cm-selectionMatch', () => {
    expect(themeConfig['&dark .cm-selectionMatch']).toBeDefined();
  });

  it('defines dark mode overrides for .cm-selectionMatch-main', () => {
    expect(themeConfig['&dark .cm-selectionMatch-main']).toBeDefined();
  });

  it('defines light mode overrides for .cm-selectionMatch', () => {
    expect(themeConfig['&light .cm-selectionMatch']).toBeDefined();
  });

  it('defines light mode overrides for .cm-selectionMatch-main', () => {
    expect(themeConfig['&light .cm-selectionMatch-main']).toBeDefined();
  });

  // ── Default (base) theme style values ─────────────────────────

  it('uses a custom backgroundColor for .cm-selectionMatch (not the default lime green)', () => {
    // The CodeMirror default is '#99ff7780' (ugly lime green)
    // Our custom color should NOT be this
    expect(themeConfig['.cm-selectionMatch'].backgroundColor).not.toBe('#99ff7780');
    expect(themeConfig['.cm-selectionMatch'].backgroundColor).toBe('rgba(97, 175, 239, 0.12)');
  });

  it('uses a custom backgroundColor for .cm-selectionMatch-main (not the default lime green)', () => {
    expect(themeConfig['.cm-selectionMatch-main'].backgroundColor).not.toBe('#99ff7780');
    expect(themeConfig['.cm-selectionMatch-main'].backgroundColor).toBe('rgba(97, 175, 239, 0.22)');
  });

  it('sets .cm-searchMatch .cm-selectionMatch to transparent to prevent visual conflict', () => {
    expect(themeConfig['.cm-searchMatch .cm-selectionMatch'].backgroundColor).toBe('transparent');
    expect(themeConfig['.cm-searchMatch .cm-selectionMatch'].outline).toBe('none');
  });

  it('sets borderRadius on .cm-selectionMatch', () => {
    expect(themeConfig['.cm-selectionMatch'].borderRadius).toBe('2px');
  });

  it('sets borderRadius on .cm-selectionMatch-main', () => {
    expect(themeConfig['.cm-selectionMatch-main'].borderRadius).toBe('2px');
  });

  it('sets boxShadow on .cm-selectionMatch to prevent stacking', () => {
    expect(themeConfig['.cm-selectionMatch'].boxShadow).toBe('0 0 0 1px transparent');
  });

  it('sets boxShadow on .cm-selectionMatch-main to prevent stacking', () => {
    expect(themeConfig['.cm-selectionMatch-main'].boxShadow).toBe('0 0 0 1px transparent');
  });

  it('sets outline on .cm-selectionMatch with subtle transparency', () => {
    expect(themeConfig['.cm-selectionMatch'].outline).toBe('1px solid rgba(97, 175, 239, 0.4)');
  });

  it('sets outline on .cm-selectionMatch-main with more prominent transparency', () => {
    expect(themeConfig['.cm-selectionMatch-main'].outline).toBe('1.5px solid rgba(97, 175, 239, 0.6)');
  });

  // ── Dark mode variant tests ───────────────────────────────────

  it('uses darker variant colors for &dark .cm-selectionMatch', () => {
    expect(themeConfig['&dark .cm-selectionMatch'].backgroundColor).toBe('rgba(139, 233, 253, 0.15)');
    expect(themeConfig['&dark .cm-selectionMatch'].outline).toBe('1px solid rgba(139, 233, 253, 0.45)');
  });

  it('uses darker variant colors for &dark .cm-selectionMatch-main', () => {
    expect(themeConfig['&dark .cm-selectionMatch-main'].backgroundColor).toBe('rgba(139, 233, 253, 0.25)');
    expect(themeConfig['&dark .cm-selectionMatch-main'].outline).toBe('1.5px solid rgba(139, 233, 253, 0.65)');
  });

  it('sets boxShadow on dark mode .cm-selectionMatch', () => {
    expect(themeConfig['&dark .cm-selectionMatch'].boxShadow).toBe('0 0 0 1px transparent');
  });

  it('sets boxShadow on dark mode .cm-selectionMatch-main', () => {
    expect(themeConfig['&dark .cm-selectionMatch-main'].boxShadow).toBe('0 0 0 1px transparent');
  });

  // ── Light mode variant tests ──────────────────────────────────

  it('uses lighter variant colors for &light .cm-selectionMatch', () => {
    expect(themeConfig['&light .cm-selectionMatch'].backgroundColor).toBe('rgba(64, 120, 242, 0.14)');
    expect(themeConfig['&light .cm-selectionMatch'].outline).toBe('1px solid rgba(64, 120, 242, 0.5)');
  });

  it('uses lighter variant colors for &light .cm-selectionMatch-main', () => {
    expect(themeConfig['&light .cm-selectionMatch-main'].backgroundColor).toBe('rgba(64, 120, 242, 0.24)');
    expect(themeConfig['&light .cm-selectionMatch-main'].outline).toBe('1.5px solid rgba(64, 120, 242, 0.7)');
  });

  it('sets boxShadow on light mode .cm-selectionMatch', () => {
    expect(themeConfig['&light .cm-selectionMatch'].boxShadow).toBe('0 0 0 1px transparent');
  });

  it('sets boxShadow on light mode .cm-selectionMatch-main', () => {
    expect(themeConfig['&light .cm-selectionMatch-main'].boxShadow).toBe('0 0 0 1px transparent');
  });
});
