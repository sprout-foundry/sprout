/**
 * indentGuides.ts — CodeMirror 6 extension for rendering indent guide lines.
 *
 * Draws subtle vertical lines for each indent level on every visible,
 * indented line.  Guides sit at column positions N, 2N, 3N, … where N is
 * the editor's configured indent unit size.
 *
 * Visual approach:
 * - A ViewPlugin computes `Decoration.widget` markers for each indent
 *   column boundary on every visible, non-empty line.
 * - Each widget is a zero-width `<span>` with a `::after` pseudo-element
 *   that draws the guide line via `position: absolute` relative to
 *   `.cm-line` (which the theme makes `position: relative`).
 * - The widget uses `position: absolute` with `left: auto` so the browser
 *   places it at its "static position" (the correct column in the inline
 *   flow) while spanning the full line height via `top/bottom: 0`.
 *
 * Theming:
 * - Respects `--cm-indent-guide` CSS variable for guide colour.
 * - Falls back to mode-aware defaults when the variable is absent.
 * - Cursor-line guides use `--cm-indent-guide-active` for a brighter look.
 */

import {
  Decoration,
  DecorationSet,
  EditorView,
  ViewPlugin,
  ViewUpdate,
  WidgetType,
} from '@codemirror/view';
import { getIndentUnit } from '@codemirror/language';
import { type Extension } from '@codemirror/state';

// ── Indent helpers ──────────────────────────────────────────────────

/**
 * Measure the visual-column indent of `text`, expanding tabs to `tabSize`
 * columns with proper tab-stop alignment.
 *
 * Returns `0` when `tabSize` is zero or negative.
 */
export function measureIndent(text: string, tabSize: number): number {
  if (tabSize <= 0) return 0;
  let col = 0;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === ' ') {
      col++;
    } else if (ch === '\t') {
      col += tabSize - (col % tabSize);
    } else {
      break;
    }
  }
  return col;
}

// ── Widget type ─────────────────────────────────────────────────────

/**
 * Zero-width widget placed at each indent-guide column boundary.
 * The visible line is drawn entirely by CSS (`::after` pseudo-element).
 */
class IndentGuideWidget extends WidgetType {
  constructor(public active: boolean) {
    super();
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span');
    span.className = this.active
      ? 'cm-indent-guide cm-indent-guide-active'
      : 'cm-indent-guide';
    span.setAttribute('aria-hidden', 'true');
    return span;
  }

