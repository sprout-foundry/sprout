/**
 * whitespaceRendering.ts — CodeMirror 6 extension for rendering whitespace characters.
 *
 * Renders whitespace characters as visible symbols:
 * - Tab characters → rendered as → (right arrow)
 * - Space characters → rendered as · (middle dot)
 * - Trailing spaces in "boundary" mode → rendered as · (middle dot)
 *
 * Supports three rendering modes:
 * - "none" - no whitespace rendered (default)
 * - "boundary" - only render tab characters and trailing whitespace
 * - "all" - render all whitespace (leading, internal, and trailing)
 *
 * Theming:
 * - Uses `--cm-whitespace-char` CSS variable for tab characters.
 * - Uses `--cm-whitespace-space` CSS variable for space dots.
 * - Falls back to mode-aware defaults when variables are absent.
 */

import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType } from '@codemirror/view';
import { type Extension } from '@codemirror/state';

// ── Types ───────────────────────────────────────────────────────────────────

export type WhitespaceRenderingMode = 'none' | 'boundary' | 'all';

// ── Whitespace detection helpers ────────────────────────────────────────

/**
 * Find whitespace positions in a line of text based on the rendering mode.
 *
 * @param lineText — The text content of the line (without trailing newline).
 * @param mode — The rendering mode (none, boundary, or all).
 * @returns Array of objects with from/to positions and whitespace type.
 */
export function findWhitespacePositions(
  lineText: string,
  mode: WhitespaceRenderingMode,
): Array<{ from: number; to: number; type: 'tab' | 'space' | 'trailing' }> {
  const positions: Array<{ from: number; to: number; type: 'tab' | 'space' | 'trailing' }> = [];

  if (mode === 'none' || lineText.length === 0) {
    return positions;
  }

  // Find trailing whitespace start position (boundary between content and trailing)
  let trailingStart = -1;
  for (let i = lineText.length - 1; i >= 0; i--) {
    const ch = lineText[i];
    if (ch !== ' ' && ch !== '\t' && ch !== '\r') {
      trailingStart = i + 1;
      break;
    }
  }

  // If line is all whitespace, everything counts as trailing (no non-whitespace content)
  if (trailingStart === -1 && lineText.length > 0) {
    trailingStart = 0;
  }

  // Scan the line for whitespace
  let i = 0;
  while (i < lineText.length) {
    const ch = lineText[i];

    if (ch === '\t') {
      // Tab character - always rendered in boundary and all modes
      if (mode === 'all' || mode === 'boundary') {
        positions.push({
          from: i,
          to: i + 1,
          type: 'tab',
        });
      }
      i++;
    } else if (ch === ' ' || ch === '\r') {
      // Space character - determine if it's trailing
      const isTrailing = trailingStart >= 0 && i >= trailingStart;

      if (mode === 'all') {
        // Render all spaces as whitespace
        positions.push({
          from: i,
          to: i + 1,
          type: 'space',
        });
      } else if (mode === 'boundary') {
        // In boundary mode, render trailing spaces (spaces after content)
        // OR if the entire line is whitespace (all are trailing)
        if (isTrailing || (trailingStart === 0)) {
          positions.push({
            from: i,
            to: i + 1,
            type: 'trailing',
          });
        }
      }
      i++;
    } else {
      // Non-whitespace character
      i++;
    }
  }

  return positions;
}

// ── Widget for tab character replacement ────────────────────────────────

/**
 * Widget that renders a visible character (→ for tabs) in place of the whitespace.
 */
class WhitespaceCharWidget extends WidgetType {
  constructor(private char: string) {
    super();
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span');
    span.className = 'cm-whitespace-char';
    span.textContent = this.char;
    return span;
  }

  eq(other: WhitespaceCharWidget): boolean {
    return this.char === other.char;
  }
}

// ── Pre-built decorations (reused for performance) ───────────────────

const spaceMark = Decoration.mark({ class: 'cm-whitespace-space' });
const trailingSpaceMark = Decoration.mark({ class: 'cm-whitespace-trailing' });



// ── ViewPlugin for decoration building ────────────────────────────────

