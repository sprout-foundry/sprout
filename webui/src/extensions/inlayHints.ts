/**
 * inlayHints.ts — CodeMirror 6 extension for inline inlay hints.
 *
 * Displays type annotations and parameter names inline using
 * CodeMirror Decoration.widget (inline, not block).
 *
 * Implementation approach:
 * - ViewPlugin manages decorations for inlay hints.
 * - On document/viewport changes, debounces recomputation (250ms).
 * - Fetches hints from the semantic API (POST /api/semantic).
 * - Only renders hints for lines in current viewport for performance.
 * - Only runs for TypeScript/Go files (semantic-supported languages).
 * - Toggle via editor settings using a Compartment.
 *
 * Theming:
 * - Uses CSS variables via EditorView.baseTheme().
 * - Distinct styling for type hints vs parameter hints.
 * - Inline decorations with reduced opacity and pointer-events: none.
 */

import { type Extension, Annotation } from '@codemirror/state';
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType } from '@codemirror/view';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import { isLSPClientConnected } from './lspExtensions';

// ── Constants ────────────────────────────────────────────────────────

const DEBOUNCE_MS = 250;

/** Internal annotation used to trigger a view re-render after async hint computation. */
const inlayHintsAnnotation = Annotation.define<boolean>();

/** Languages that support inlay hints via the semantic API. */
function isInlayHintLanguage(languageId: string | undefined): boolean {
  if (!languageId) return false;
  return (
    languageId === 'typescript' ||
    languageId === 'typescript-jsx' ||
    languageId === 'javascript' ||
    languageId === 'javascript-jsx' ||
    languageId === 'go'
  );
}

// ── Types ──────────────────────────────────────────────────────────────

interface InlayHint {
  from: number;
  to: number;
  label: string;
  kind: 'type' | 'parameter' | 'none';
}

// ── Widget Type ───────────────────────────────────────────────────

/**
 * InlayHintWidget — An inline widget displaying an inlay hint.
 */
class InlayHintWidget extends WidgetType {
  constructor(
    private readonly label: string,
    private readonly kind: 'type' | 'parameter' | 'none',
  ) {
    super();
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span');
    const kindClass =
      this.kind === 'type'
        ? 'cm-inlayHint-type'
        : this.kind === 'parameter'
          ? 'cm-inlayHint-parameter'
          : 'cm-inlayHint';
    span.className = `cm-inlayHint ${kindClass}`;
    span.textContent = this.label;
    span.setAttribute('role', 'presentation');
    span.setAttribute('aria-hidden', 'true');
    return span;
  }

  eq(other: InlayHintWidget): boolean {
    return other instanceof InlayHintWidget && this.label === other.label && this.kind === other.kind;
  }

  ignoreEvent(_event: Event): boolean {
    return true;
  }
}

// ── ViewPlugin ─────────────────────────────────────────────────────

/**
 * The inlay hints ViewPlugin class.
 *
 * Manages inline widgets showing type annotations and parameter names.
 */
class InlayHintsPlugin {
  decorations: DecorationSet = Decoration.none;
  private view: EditorView | null;
  private getFilePath: () => string | undefined;
  private getContent: () => string;
  private languageId: string | undefined;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private cachedContent: string = '';
  private cachedHints: InlayHint[] = [];
  private destroyed = false;
  private fetchGeneration = 0;

  constructor(
    view: EditorView,
    getFilePath: () => string | undefined,
    getContent: () => string,
    languageId: string | undefined,
  ) {
    this.view = view;
    this.getFilePath = getFilePath;
    this.getContent = getContent;
    this.languageId = languageId;
    this.cachedContent = view.state.doc.toString();
    this.scheduleUpdate();
  }

  update(update: ViewUpdate): void {
    // Skip re-scheduling when this plugin itself triggered the transaction.
    if (update.transactions.some((t) => t.annotation(inlayHintsAnnotation))) {
      return;
    }
    // Keep view reference current for reconfiguration scenarios.
    this.view = update.view;
    if (update.docChanged || update.viewportChanged || update.transactions.some((t) => t.reconfigured)) {
      this.scheduleUpdate();
    }
  }

  private scheduleUpdate(): void {
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
    }

