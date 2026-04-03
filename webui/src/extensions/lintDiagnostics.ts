/**
 * Lint diagnostics integration for CodeMirror.
 *
 * Wraps `@codemirror/lint` to provide a clean imperative API for pushing
 * diagnostics into the editor from external sources (e.g., an LSP server
 * or WebSocket connection).
 *
 * Usage:
 *   // In the editor extensions array:
 *   lintDiagnostics()
 *
 *   // Push diagnostics externally:
 *   // severity: 'hint' | 'info' | 'warning' | 'error'
 *   updateDiagnostics(view, [{ from: 0, to: 10, severity: 'error', message: 'oops' }]);
 *
 *   // Clear:
 *   clearDiagnostics(view);
 */

import { setDiagnostics, lintGutter, linter, lintKeymap, diagnosticCount as cmDiagnosticCount, openLintPanel, nextDiagnostic, previousDiagnostic, forceLinting, type Diagnostic } from '@codemirror/lint';
import { EditorView, keymap } from '@codemirror/view';

/** Re-export the Diagnostic type so consumers don't need to import from @codemirror/lint directly. */
export type { Diagnostic };

/** Re-export useful lint commands for programmatic use (e.g., custom keybindings). */
export { openLintPanel, nextDiagnostic, previousDiagnostic, forceLinting };

/**
 * Returns an array of CodeMirror extensions that enable the lint
 * infrastructure without any active linting source.
 *
 * Diagnostics are pushed in imperatively via `updateDiagnostics()` /
 * `clearDiagnostics()`. The gutter shows severity-colored markers, and
 * the default keybindings are registered (Ctrl+Shift+m to open the
 * lint panel, F8 to jump to the next diagnostic).
 */
export function lintDiagnostics() {
  return [
    linter(null),
    lintGutter(),
    keymap.of(lintKeymap),
  ];
}

/**
 * Push diagnostics into the editor from an external source.
 *
 * Each diagnostic must have `from`, `to`, `severity`, and `message`
 * fields matching the `@codemirror/lint` `Diagnostic` interface.
 *
 * Positions (`from`/`to`) are clamped to the document length to handle
 * stale diagnostics from an LSP that hasn't caught up with local edits.
 */
export function updateDiagnostics(view: EditorView, diagnostics: Diagnostic[]): void {
  const len = view.state.doc.length;
  const clamped: Diagnostic[] = diagnostics.map(d => ({
    ...d,
    from: Math.max(0, Math.min(d.from, len)),
    to: Math.max(0, Math.min(d.to, len)),
  }));
  view.dispatch(setDiagnostics(view.state, clamped));
}

/**
 * Remove all active diagnostics from the editor.
 */
export function clearDiagnostics(view: EditorView): void {
  view.dispatch(setDiagnostics(view.state, []));
}

/**
 * Returns the number of active diagnostics in the editor's current state.
 */
export function diagnosticCount(view: EditorView): number {
  return cmDiagnosticCount(view.state);
}