const createWhitespacePlugin = (mode: WhitespaceRenderingMode) =>
  ViewPlugin.fromClass(
    class WhitespaceRenderingPlugin {
      decorations: DecorationSet;

      constructor(view: EditorView) {
        this.decorations = this.buildDecorations(view);
      }

      update(update: ViewUpdate): void {
        if (update.viewportChanged || update.docChanged || update.transactions.some((t) => t.reconfigured)) {
          this.decorations = this.buildDecorations(update.view);
        }
      }

      private buildDecorations(view: EditorView): DecorationSet {
        if (mode === 'none') {
          return Decoration.none;
        }

        const pieces: Array<{ from: number; to: number; decoration: any }> = [];
        const { from: viewFrom, to: viewTo } = view.viewport;

        if (view.state.doc.length === 0 || viewFrom >= view.state.doc.length) {
          return Decoration.none;
        }

        let pos = viewFrom > 0 ? view.state.doc.lineAt(viewFrom).from : 0;

        while (pos < viewTo && pos < view.state.doc.length) {
          const line = view.state.doc.lineAt(pos);
          const lineText = line.text;
          const whitespacePositions = findWhitespacePositions(lineText, mode);

          for (const wp of whitespacePositions) {
            const from = line.from + wp.from;
            const to = line.from + wp.to;

            if (wp.type === 'tab') {
              // Use widget for tab character replacement — must call .range() to attach positions
              pieces.push({
                from,
                to,
                decoration: Decoration.replace({ widget: new WhitespaceCharWidget('→'), block: false }).range(from, to),
              });
            } else if (wp.type === 'space') {
              // Use mark decoration for spaces in 'all' mode
              pieces.push({
                from,
                to,
                decoration: spaceMark.range(from, to),
              });
            } else if (wp.type === 'trailing') {
              // Use mark decoration for trailing spaces in 'boundary' mode
              pieces.push({
                from,
                to,
                decoration: trailingSpaceMark.range(from, to),
              });
            }
          }

          pos = line.to + 1;
        }

        pieces.sort((a, b) => a.from - b.from);

        return Decoration.set(
          pieces.map(({ decoration }) => decoration),
          true,
        );
      }
    },
    {
      decorations: (v) => v.decorations,
    },
  );

// ── Theme (base) ────────────────────────────────────────────────────────

/**
 * Base theme styles for whitespace rendering.
 *
 * Uses `&dark` / `&light` selectors from `baseTheme` to provide
 * theme-mode-aware fallback colours.
 */
const whitespaceRenderingBaseTheme = EditorView.baseTheme({
  '.cm-whitespace-char': {
    color: 'var(--cm-whitespace-char, rgba(128, 128, 128, 0.4))',
    fontSize: '0.85em',
    lineHeight: '1',
  },
  '.cm-whitespace-space': {
    position: 'relative',
  },
  '&light .cm-whitespace-space::after': {
    content: "'·'",
    position: 'absolute',
    left: '0',
    top: '0',
    width: '100%',
    textAlign: 'center',
    color: 'var(--cm-whitespace-space, rgba(128, 128, 128, 0.35))',
    fontSize: '0.7em',
    lineHeight: '1',
    pointerEvents: 'none',
  },
  '&dark .cm-whitespace-space::after': {
    content: "'·'",
    position: 'absolute',
    left: '0',
    top: '0',
    width: '100%',
    textAlign: 'center',
    color: 'var(--cm-whitespace-space, rgba(180, 180, 180, 0.4))',
    fontSize: '0.7em',
    lineHeight: '1',
    pointerEvents: 'none',
  },
  '.cm-whitespace-trailing': {
    position: 'relative',
  },
  '&light .cm-whitespace-trailing::after': {
    content: "'·'",
    position: 'absolute',
    left: '0',
    top: '0',
    width: '100%',
    textAlign: 'center',
    color: 'var(--cm-whitespace-space, rgba(255, 80, 80, 0.5))',
    fontSize: '0.7em',
    lineHeight: '1',
    pointerEvents: 'none',
  },
  '&dark .cm-whitespace-trailing::after': {
    content: "'·'",
    position: 'absolute',
    left: '0',
    top: '0',
    width: '100%',
    textAlign: 'center',
    color: 'var(--cm-whitespace-space, rgba(255, 100, 100, 0.6))',
    fontSize: '0.7em',
    lineHeight: '1',
    pointerEvents: 'none',
  },
});

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension bundle for rendering whitespace characters.
 *
 * @param mode — The rendering mode: 'none', 'boundary', or 'all'.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { whitespaceRenderingPlugin } from '../extensions/whitespaceRendering';
 * // …
 * extensions: [..., whitespaceRenderingPlugin('boundary'), ...]
 * ```
 *
 * To change the mode dynamically, use the compartment:
 * ```ts
 * view.dispatch({
 *   effects: whitespaceCompartment.reconfigure(whitespaceRenderingPlugin(newMode))
 * });
 * ```
 */
export function whitespaceRenderingPlugin(mode: WhitespaceRenderingMode): Extension {
  return [
    whitespaceRenderingBaseTheme,
    createWhitespacePlugin(mode),
  ];
}