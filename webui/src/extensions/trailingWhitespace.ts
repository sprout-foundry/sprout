/**
 * trailingWhitespace.ts — CodeMirror 6 extension for highlighting trailing whitespace.
 *
 * Highlights spaces and tabs at the end of each line (before the newline)
 * with a subtle background color. Only processes visible lines for performance
 * on large files.
 *
 * Theming:
 * - Uses `--cm-trailing-whitespace` CSS variable for the background color.
 * - Falls back to mode-aware defaults when the variable is absent.
 */

import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';
import { type Extension } from '@codemirror/state';

// ── Whitespace detection ────────────────────────────────────────────

/**
 * Find the start position of trailing whitespace in a line's text.
 *
 * Scans backwards from the end of the line content (not including
 * the newline character) and returns the index where trailing
 * whitespace begins. Returns -1 if there is no trailing whitespace.
 *
 * @param lineText — The text content of the line (without trailing newline).
 * @returns The 0-based index where trailing whitespace starts, or -1 if none.
 */
export function findTrailingWhitespaceStart(lineText: string): number {
  // Start from the end and work backwards.
  for (let i = lineText.length - 1; i >= 0; i--) {
    const ch = lineText[i];
    if (ch !== ' ' && ch !== '\t' && ch !== '\r') {
      // Found a non-whitespace character.
      // If we're at the end of the string, there's no trailing whitespace.
      if (i === lineText.length - 1) {
        return -1;
      }
      // Otherwise, trailing whitespace starts at i + 1.
      return i + 1;
    }
  }

  // All characters are whitespace — this is a whitespace-only line.
  // Return 0 to highlight the entire line.
  return lineText.length > 0 ? 0 : -1;
}

// ── Pre-built decoration ────────────────────────────────────────────

/** Mark decoration applied to trailing whitespace. Reused across all decorations. */
const trailingWhitespaceMark = Decoration.mark({ class: 'cm-trailing-whitespace' });

// ── ViewPlugin ──────────────────────────────────────────────────────

const trailingWhitespaceViewPlugin = ViewPlugin.fromClass(
  class TrailingWhitespacePlugin {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = this.buildDecorations(view);
    }

    update(update: ViewUpdate): void {
      // Rebuild on viewport changes, document changes, or reconfiguration.
      if (update.viewportChanged || update.docChanged || update.transactions.some((t) => t.reconfigured)) {
        this.decorations = this.buildDecorations(update.view);
      }
    }

    /**
     * Build decorations for trailing whitespace on all visible lines.
     *
     * For each visible line, scans from the end of the line content
     * (before newline) backwards to find trailing spaces/tabs, then
     * creates a mark decoration covering that region.
     */
    private buildDecorations(view: EditorView): DecorationSet {
      const pieces: { from: number; to: number }[] = [];
      const { from: viewFrom, to: viewTo } = view.viewport;

      // Handle empty document case.
      if (view.state.doc.length === 0) {
        return Decoration.none;
      }

      // Walk visible lines.
      let pos = viewFrom > 0 ? view.state.doc.lineAt(viewFrom).from : 0;

      while (pos < viewTo && pos < view.state.doc.length) {
        const line = view.state.doc.lineAt(pos);
        const lineText = line.text;

        // Find trailing whitespace: scan backwards from end of line content.
        const trailingStart = findTrailingWhitespaceStart(lineText);

        if (trailingStart !== -1) {
          // Create decoration from start of trailing whitespace to end of line content.
          pieces.push({
            from: line.from + trailingStart,
            to: line.to,
          });
        }

        pos = line.to + 1;
      }

      // Sort pieces by position (required by Decoration.set).
      pieces.sort((a, b) => a.from - b.from);

      return Decoration.set(
        pieces.map(({ from, to }) => trailingWhitespaceMark.range(from, to)),
        true, // already sorted
      );
    }
  },
  {
    decorations: (v) => v.decorations,
  },
);

// ── Theme (base) ────────────────────────────────────────────────────

/**
 * Base theme styles for trailing whitespace highlighting.
 *
 * Uses `&dark` / `&light` selectors from `baseTheme` to provide
 * theme-mode-aware fallback colours when `--cm-trailing-whitespace`
 * is not defined.
 */
const trailingWhitespaceBaseTheme = EditorView.baseTheme({
  '.cm-trailing-whitespace': {
    background: 'var(--cm-trailing-whitespace, rgba(255, 80, 80, 0.12))',
    borderRadius: '2px',
  },
  '&dark .cm-trailing-whitespace': {
    background: 'var(--cm-trailing-whitespace, rgba(255, 80, 80, 0.12))',
  },
  '&light .cm-trailing-whitespace': {
    background: 'var(--cm-trailing-whitespace, rgba(255, 80, 80, 0.15))',
  },
});

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension bundle that highlights trailing whitespace.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { trailingWhitespacePlugin } from '../extensions/trailingWhitespace';
 * // …
 * extensions: [..., trailingWhitespacePlugin(), ...]
 * ```
 */
export function trailingWhitespacePlugin(): Extension {
  return [trailingWhitespaceBaseTheme, trailingWhitespaceViewPlugin];
}
