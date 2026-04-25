/**
 * codeLens.ts — CodeMirror 6 extension for inline code lens reference counts.
 *
 * Displays reference counts above function/method/class/interface definitions.
 * Uses Decoration.widget with block: true to place inline widgets above lines.
 *
 * Implementation approach:
 * - ViewPlugin manages decorations for code lenses.
 * - On document/viewport changes, debounces recomputation (300ms).
 * - Extracts symbols using extractSymbols() from symbolUtils.
 * - Counts references using word-boundary regex.
 * - Only renders lenses for lines in current viewport for performance.
 *
 * Theming:
 * - Uses CSS variables via EditorView.baseTheme().
 * - Falls back to dark/light mode defaults when variables absent.
 */

import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType } from '@codemirror/view';
import { type Extension } from '@codemirror/state';
import { extractSymbols, CONTAINER_KINDS, type SymbolInfo } from '../utils/symbolUtils';
import { debugLog } from '../utils/log';

// ── Constants ────────────────────────────────────────────────────────

const DEBOUNCE_MS = 300;

// ── Widget Type ───────────────────────────────────────────────────

/**
 * CodeLensWidget — A block widget displaying reference count text.
 */
class CodeLensWidget extends WidgetType {
  constructor(private readonly text: string) {
    super();
  }

  toDOM(): HTMLElement {
    const div = document.createElement('div');
    div.className = 'cm-codeLens';
    div.textContent = this.text;
    return div;
  }

  eq(other: CodeLensWidget): boolean {
    return this.text === other.text;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

// ── Helper Functions (exported for testing) ────────────────────────

/**
 * Escape special regex characters in a string.
 */
function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Count references to a symbol name in document content.
 *
 * Uses word-boundary regex to find occurrences of the symbol name,
 * then subtracts 1 for the definition itself.
 *
 * @param content - The document content to search in.
 * @param name - The symbol name to count references for.
 * @returns The number of references (excluding definition), minimum 0.
 */
export function countReferences(content: string, name: string): number {
  if (!name || !content) return 0;

  try {
    const escapedName = escapeRegExp(name);
    // Use more flexible matching: word boundary at start, lookahead for valid word end
    // This handles names with special characters at the end (like test*) better
    const regex = new RegExp(`(?<![\\w])${escapedName}(?![\\w])`, 'g');
    const matches = content.match(regex);
    const count = matches ? matches.length : 0;
    return Math.max(0, count - 1); // Subtract 1 for the definition
  } catch {
    return 0;
  }
}

/**
 * Format reference count as display text.
 *
 * @param count - The reference count.
 * @returns "1 ref" for count === 1, "N refs" otherwise.
 */
export function formatRefText(count: number): string {
  const safe = Math.max(0, count);
  return safe === 1 ? '1 ref' : `${safe} refs`;
}

/**
 * Strip single-line and block comments from source content for accurate
 * reference counting. Does NOT strip string contents (which are valid reference
 * sites for type names, etc.).
 */
function stripComments(content: string): string {
  return content
    .replace(/\/\/.*$/gm, '')       // single-line comments
    .replace(/\/\*[\s\S]*?\*\//g, ''); // block comments
}

/**
 * Compute code lenses for all container symbols in the document.
 *
 * Comments are stripped before reference counting to avoid inflated counts
 * from docstrings and inline comments that mention the symbol name.
 *
 * @param content - The document content.
 * @param languageId - The file extension/language identifier.
 * @returns Array of code lens objects with line, name, kind, and refCount.
 */
export function computeCodeLenses(
  content: string,
  languageId: string | undefined,
): Array<{ line: number; name: string; kind: string; refCount: number }> {
  if (!content) return [];

  const symbols = extractSymbols(content, languageId);
  const lenses: Array<{ line: number; name: string; kind: string; refCount: number }> = [];

  // Filter to container kinds only and process
  const containerSymbols = symbols.filter((s) => CONTAINER_KINDS.has(s.kind));

  // Deduplicate by line (keep first symbol per line)
  const seenLines = new Set<number>();

  // Strip comments for more accurate reference counts
  const strippedContent = stripComments(content);

  for (const sym of containerSymbols) {
    if (seenLines.has(sym.line)) continue;
    seenLines.add(sym.line);

    const refCount = countReferences(strippedContent, sym.name);
    if (refCount > 0) {
      lenses.push({
        line: sym.line,
        name: sym.name,
        kind: sym.kind,
        refCount,
      });
    }
  }

  // Sort by line ascending
  return lenses.sort((a, b) => a.line - b.line);
}

// ── ViewPlugin ─────────────────────────────────────────────────────

/**
 * The code lens ViewPlugin class.
 *
 * Manages inline widgets showing reference counts above function/method/
 * class/interface definition lines.
 */
class CodeLensPlugin {
  decorations: DecorationSet = Decoration.none;
  private view: EditorView;
  private getFileExtension: () => string | undefined;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private cachedContent: string = '';
  private cachedLenses: Array<{ line: number; name: string; kind: string; refCount: number }> = [];
  private cachedDocChanged = false;

  constructor(view: EditorView, getFileExtension: () => string | undefined) {
    this.view = view;
    this.getFileExtension = getFileExtension;
    this.cachedContent = view.state.doc.toString();
    this.scheduleUpdate();
  }

  update(update: ViewUpdate): void {
    if (update.docChanged) {
      this.cachedDocChanged = true;
    }
    if (update.docChanged || update.viewportChanged || update.transactions.some((t) => t.reconfigured)) {
      this.scheduleUpdate();
    }
  }

  /**
   * Schedule a debounced update of decorations.
   */
  private scheduleUpdate(): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
    }

    this.timeoutId = setTimeout(() => {
      this.decorations = this.buildDecorations(this.view);
    }, DEBOUNCE_MS);
  }

  /**
   * Build decorations for code lenses in the current viewport.
   *
   * Caches the computed lenses array and only re-parses symbols when the
   * document content actually changes. Viewport-only changes reuse the
   * cached lenses and just re-filter for visibility.
   */
  private buildDecorations(view: EditorView): DecorationSet {
    try {
      const content = view.state.doc.toString();
      const languageId = this.getFileExtension();

      // Only recompute lenses when the document has changed.
      // Viewport-only changes reuse the cached results.
      if (this.cachedDocChanged || content !== this.cachedContent) {
        this.cachedLenses = computeCodeLenses(content, languageId);
        this.cachedContent = content;
        this.cachedDocChanged = false;
      }

      const lenses = this.cachedLenses;
      if (lenses.length === 0) {
        return Decoration.none;
      }

      const { from: viewFrom, to: viewTo } = view.viewport;
      const decorations: Array<{ from: number; value: ReturnType<typeof Decoration.widget> }> = [];

      for (const lens of lenses) {
        // Only render lenses for lines in the current viewport.
        // Compare line's character range (from/to) against viewport character range.
        try {
          const lineInfo = view.state.doc.line(lens.line);
          if (lineInfo.from > viewTo || lineInfo.to < viewFrom) continue;

          const widget = Decoration.widget({
            widget: new CodeLensWidget(formatRefText(lens.refCount)),
            block: true,
          });
          decorations.push({
            from: lineInfo.from,
            value: widget,
          });
        } catch (err) {
          debugLog('[codeLens] Error creating widget for line', lens.line, err);
        }
      }

      // Sort by position (required by Decoration.set)
      decorations.sort((a, b) => a.from - b.from);

      return Decoration.set(
        decorations.map((d) => d.value.range(d.from)),
        true, // already sorted
      );
    } catch (err) {
      debugLog('[codeLens] buildDecorations error:', err);
      return Decoration.none;
    }
  }

  /**
   * Destroy the plugin: clear any pending timeout.
   */
  destroy(): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
      this.timeoutId = null;
    }
    this.cachedLenses = [];
  }
}

