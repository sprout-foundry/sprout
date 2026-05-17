/**
 * errorLens.ts — CodeMirror 6 extension for inline diagnostic messages.
 *
 * Shows diagnostic messages (error, warning, info, hint) inline at the end
 * of the line where the diagnostic occurs. Inspired by VS Code Error Lens.
 *
 * Uses a debounced ViewPlugin that reads diagnostics from @codemirror/lint
 * state via forEachDiagnostic and creates inline widgets at line.to positions.
 *
 * Decorations are recomputed when:
 * - Document content changes
 * - Viewport scrolls
 * - Diagnostics are pushed/cleared (detected via diagnosticCount change)
 *
 * Exported factory: {@link errorLensPlugin}
 */

import { forEachDiagnostic, diagnosticCount, type Diagnostic } from '@codemirror/lint';
import { type Extension, Annotation } from '@codemirror/state';
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType } from '@codemirror/view';

import './errorLens.css';
import { debugLog } from '../utils/log';

// ── Constants ────────────────────────────────────────────────────────

const DEBOUNCE_MS = 150;
const MAX_MESSAGE_LENGTH = 120;
const MAX_COMBINED_LENGTH = 200;
const DIAGNOSTIC_SEPARATOR = ' \u2022 ';

/** Internal annotation used to identify re-render dispatches from this plugin. */
const errorLensAnnotation = Annotation.define<boolean>();

// ── Widget Type ───────────────────────────────────────────────────

/** Inline widget that renders a diagnostic message at the end of a line. */
class ErrorLensWidget extends WidgetType {
  constructor(
    private readonly message: string,
    private readonly severity: string,
  ) {
    super();
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span');
    span.className = `cm-errorLens cm-errorLens-${this.severity}`;
    span.textContent = `\u2002${this.message}`;
    span.setAttribute('aria-hidden', 'true');
    return span;
  }

  eq(other: ErrorLensWidget): boolean {
    return this.message === other.message && this.severity === other.severity;
  }

  ignoreEvent(_event: Event): boolean {
    return true;
  }
}

// ── Helper Functions (exported for testing) ────────────────────────

/** Normalize a severity string to a known value, defaulting to "info" for unknowns. */
function normalizeSeverity(severity: string): string {
  return (
    ({ error: 'error', warning: 'warning', info: 'info', hint: 'hint' } as Record<string, string>)[severity] ?? 'info'
  );
}

/** Numeric priority for determining primary severity when multiple diagnostics share a line. */
function severityPriority(severity: string): number {
  return ({ error: 4, warning: 3, info: 2, hint: 1 } as Record<string, number>)[severity] ?? 2;
}

/**
 * Truncate a diagnostic message to `maxLength` characters, appending an
 * ellipsis if it exceeds the limit.
 */
export function truncateMessage(message: string, maxLength = MAX_MESSAGE_LENGTH): string {
  if (maxLength < 1) return '';
  return message.length <= maxLength ? message : message.slice(0, maxLength - 1) + '\u2026';
}

/**
 * Compute decoration set from all diagnostics in the visible viewport.
 *
 * Groups diagnostics by line, combines messages with a middot separator,
 * and picks the highest-severity diagnostic to determine widget styling.
 *
 * Exported for unit testing.
 */
export function computeErrorLensDecorations(view: EditorView): DecorationSet {
  const { from: viewFrom, to: viewTo } = view.viewport;

  // Collect diagnostics by line number
  const diagnosticsByLine = new Map<number, Array<{ message: string; severity: string }>>();

  forEachDiagnostic(view.state, (d: Diagnostic) => {
    const line = view.state.doc.lineAt(d.from);
    // Only include diagnostics in the visible viewport
    if (line.to < viewFrom || line.from > viewTo) return;
    const severity = normalizeSeverity(d.severity);
    const message = truncateMessage(d.message);
    let arr = diagnosticsByLine.get(line.number);
    if (!arr) {
      arr = [];
      diagnosticsByLine.set(line.number, arr);
    }
    arr.push({ message, severity });
  });

  if (diagnosticsByLine.size === 0) return Decoration.none;

  // Build sorted decoration array
  const decorations: Array<{ pos: number; deco: ReturnType<typeof Decoration.widget> }> = [];

  for (const [lineNumber, diags] of diagnosticsByLine) {
    const line = view.state.doc.line(lineNumber);
    const combinedMessage = truncateMessage(
      diags.map((d) => d.message).join(DIAGNOSTIC_SEPARATOR),
      MAX_COMBINED_LENGTH,
    );
    // Use the highest-severity diagnostic for the widget's CSS class
    const primarySeverity = diags.reduce(
      (worst, d) => (severityPriority(d.severity) > severityPriority(worst.severity) ? d : worst),
      diags[0],
    ).severity;
    decorations.push({
      pos: line.to,
      deco: Decoration.widget({
        widget: new ErrorLensWidget(combinedMessage, primarySeverity),
        side: 1,
      }),
    });
  }

  // Sort by position (required by Decoration.set)
  decorations.sort((a, b) => a.pos - b.pos);

  return Decoration.set(
    decorations.map((d) => d.deco.range(d.pos)),
    true,
  );
}

