/**
 * wordHighlights.ts — CodeMirror 6 extension for custom word occurrence highlighting.
 *
 * Provides themed styling for the `highlightSelectionMatches()` feature from
 * @codemirror/search. When text is selected, matching occurrences are highlighted
 * throughout the document with a subtle background and outline that integrates
 * with the project's theme system.
 *
 * Highlights:
 * - `.cm-selectionMatch` — all matching occurrences (subtle)
 * - `.cm-selectionMatch-main` — the primary match at cursor (more prominent)
 *
 * The extension uses `highlightSelectionMatches()` from @codemirror/search with
 * sensible defaults (word-around-cursor highlighting, minimum length, etc.) and
 * overrides the default lime-green styling with subtle, mode-aware colors via
 * CSS variables that are defined in each theme pack.
 *
 * Exported factory: {@link wordHighlightsExtension}
 */

import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { highlightSelectionMatches } from '@codemirror/search';

// ── Base Theme ─────────────────────────────────────────────────────

const wordHighlightsBaseTheme = EditorView.baseTheme({
  // Default styling for matching occurrences (subtle, visible but not jarring)
  '.cm-selectionMatch': {
    backgroundColor: 'var(--cm-selection-match-bg, rgba(97, 175, 239, 0.12))',
    outline: '1px solid var(--cm-selection-match-outline, rgba(97, 175, 239, 0.4))',
    borderRadius: '2px',
    boxShadow: '0 0 0 1px transparent', // Prevent box-shadow stacking issues
  },
  // Primary match at cursor position (slightly more prominent)
  '.cm-selectionMatch-main': {
    backgroundColor: 'var(--cm-selection-match-main-bg, rgba(97, 175, 239, 0.22))',
    outline: '1.5px solid var(--cm-selection-match-main-outline, rgba(97, 175, 239, 0.6))',
    borderRadius: '2px',
    boxShadow: '0 0 0 1px transparent',
  },
  // Prevent visual conflict when search panel is open
  '.cm-searchMatch .cm-selectionMatch': {
    backgroundColor: 'transparent',
    outline: 'none',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Create the word highlights extension.
 *
 * Returns an array of extensions to be added to the editor's extension set.
 * Includes:
 * 1. Custom base theme for `.cm-selectionMatch` and `.cm-selectionMatch-main`
 * 2. `highlightSelectionMatches()` from @codemirror/search with sensible defaults
 *
 * The configured `highlightSelectionMatches()` behavior:
 * - Highlights word around cursor when no text is selected
 * - Requires at least 2 characters to avoid noise (single letters, punctuation)
 * - Limits to 200 matches for performance
 * - Allows partial matches (not whole-word-only)
 *
 * Note: `wholeWords: false` means partial matches are highlighted (e.g. `foo` matches
 * inside `foobar`).
 */
export function wordHighlightsExtension(): Extension {
  return [
    wordHighlightsBaseTheme,
    highlightSelectionMatches({
      highlightWordAroundCursor: true,
      minSelectionLength: 2,
      maxMatches: 200,
      wholeWords: false,
    }),
  ];
}