  eq(other: WidgetType): boolean {
    return other instanceof IndentGuideWidget && other.active === this.active;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

// Pre-created widget instances (reused by all decorations).
const INACTIVE_WIDGET = new IndentGuideWidget(false);
const ACTIVE_WIDGET = new IndentGuideWidget(true);

function indentGuideDeco(active: boolean): Decoration {
  return Decoration.widget({
    widget: active ? ACTIVE_WIDGET : INACTIVE_WIDGET,
    side: -1,
  });
}

// ── ViewPlugin ──────────────────────────────────────────────────────

const guidePlugin = ViewPlugin.fromClass(
  class IndentGuidePlugin {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = this.buildDecorations(view);
    }

    update(update: ViewUpdate): void {
      if (
        update.viewportChanged ||
        update.docChanged ||
        update.selectionSet ||
        update.transactions.some((t) => t.reconfigured)
      ) {
        this.decorations = this.buildDecorations(update.view);
      }
    }

    /**
     * Build widget decorations for indent guides on all visible lines.
     *
     * For each visible, non-empty line the leading whitespace is scanned
     * and a widget decoration is placed at each indent-unit column boundary:
     *
     * - **Spaces** — widget placed *after* the space that crosses the
     *   boundary (`line.from + i + 1`).
     * - **Tabs** — if a tab spans one or more guide boundaries, the
     *   widget is placed *after* the tab character so it visually aligns
     *   with the next tab-stop column.
     */
    private buildDecorations(view: EditorView): DecorationSet {
      const pieces: { from: number; deco: Decoration }[] = [];
      const tabSize = getIndentUnit(view.state);
      if (tabSize <= 0) return Decoration.none;
      const { from: viewFrom, to: viewTo } = view.viewport;

      // Cursor line for active-guide highlighting.
      const sel = view.state.selection.main;
      const cursorLine = view.state.doc.length > 0
        ? view.state.doc.lineAt(sel.head).number
        : -1;

      // Walk visible lines.
      let pos =
        viewFrom > 0
          ? view.state.doc.lineAt(viewFrom).from
          : view.state.doc.length > 0
            ? 0
            : viewTo;

      while (pos < viewTo && pos < view.state.doc.length) {
        const line = view.state.doc.lineAt(pos);
        const lineText = line.text;

        // Skip blank lines.
        if (lineText.trim().length === 0) {
          pos = line.to + 1;
          continue;
        }

        const indentCol = measureIndent(lineText, tabSize);
        const numGuides = Math.floor(indentCol / tabSize);

        if (numGuides === 0) {
          pos = line.to + 1;
          continue;
        }

        const isActive = line.number === cursorLine;
        const lineFrom = line.from;

        // Walk leading whitespace, tracking visual columns.
        let col = 0;
        let emitted = 0;

        for (
          let i = 0;
          i < lineText.length && emitted < numGuides;
          i++
        ) {
          const ch = lineText[i];

          if (ch === ' ') {
            col++;
            if (col % tabSize === 0 && col > 0) {
              pieces.push({
                from: lineFrom + i + 1,
                deco: indentGuideDeco(isActive),
              });
              emitted++;
            }
          } else if (ch === '\t') {
            // A tab may span multiple guide boundaries.
            const tabEnd = col + (tabSize - (col % tabSize));
            const firstBoundary =
              Math.ceil((col + 1) / tabSize) * tabSize;

            for (
              let g = firstBoundary;
              g <= tabEnd && emitted < numGuides;
              g += tabSize
            ) {
              // Emit only one decoration per position. Multiple boundaries in a
              // single tab all map to the same text offset (after the tab char).
              if (
                pieces.length === 0 ||
                pieces[pieces.length - 1].from !== lineFrom + i + 1
              ) {
                pieces.push({
                  from: lineFrom + i + 1,
                  deco: indentGuideDeco(isActive),
                });
                emitted++;
              }
            }
            col = tabEnd;
          } else {
            break; // reached non-whitespace
          }
        }

        pos = line.to + 1;
      }

      // Decoration.set requires ranges in ascending order.
      pieces.sort((a, b) => a.from - b.from);

      return Decoration.set(
        pieces.map(({ from, deco }) => deco.range(from)),
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
 * Base theme styles for indent guide widgets.
 *
 * `.cm-line` is given `position: relative` so absolutely-positioned guide
 * spans are constrained to the line.  The `::after` pseudo-element draws
 * the visible 1px line.
 *
 * Uses `&dark` / `&light` selectors from `baseTheme` to theme-mode aware
 * fallback colours when `--cm-indent-guide` is not defined.
 */
const indentGuideBaseTheme = EditorView.baseTheme({
  '.cm-line': {
    position: 'relative',
  },
  '.cm-indent-guide': {
    position: 'absolute',
    display: 'block',
    width: '0',
    top: '0',
    bottom: '0',
    pointerEvents: 'none',
    zIndex: '1',
  },
  '.cm-indent-guide::after': {
    content: '""',
    position: 'absolute',
    left: '-0.5px',
    top: '0',
    bottom: '0',
    width: '1px',
    background: 'var(--cm-indent-guide, rgba(128, 128, 128, 0.2))',
  },
  '.cm-indent-guide-active::after': {
    background:
      'var(--cm-indent-guide-active, rgba(128, 128, 128, 0.38))',
  },
  '&dark .cm-indent-guide::after': {
    background: 'var(--cm-indent-guide, rgba(128, 128, 128, 0.15))',
  },
  '&dark .cm-indent-guide-active::after': {
    background:
      'var(--cm-indent-guide-active, rgba(180, 180, 180, 0.35))',
  },
  '&light .cm-indent-guide::after': {
    background: 'var(--cm-indent-guide, rgba(0, 0, 0, 0.1))',
  },
  '&light .cm-indent-guide-active::after': {
    background:
      'var(--cm-indent-guide-active, rgba(0, 0, 0, 0.22))',
  },
});

// ── Public API ──────────────────────────────────────────────────────

/**
 * Returns a CodeMirror extension bundle that renders indent guide lines.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { indentGuidesPlugin } from '../extensions/indentGuides';
 * // …
 * extensions: [..., indentGuidesPlugin(), ...]
 * ```
 */
export function indentGuidesPlugin(): Extension {
  return [indentGuideBaseTheme, guidePlugin];
}
