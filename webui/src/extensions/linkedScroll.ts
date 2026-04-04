/**
 * linkedScroll.ts — CodeMirror 6 extension for synchronizing scroll
 * positions across split panes that display the same file.
 *
 * When enabled, scrolling one pane dispatches a `editor:linked-scroll`
 * CustomEvent on `document`.  Other EditorPane instances listen for
 * this event and programatically scroll their CodeMirror view to the
 * same top-visible line.
 *
 * Architecture:
 * - Module-level state tracks:
 *   - `_linkedScrollEnabled: boolean` — global on/off toggle
 *   - `_syncingPaneIds: Set<string>` — pane IDs currently being synced
 *     (re-entrancy guard to prevent infinite scroll loops)
 * - `linkedScrollExtension(paneId, getFilePath)` returns a CodeMirror
 *   Extension (ViewPlugin + updateListener) that:
 *   - On viewport change, if linked scroll is enabled:
 *     1. Checks re-entrancy guard
 *     2. Debounces via requestAnimationFrame (one event per frame)
 *     3. Dispatches `editor:linked-scroll` with
 *        `{ sourcePaneId, filePath, topLine }`
 *     4. Cleans up via microtask
 * - `setLinkedScrollEnabled(boolean)` / `isLinkedScrollEnabled()`
 *   allow external code (EditorManagerContext) to control the toggle.
 *
 * Scroll sync is LINE-based, not pixel-based.  This handles different
 * pane widths and word-wrap settings gracefully.  The source pane
 * reads the top-visible line number; receiver panes scroll to show
 * that same line at the top of their viewport.
 */

import { type EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';

// Type alias for readability — referenced by the factory return type.
import type { Extension } from '@codemirror/state';

// ── Module-level state ──────────────────────────────────────────────

let _linkedScrollEnabled = false;
const _syncingPaneIds = new Set<string>();

/**
 * Enable or disable linked scrolling globally.
 * Typically called from a React effect that mirrors the
 * EditorManagerContext state.
 */
export function setLinkedScrollEnabled(enabled: boolean): void {
  _linkedScrollEnabled = enabled;
}

/**
 * Query whether linked scrolling is currently enabled.
 */
export function isLinkedScrollEnabled(): boolean {
  return _linkedScrollEnabled;
}

/**
 * Mark a pane as receiving a programmatic scroll sync.  The receiver
 * pane's next `viewportChanged` update is suppressed to prevent an
 * echo loop (source → receiver → source → …).  The guard clears via
 * microtask once the current task finishes.
 *
 * Called from EditorPane's scroll-sync event listener before calling
 * `scrollDOM.scrollTo`.
 */
export function suppressScrollSync(paneId: string): void {
  _syncingPaneIds.add(paneId);
  queueMicrotask(() => {
    _syncingPaneIds.delete(paneId);
  });
}

/**
 * @internal Test-only helper to fully reset module-level state.
 */
export function _resetModuleStateForTesting(): void {
  _syncingPaneIds.clear();
  _linkedScrollEnabled = false;
}

// ── Extension factory ───────────────────────────────────────────────

/**
 * Returns a CodeMirror Extension that participates in linked scrolling.
 *
 * @param paneId  — Unique identifier for the pane (used in sync events
 *                  to avoid self-loops).
 * @param getFilePath — Stable callback returning the file path currently
 *                     displayed in this pane (or null / empty for
 *                     non-file buffers).
 */
export function linkedScrollExtension(paneId: string, getFilePath: () => string | null): Extension {
  return ViewPlugin.fromClass(
    class LinkedScrollPlugin {
      /** Pending requestAnimationFrame id for debounced dispatch (one per frame). */
      private _rafId: number | null = null;

      constructor(public view: EditorView) {}

      update(update: ViewUpdate) {
        if (!_linkedScrollEnabled) return;
        if (!update.viewportChanged) return;
        if (_syncingPaneIds.has(paneId)) return;

        const filePath = getFilePath();
        if (!filePath) return;

        // Determine the top-visible line number (1-based).
        const topLine = update.state.doc.lineAt(update.view.viewport.from).number;

        // Debounce: coalesce rapid scroll changes into one dispatch per
        // animation frame.  This matches the browser's own repaint rate
        // and avoids flooding receivers with events they can't render
        // faster than ~60 fps anyway.
        if (this._rafId !== null) cancelAnimationFrame(this._rafId);
        this._rafId = requestAnimationFrame(() => {
          this._rafId = null;
          this._dispatchSync(filePath, topLine);
        });
      }

      /** Dispatch the sync event and set up the re-entrancy guard. */
      private _dispatchSync(filePath: string, topLine: number): void {
        // Re-entrancy guard: mark this pane as currently syncing.
        _syncingPaneIds.add(paneId);

        document.dispatchEvent(
          new CustomEvent('editor:linked-scroll', {
            detail: { sourcePaneId: paneId, filePath, topLine },
          }),
        );

        // Clear the guard after the current task completes.  Microtask
        // ensures dispatch handlers run before we unguard — handlers
        // may synchronously trigger another EditorView.update →
        // viewportChanged cycle.  The guard clears before CM6's next
        // rAF-driven update cycle so normal scrolling resumes.
        queueMicrotask(() => {
          _syncingPaneIds.delete(paneId);
        });
      }

      destroy() {
        // Cancel any pending debounced dispatch.
        if (this._rafId !== null) {
          cancelAnimationFrame(this._rafId);
          this._rafId = null;
        }
        // Remove from the re-entrancy guard set.
        // Note: a pending microtask from _dispatchSync may still fire
        // and call _syncingPaneIds.delete(paneId) after this destroy.
        // This is harmless — pane IDs include timestamps, making reuse
        // practically impossible, and the delete is idempotent.
        _syncingPaneIds.delete(paneId);
      }
    },
  );
}
