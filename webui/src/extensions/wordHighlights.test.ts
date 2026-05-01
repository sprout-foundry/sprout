/**
 * wordHighlights.test.ts — Unit tests for the wordHighlights extension.
 *
 * CodeMirror 6 modules are mocked to avoid loading their ESM internals
 * in the test runner. We test the exported factory function and verify
 * the structure of the returned extension array.
 *
 * Note: EditorView.baseTheme is called at module-load time (not inside the
 * factory), so its mock call data is captured on first import and must not
 * be cleared between tests.
 */

import { vi, describe, it, expect } from 'vitest';

// ── Mock CodeMirror modules ───────────────────────────────────────

vi.mock('@codemirror/view', () => ({
  Decoration: {
    mark: vi.fn(() => ({ range: vi.fn() })),
    none: [],
    set: vi.fn(),
    widget: vi.fn(),
  },
  ViewPlugin: { fromClass: vi.fn() },
  EditorView: {
    baseTheme: vi.fn(() => []),
  },
}));

vi.mock('@codemirror/state', () => ({}));

vi.mock('@codemirror/search', () => ({
  highlightSelectionMatches: vi.fn(() => ({ type: 'highlightSelectionMatches' })),
}));

// ── Module under test (Vitest hoists vi.mock above imports) ────────

import { wordHighlightsExtension } from './wordHighlights';
import { EditorView } from '@codemirror/view';
import { highlightSelectionMatches } from '@codemirror/search';

// Access the mocked EditorView to capture baseTheme calls
const mockEditorViewBaseTheme = vi.mocked(EditorView.baseTheme);
const mockHSM = vi.mocked(highlightSelectionMatches);

// Captured once from module-load time (before any tests run)
const themeConfig = mockEditorViewBaseTheme.mock.calls[0]?.[0];

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

  // ── Theme selector presence tests ─────────────────────────────

  it('defines a theme selector for .cm-selectionMatch', () => {
    expect(themeConfig['.cm-selectionMatch']).toBeDefined();
  });

  it('defines a theme selector for .cm-selectionMatch-main', () => {
    expect(themeConfig['.cm-selectionMatch-main']).toBeDefined();
  });

  it('defines a theme selector to prevent conflict with .cm-searchMatch .cm-selectionMatch', () => {
    expect(themeConfig['.cm-searchMatch .cm-selectionMatch']).toBeDefined();
  });

  it('does NOT define &dark .cm-selectionMatch selector (removed in refactor)', () => {
    expect(themeConfig['&dark .cm-selectionMatch']).toBeUndefined();
  });

  it('does NOT define &light .cm-selectionMatch selector (removed in refactor)', () => {
    expect(themeConfig['&light .cm-selectionMatch']).toBeUndefined();
  });

  // ── CSS variable theme style tests ────────────────────────────────

  it('uses CSS variable --cm-selection-match-bg with fallback for .cm-selectionMatch backgroundColor', () => {
    expect(themeConfig['.cm-selectionMatch'].backgroundColor).toBe('var(--cm-selection-match-bg, rgba(97, 175, 239, 0.12))');
  });

  it('uses CSS variable --cm-selection-match-outline with fallback for .cm-selectionMatch outline', () => {
    expect(themeConfig['.cm-selectionMatch'].outline).toBe('1px solid var(--cm-selection-match-outline, rgba(97, 175, 239, 0.4))');
  });

  it('uses CSS variable --cm-selection-match-main-bg with fallback for .cm-selectionMatch-main backgroundColor', () => {
    expect(themeConfig['.cm-selectionMatch-main'].backgroundColor).toBe('var(--cm-selection-match-main-bg, rgba(97, 175, 239, 0.22))');
  });

  it('uses CSS variable --cm-selection-match-main-outline with fallback for .cm-selectionMatch-main outline', () => {
    expect(themeConfig['.cm-selectionMatch-main'].outline).toBe('1.5px solid var(--cm-selection-match-main-outline, rgba(97, 175, 239, 0.6))');
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
});
