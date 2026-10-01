/**
 * codeLens.ts — CodeMirror 6 extension for inline code lens reference counts.
 *
 * Displays reference counts above function/method/class/interface definitions.
 * Block widgets sit above definition lines. They live in a StateField (block
 * decorations may not come from a ViewPlugin); a ViewPlugin debounces the
 * recompute (300ms) after document changes and publishes via an effect.
 * - Extracts symbols using extractSymbols() from symbolUtils.
 * - Counts references using word-boundary regex.
 *
 * Theming:
 * - Uses CSS variables via EditorView.baseTheme().
 * - Falls back to dark/light mode defaults when variables absent.
 */

import { type Extension, StateEffect, StateField, type Text } from '@codemirror/state';
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType } from '@codemirror/view';
import { debugLog } from '../utils/log';
import { extractSymbols, CONTAINER_KINDS, type SymbolInfo } from '../utils/symbolUtils';

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
    div.setAttribute('role', 'presentation');
    div.setAttribute('aria-hidden', 'true');
    return div;
  }

  eq(other: CodeLensWidget): boolean {
    return this.text === other.text;
  }

  ignoreEvent(_event: Event): boolean {
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
 * reference counting. Handles strings correctly so that `//` inside string
 * literals (URLs, file paths, etc.) is not treated as a comment start.
 */
function stripComments(content: string, languageId?: string): string {
  const isPython = languageId === '.py' || languageId === '.pyw' || languageId === '.pyi';
  const lines = content.split('\n');
  let inBlockComment = false;
  let inString: string | null = null; // '"' | "'" | '`' | null

  return lines
    .map((line) => {
      let result = '';
      for (let i = 0; i < line.length; i++) {
        const ch = line[i];

        // Inside a string — pass through, handle escape sequences
        if (inString) {
          result += ch;
          if (ch === '\\' && i + 1 < line.length) {
            result += line[++i];
            continue;
          }
          if (ch === inString) inString = null;
          continue;
        }

        // Inside a block comment — look for */
        if (inBlockComment) {
          if (ch === '*' && line[i + 1] === '/') {
            inBlockComment = false;
            i++; // skip /
          }
          continue;
        }

        // Check for comment start (only outside strings)
        if (ch === '/' && line[i + 1] === '/') break; // rest of line is comment
        if (ch === '/' && line[i + 1] === '*') {
          inBlockComment = true;
          i++;
          continue;
        }

        // Python # comments (only outside strings and block comments)
        if (isPython && ch === '#') break;

        // Check for string start
        if (ch === '"' || ch === "'" || ch === '`') inString = ch;

        result += ch;
      }
      return result;
    })
    .join('\n');
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
  const strippedContent = stripComments(content, languageId);

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

// ── State ──────────────────────────────────────────────────────────

/**
 * Block widgets must come from state, not a ViewPlugin — CodeMirror throws
 * when a plugin supplies them. The field maps the set through every edit so
 * positions stay valid during the recompute debounce.
 */
const setCodeLensDecorations = StateEffect.define<DecorationSet>();

const codeLensField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(decorations, tr) {
    let next = decorations.map(tr.changes);
    for (const effect of tr.effects) {
      if (effect.is(setCodeLensDecorations)) next = effect.value;
    }
    return next;
  },
  provide: (field) => EditorView.decorations.from(field),
});

/** Build the block-widget set for every lens; out-of-range lines are skipped. */
export function buildCodeLensDecorations(doc: Text, lenses: Array<{ line: number; refCount: number }>): DecorationSet {
  const ranges = [];
  for (const lens of lenses) {
    if (lens.line < 1 || lens.line > doc.lines) continue;
    const widget = Decoration.widget({
      widget: new CodeLensWidget(formatRefText(lens.refCount)),
      block: true,
      side: -1,
    });
    ranges.push(widget.range(doc.line(lens.line).from));
  }
  return Decoration.set(ranges, true);
}

// ── ViewPlugin ─────────────────────────────────────────────────────

/**
 * Debounces lens recomputation after document changes and publishes the
 * result to codeLensField.
 */
class CodeLensPlugin {
  private view: EditorView;
  private getFileExtension: () => string | undefined;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private cachedContent: string | null = null;
  private cachedLanguage: string | undefined;
  private destroyed = false;

  constructor(view: EditorView, getFileExtension: () => string | undefined) {
    this.view = view;
    this.getFileExtension = getFileExtension;
    this.scheduleUpdate();
  }

  update(update: ViewUpdate): void {
    if (update.docChanged || update.transactions.some((t) => t.reconfigured)) {
      this.scheduleUpdate();
    }
  }

  private scheduleUpdate(): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
    }
    this.timeoutId = setTimeout(() => {
      this.timeoutId = null;
      if (this.destroyed) return;
      this.publish();
    }, DEBOUNCE_MS);
  }

  private publish(): void {
    try {
      const { doc } = this.view.state;
      const content = doc.toString();
      const languageId = this.getFileExtension();
      if (content === this.cachedContent && languageId === this.cachedLanguage) return;
      this.cachedContent = content;
      this.cachedLanguage = languageId;
      const decorations = buildCodeLensDecorations(doc, computeCodeLenses(content, languageId));
      this.view.dispatch({ effects: setCodeLensDecorations.of(decorations) });
    } catch (err) {
      debugLog('[codeLens] update error:', err);
    }
  }

  destroy(): void {
    this.destroyed = true;
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
      this.timeoutId = null;
    }
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
    codeLensField,
    ViewPlugin.fromClass(
      class extends CodeLensPlugin {
        constructor(view: EditorView) {
          super(view, getFileExtension);
        }
      },
    ),
  ];
}

// Re-export types for testing
export type { SymbolInfo };
