/**
 * stickyScroll.ts — CodeMirror 6 extension for sticky scroll headers.
 *
 * Displays a pinned bar at the top of the editor when scrolling past
 * function/class/interface definitions, showing the enclosing scope chain
 * (e.g., "MyClass › myMethod").
 *
 * Implementation approach:
 * - ViewPlugin manages a persistent DOM element appended to the scroller.
 * - On each viewport/scroll change, recalculates enclosing symbols.
 * - Symbol extraction is cached and only re-parsed on document changes.
 * - Clicking a scope name scrolls the editor to that line.
 *
 * Theming:
 * - Uses CSS variables via EditorView.baseTheme().
 * - Falls back to dark/light mode defaults when variables absent.
 */

import { EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';
import { type Extension } from '@codemirror/state';
import { extractSymbols, getEnclosingSymbols, type SymbolInfo } from '../components/GoToSymbolOverlay';

// ── Constants ────────────────────────────────────────────────────────

const MAX_SCOPES_DISPLAY = 3;

// ── Helper Function (exported for testing) ───────────────────────────

/**
 * Compute the sticky scope chain for a given viewport top line.
 *
 * Extracts symbols and finds those enclosing the specified line.
 * Returns up to MAX_SCOPES_DISPLAY (3) levels, sorted outermost → innermost.
 *
 * @param content - The document content.
 * @param fileExtension - The file extension (e.g., ".go", ".ts").
 * @param topLine - The 1-based line number at the top of the viewport.
 * @returns Array of enclosing SymbolInfo objects (up to 3).
 */
export function computeStickyScopes(
  content: string,
  fileExtension: string | undefined,
  topLine: number,
): SymbolInfo[] {
  if (!content || topLine < 1) {
    return [];
  }

  // getEnclosingSymbols returns up to 3 levels by default
  const scopes = getEnclosingSymbols(content, fileExtension, topLine);
  return scopes.slice(0, MAX_SCOPES_DISPLAY);
}

// ── ViewPlugin ────────────────────────────────────────────────────────

/**
 * The sticky scroll ViewPlugin class.
 *
 * Manages a sticky header DOM element that displays the enclosing
 * function/class/interface scope chain based on the viewport position.
 */
class StickyScrollPlugin {
  private domElement: HTMLElement | null = null;
  private cachedSymbols: SymbolInfo[] = [];
  private lastContentHash = '';
  private fileExtension: string | undefined;

  constructor(
    readonly view: EditorView,
    readonly getFileExtension: () => string | undefined,
  ) {
    // Initialize file extension from getter
    this.fileExtension = getFileExtension();

    // Initialize DOM element
    this.createDOMElement();

    // Initial symbol extraction
    this.updateSymbols();

    // Initial render
    this.render();
  }

  /**
   * Create the sticky header DOM element and append to the scroller.
   */
  private createDOMElement(): void {
    const dom = document.createElement('div');
    dom.className = 'sticky-scroll-header';
    dom.style.display = 'none'; // Hidden by default until scopes are found

    // Append as the first child of the scroller container
    const scroller = this.view.scrollDOM;
    if (scroller.firstChild) {
      scroller.insertBefore(dom, scroller.firstChild);
    } else {
      scroller.appendChild(dom);
    }

    this.domElement = dom;
  }

  /**
   * Update cached symbols if the document has changed.
   */
  private updateSymbols(): void {
    const doc = this.view.state.doc;
    const content = doc.toString();

    // Simple hash for change detection (skip full parse when unchanged)
    const contentHash = content.slice(0, 10000) + content.length;
    if (contentHash === this.lastContentHash && this.cachedSymbols.length > 0) {
      return;
    }

    this.lastContentHash = contentHash;

    // Re-extract symbols only when document changes
    this.cachedSymbols = extractSymbols(content, this.fileExtension);
  }

  /**
   * Update the sticky header display based on current viewport.
   */
  update(update: ViewUpdate): void {
    // Update file extension from getter (for when it's re-initialized)
    this.fileExtension = this.getFileExtension();

    // Re-extract symbols on document changes
    if (update.docChanged) {
      this.updateSymbols();
    }

    // Re-render on viewport or selection changes
    if (update.viewportChanged || update.selectionSet || update.docChanged) {
      this.render();
    }
  }

  /**
   * Render the sticky header based on current viewport position.
   */
  private render(): void {
    if (!this.domElement) return;

    const dom = this.domElement;
    const view = this.view;
    const doc = view.state.doc;

    // Guard: empty document
    if (doc.length === 0) {
      dom.style.display = 'none';
      dom.innerHTML = '';
      return;
    }

    // Get the top line of the viewport (1-based)
    const topLineNumber = view.state.doc.lineAt(view.viewport.from).number;

    // Compute enclosing scopes at the viewport top
    const scopes = computeStickyScopes(
      doc.toString(),
      this.fileExtension,
      topLineNumber,
    );

    // No scope or header visible — hide and clear
    if (scopes.length === 0) {
      dom.style.display = 'none';
      dom.innerHTML = '';
      return;
    }

    // Get the outermost symbol's line
    const outermostScope = scopes[0];

    // Hide if the outermost symbol's header is still visible in the viewport
    // (i.e., user hasn't scrolled past it yet)
    if (outermostScope.line >= topLineNumber) {
      dom.style.display = 'none';
      dom.innerHTML = '';
      return;
    }

    // Build the scope chain HTML
    const html = this.buildScopeChainHTML(scopes);
    dom.innerHTML = html;

    // Re-attach click handlers after innerHTML update
    this.attachClickHandlers(dom, view);

    // Show the element
    dom.style.display = 'block';
  }

  /**
   * Build HTML for the scope chain display.
   */
  private buildScopeChainHTML(scopes: SymbolInfo[]): string {
    const scopeElements: string[] = [];

    for (let i = 0; i < scopes.length; i++) {
      const scope = scopes[i];

      // Add separator before subsequent scopes
      if (i > 0) {
        scopeElements.push('<span class="sticky-scope-separator">›</span>');
      }

      // Add the scope as a clickable span
      const escapedName = scope.name.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
      scopeElements.push(
        `<span class="sticky-scope" data-line="${scope.line}" data-name="${escapedName}">${escapedName}</span>`,
      );
    }

    return scopeElements.join('');
  }

  /**
   * Attach click handlers to scope spans for navigation.
   */
  private attachClickHandlers(dom: HTMLElement, view: EditorView): void {
    const scopeSpans = dom.querySelectorAll('.sticky-scope');
    scopeSpans.forEach((span) => {
      const element = span as HTMLElement;
      const lineAttr = element.getAttribute('data-line');
      if (!lineAttr) return;

      const targetLine = parseInt(lineAttr, 10);
      if (isNaN(targetLine)) return;

      element.addEventListener('click', () => {
        this.navigateToLine(view, targetLine);
      });

      // Change cursor to pointer
      element.style.cursor = 'pointer';
    });
  }

  /**
   * Navigate the editor to a specific line.
   */
  private navigateToLine(view: EditorView, lineNumber: number): void {
    const doc = view.state.doc;

    // Validate line number
    if (lineNumber < 1 || lineNumber > doc.lines) {
      return;
    }

    const line = doc.line(lineNumber);
    view.dispatch({
      selection: { anchor: line.from },
      scrollIntoView: true,
    });
    view.focus();
  }

  /**
   * Destroy the plugin: remove the DOM element.
   */
  destroy(): void {
    if (this.domElement && this.domElement.parentNode) {
      this.domElement.parentNode.removeChild(this.domElement);
    }
    this.domElement = null;
  }
}

// ── Base Theme ────────────────────────────────────────────────────────────

/**
 * Base theme for sticky scroll styling.
 *
 * Uses CSS variables with dark/light mode fallbacks matching the
 * project's established theme pattern.
 */
const stickyScrollBaseTheme = EditorView.baseTheme({
  '.sticky-scroll-header': {
    position: 'sticky',
    top: '0',
    zIndex: '5',
    background: 'var(--cm-sticky-scroll-bg, rgba(46, 52, 64, 0.95))',
    color: 'var(--cm-sticky-scroll-fg, rgba(255, 255, 255, 0.9))',
    fontSize: '0.85em',
    padding: '2px 16px 2px calc(var(--cm-gutter-width, 50px) + 16px)',
    borderBottom: '1px solid var(--cm-sticky-scroll-border, rgba(128, 128, 128, 0.2))',
    cursor: 'default',
    userSelect: 'none',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    pointerEvents: 'auto',
  },
  '.sticky-scope': {
    cursor: 'pointer',
    opacity: '0.8',
  },
  '.sticky-scope:hover': {
    opacity: '1',
    textDecoration: 'underline',
  },
  '.sticky-scope-separator': {
    margin: '0 6px',
    opacity: '0.5',
  },
  // Dark mode overrides
  '&dark .sticky-scroll-header': {
    background: 'var(--cm-sticky-scroll-bg, rgba(30, 30, 30, 0.95))',
    color: 'var(--cm-sticky-scroll-fg, rgba(200, 200, 200, 0.9))',
    borderBottom: '1px solid var(--cm-sticky-scroll-border, rgba(100, 100, 100, 0.3))',
  },
  // Light mode overrides
  '&light .sticky-scroll-header': {
    background: 'var(--cm-sticky-scroll-bg, rgba(250, 250, 250, 0.95))',
    color: 'var(--cm-sticky-scroll-fg, rgba(30, 30, 30, 0.9))',
    borderBottom: '1px solid var(--cm-sticky-scroll-border, rgba(0, 0, 0, 0.1))',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Creates a CodeMirror 6 extension for sticky scroll headers.
 *
 * @param getFileExtension - A getter function that returns the current file extension
 *                         (e.g., ".go", ".ts", ".js").
 * @returns Extension bundle containing theme and ViewPlugin.
 *
 * Include in the editor's extensions array:
 * ```ts
 * import { stickyScrollPlugin } from '../extensions/stickyScroll';
 * // ...
 * extensions: [..., stickyScrollPlugin(() => buffer?.file?.ext), ...]
 * ```
 */
export function stickyScrollPlugin(getFileExtension: () => string | undefined): Extension {
  return [
    stickyScrollBaseTheme,
    ViewPlugin.fromClass(class extends StickyScrollPlugin {
      constructor(view: EditorView) {
        super(view, getFileExtension);
      }
    }),
  ];
}

// Also export the computeStickyScopes for testing
export type { SymbolInfo };