// ── ViewPlugin ─────────────────────────────────────────────────────

class ErrorLensPluginValue {
  decorations: DecorationSet = Decoration.none;
  private view: EditorView;
  private timeoutId: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;

  constructor(view: EditorView) {
    this.view = view;
    // Compute synchronously on init so diagnostics appear immediately
    this.decorations = computeErrorLensDecorations(view);
  }

  update(update: ViewUpdate): void {
    if (this.destroyed) return;
    // Skip our own annotation-triggered re-renders (avoid infinite loop)
    if (update.transactions.some((t) => t.annotation(errorLensAnnotation))) return;

    // Recompute when document changes, viewport scrolls, or diagnostics change
    const diagsChanged = diagnosticCount(update.startState) !== diagnosticCount(update.state);
    if (update.docChanged || update.viewportChanged || diagsChanged) {
      this.scheduleUpdate();
    }
  }

  private scheduleUpdate(): void {
    if (this.timeoutId) clearTimeout(this.timeoutId);
    this.timeoutId = setTimeout(() => {
      if (this.destroyed) return;
      try {
        this.decorations = computeErrorLensDecorations(this.view);
        this.view.dispatch({ annotations: [errorLensAnnotation.of(true)] });
      } catch (err) {
        debugLog('[errorLens] scheduled update error:', err);
      }
      this.timeoutId = null;
    }, DEBOUNCE_MS);
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

const errorLensBaseTheme = EditorView.baseTheme({
  '.cm-errorLens': {
    fontSize: '85%',
    opacity: '0.8',
    padding: '0 4px',
    whiteSpace: 'nowrap',
    userSelect: 'none',
    cursor: 'default',
  },
  '.cm-errorLens:hover': {
    opacity: '1',
  },
  '.cm-errorLens-error': {
    color: 'var(--accent-error, #e06c75)',
    background: 'var(--accent-error-bg, rgba(224, 108, 117, 0.15))',
  },
  '.cm-errorLens-warning': {
    color: 'var(--accent-warning, #e5c07b)',
    background: 'var(--accent-warning-bg, rgba(229, 192, 123, 0.15))',
  },
  '.cm-errorLens-info': {
    color: 'var(--accent-primary, #61afef)',
    background: 'var(--accent-primary-bg, rgba(97, 175, 239, 0.15))',
  },
  '.cm-errorLens-hint': {
    color: 'var(--accent-success, #98c379)',
    background: 'var(--accent-success-bg, rgba(152, 195, 121, 0.15))',
  },
  '&dark .cm-errorLens': {
    opacity: '0.75',
  },
  '&dark .cm-errorLens:hover': {
    opacity: '0.9',
  },
  '&light .cm-errorLens': {
    opacity: '0.8',
  },
  '&light .cm-errorLens:hover': {
    opacity: '1',
  },
});

// ── Public API ────────────────────────────────────────────────────

/**
 * Create the Error Lens extension.
 *
 * Returns an array of extensions to be added to the editor's extension set.
 * Reads diagnostics from @codemirror/lint state and renders inline widgets.
 */
export function errorLensPlugin(): Extension {
  return [
    errorLensBaseTheme,
    ViewPlugin.fromClass(ErrorLensPluginValue, {
      decorations: (v) => v.decorations,
    }),
  ];
}