    this.timeoutId = setTimeout(() => {
      if (this.destroyed || !this.view) return;
      this.decorations = this.buildDecorations();
      this.view.dispatch({
        annotations: [inlayHintsAnnotation.of(true)],
      });
    }, DEBOUNCE_MS);
  }

  private async fetchHints(content: string, filePath: string, languageId: string): Promise<InlayHint[]> {
    try {
      const apiService = ApiService.getInstance();
      const result = await apiService.getSemanticInlayHints(filePath, content, languageId);
      if (result.inlay_hints && Array.isArray(result.inlay_hints)) {
        return result.inlay_hints as InlayHint[];
      }
      return [];
    } catch (err) {
      debugLog('[inlayHints] Failed to fetch hints:', err);
      return [];
    }
  }

  private buildDecorations(): DecorationSet {
    try {
      if (!this.view) return Decoration.none;
      const view = this.view;
      const content = view.state.doc.toString();
      const filePath = this.getFilePath();

      // Only render for supported languages
      if (!isInlayHintLanguage(this.languageId)) {
        return Decoration.none;
      }

      // Only fetch when LSP is connected (same guard as diagnostics)
      if (!this.languageId || !isLSPClientConnected(this.languageId)) {
        return Decoration.none;
      }

      // Only recompute when document has changed; viewport-only changes reuse cache.
      if (content !== this.cachedContent) {
        // Fire-and-forget the fetch; update cache and schedule re-render when it completes.
        if (!filePath || !this.languageId) return Decoration.none;
        const generation = ++this.fetchGeneration;
        void this.fetchHints(content, filePath, this.languageId).then((hints) => {
          if (this.destroyed || !this.view) return;
          // Only apply if a newer fetch hasn't started since this one was issued.
          if (this.fetchGeneration !== generation) return;
          this.cachedHints = hints;
          this.cachedContent = content;
          // Trigger re-render with new hints using current view reference
          this.decorations = this.buildViewportDecorations(this.view);
          this.view.dispatch({
            annotations: [inlayHintsAnnotation.of(true)],
          });
        });
        // Keep existing hints visible while fetching new ones (prevents visual flash).
        return this.buildViewportDecorations(view);
      }

      return this.buildViewportDecorations(view);
    } catch (err) {
      debugLog('[inlayHints] buildDecorations error:', err);
      return Decoration.none;
    }
  }

  private buildViewportDecorations(view: EditorView): DecorationSet {
    const hints = this.cachedHints;
    if (hints.length === 0) {
      return Decoration.none;
    }

    const { from: viewFrom, to: viewTo } = view.viewport;
    const decorations: Array<{ from: number; value: ReturnType<typeof Decoration.widget> }> = [];

    for (const hint of hints) {
      // Only render hints for lines in the current viewport.
      try {
        // Position the widget AFTER the `to` position of each hint.
        const pos = Math.min(hint.to, view.state.doc.length);
        if (pos > viewTo || pos < viewFrom) continue;

        const widget = Decoration.widget({
          widget: new InlayHintWidget(hint.label, hint.kind),
          block: false, // inline, not block
        });
        decorations.push({
          from: pos,
          value: widget,
        });
      } catch (err) {
        debugLog('[inlayHints] Error creating widget:', err);
      }
    }

    if (decorations.length === 0) {
      return Decoration.none;
    }

    // Sort by position (required by Decoration.set)
    decorations.sort((a, b) => a.from - b.from);

    return Decoration.set(
      decorations.map((d) => d.value.range(d.from)),
      true, // already sorted
    );
  }

  destroy(): void {
    this.destroyed = true;
    if (this.timeoutId) {
      clearTimeout(this.timeoutId);
      this.timeoutId = null;
    }
    this.view = null;
    this.cachedHints = [];
    this.cachedContent = '';
  }
}

// ── Base Theme ─────────────────────────────────────────────────────

/**
 * Base theme for inlay hint styling.
 */
const inlayHintsBaseTheme = EditorView.baseTheme({
  '.cm-inlayHint': {
    fontSize: '0.85em',
    color: 'var(--cm-inlay-hint-color, rgba(128, 128, 128, 0.6))',
    padding: '0 4px',
    lineHeight: '1.4',
    whiteSpace: 'nowrap',
    userSelect: 'none',
    pointerEvents: 'none',
    fontFamily: 'var(--editor-font-family, monospace)',
    display: 'inline-block',
    verticalAlign: 'middle',
  },
  '.cm-inlayHint-type': {
    color: 'var(--cm-inlay-hint-type-color, rgba(128, 100, 160, 0.6))',
  },
  '.cm-inlayHint-parameter': {
    color: 'var(--cm-inlay-hint-parameter-color, rgba(100, 140, 128, 0.6))',
  },
  // Dark mode overrides
  '&dark .cm-inlayHint': {
    color: 'var(--cm-inlay-hint-color, rgba(160, 160, 160, 0.5))',
  },
  '&dark .cm-inlayHint-type': {
    color: 'var(--cm-inlay-hint-type-color, rgba(160, 130, 200, 0.5))',
  },
  '&dark .cm-inlayHint-parameter': {
    color: 'var(--cm-inlay-hint-parameter-color, rgba(130, 180, 160, 0.5))',
  },
  // Light mode overrides
  '&light .cm-inlayHint': {
    color: 'var(--cm-inlay-hint-color, rgba(100, 100, 100, 0.5))',
  },
  '&light .cm-inlayHint-type': {
    color: 'var(--cm-inlay-hint-type-color, rgba(100, 80, 130, 0.5))',
  },
  '&light .cm-inlayHint-parameter': {
    color: 'var(--cm-inlay-hint-parameter-color, rgba(80, 110, 100, 0.5))',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Creates a CodeMirror 6 extension for inline inlay hints.
 *
 * @param getFilePath - A getter function that returns the current file path.
 * @param getContent - A getter function that returns the current document content.
 * @param languageId - The language identifier (e.g., "go", "typescript").
 * @returns Extension bundle containing theme and ViewPlugin.
 */
export function inlayHintsExtension(
  getFilePath: () => string | undefined,
  getContent: () => string,
  languageId: string | null | undefined,
): Extension {
  return [
    inlayHintsBaseTheme,
    ViewPlugin.fromClass(
      class extends InlayHintsPlugin {
        constructor(view: EditorView) {
          super(view, getFilePath, getContent, languageId ?? undefined);
        }
      },
      {
        decorations: (v) => v.decorations,
      },
    ),
  ];
}