// ── Base Theme ─────────────────────────────────────────────────────

/**
 * Base theme for code lens styling.
 */
const codeLensBaseTheme = EditorView.baseTheme({
  '.cm-codeLens': {
    fontSize: '0.8em',
    color: 'var(--cm-code-lens-color, rgba(128, 128, 128, 0.7))',
    padding: '0 8px',
    lineHeight: '1.4',
    whiteSpace: 'nowrap',
    userSelect: 'none',
    cursor: 'default',
    fontFamily: 'var(--editor-font-family, monospace)',
  },
  '.cm-codeLens:hover': {
    color: 'var(--cm-code-lens-color-hover, rgba(160, 160, 160, 0.9))',
  },
  // Dark mode overrides
  '&dark .cm-codeLens': {
    color: 'var(--cm-code-lens-color, rgba(160, 160, 160, 0.6))',
  },
  '&dark .cm-codeLens:hover': {
    color: 'var(--cm-code-lens-color-hover, rgba(200, 200, 200, 0.8))',
  },
  // Light mode overrides
  '&light .cm-codeLens': {
    color: 'var(--cm-code-lens-color, rgba(100, 100, 100, 0.7))',
  },
  '&light .cm-codeLens:hover': {
    color: 'var(--cm-code-lens-color-hover, rgba(60, 60, 60, 0.9))',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Creates a CodeMirror 6 extension for inline code lenses.
 *
 * @param getFileExtension - A getter function that returns the current file extension
 *                         (e.g., ".go", ".ts", ".js").
 * @returns Extension bundle containing theme and ViewPlugin.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { codeLensPlugin } from '../extensions/codeLens';
 * // ...
 * extensions: [..., codeLensPlugin(() => buffer?.file?.ext), ...]
 * ```
 */
export function codeLensPlugin(getFileExtension: () => string | undefined): Extension {
  return [
    codeLensBaseTheme,
    ViewPlugin.fromClass(
      class extends CodeLensPlugin {
        constructor(view: EditorView) {
          super(view, getFileExtension);
        }
      },
      {
        decorations: (v) => v.decorations,
      },
    ),
  ];
}

// Re-export types for testing
export type { SymbolInfo };
