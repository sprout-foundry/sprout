/**
 * useEditorScrollSync — manages scroll position persistence and cross-pane
 * linked scrolling for the editor.
 *
 * Provides:
 * - Throttled scroll position persistence (writes to buffer state)
 * - Cross-pane linked scrolling (syncs scroll position for same file across panes)
 * - Linked scroll enable/disable state synchronization
 *
 * @see EditorPane.tsx for the original implementation this hook extracts
 */
import type { EditorView } from '@codemirror/view';
import { useRef, useEffect, useCallback } from 'react';
import { setLinkedScrollEnabled, suppressScrollSync } from '../extensions/linkedScroll';
import type { EditorBuffer } from '../types/editor';

export interface UseEditorScrollSyncOptions {
  /** Editor pane ID (used to filter linked scroll events) */
  paneId: string;
  /** Ref to the CodeMirror EditorView */
  viewRef: React.RefObject<EditorView | null>;
  /** Ref to current buffer — stable identity avoids callback recreation on every scroll update */
  bufferRef: React.RefObject<EditorBuffer | null | undefined>;
  /** Current file path for linked scroll filtering (re-subscribes on file change) */
  filePath: string | null | undefined;
  /** From EditorManagerContext */
  updateBufferScroll: (bufferId: string, scroll: { top: number; left: number }) => void;
  /** From EditorManagerContext */
  isLinkedScrollEnabled: boolean;
}

export interface UseEditorScrollSyncReturn {
  /** Attach this to the CodeMirror updateListener — handles scroll persistence */
  handleScrollUpdate: (update: { viewportChanged: boolean; view: EditorView }) => void;
  /** Cancel any pending scroll flush (call from editor init cleanup) */
  cancelPendingFlush: () => void;
}

/**
 * Hook for managing scroll position persistence and cross-pane linked scrolling.
 *
 * Throttles scroll position updates to ~500ms to avoid excessive React state updates
 * that would starve the browser paint cycle. Uses rAF flush for final position capture.
 */
export function useEditorScrollSync(options: UseEditorScrollSyncOptions): UseEditorScrollSyncReturn {
  const { paneId, viewRef, bufferRef, filePath, updateBufferScroll, isLinkedScrollEnabled } = options;

  // Throttle state for scroll position persistence.
  // Without throttling, updateBufferScroll fires ~60 times/sec during scrolling,
  // creating new buffer objects each time and causing full component re-renders.
  const lastScrollSyncTime = useRef(0);
  const scrollFlushRafId = useRef<number | null>(null);

  // ── Scroll position persistence ───────────────────────────────────────

  const handleScrollUpdate = useCallback(
    (update: { viewportChanged: boolean; view: EditorView }) => {
      if (!update.viewportChanged) return;
      const buf = bufferRef.current;
      if (!buf) return;

      const scrollInfo = update.view.scrollDOM;
      if (!scrollInfo) return;

      const now = performance.now();
      if (now - lastScrollSyncTime.current < 500) return; // throttle: 500ms

      lastScrollSyncTime.current = now;
      updateBufferScroll(buf.id, { top: scrollInfo.scrollTop, left: scrollInfo.scrollLeft });

      // Flush final position via rAF to ensure the last scroll position is captured
      if (scrollFlushRafId.current != null) cancelAnimationFrame(scrollFlushRafId.current);
      scrollFlushRafId.current = requestAnimationFrame(() => {
        scrollFlushRafId.current = null;
        const sd = update.view.scrollDOM;
        if (sd && sd.isConnected) {
          const b = bufferRef.current;
          if (b) updateBufferScroll(b.id, { top: sd.scrollTop, left: sd.scrollLeft });
        }
      });
    },
    [bufferRef, updateBufferScroll],
  );

  const cancelPendingFlush = useCallback(() => {
    if (scrollFlushRafId.current != null) {
      cancelAnimationFrame(scrollFlushRafId.current);
      scrollFlushRafId.current = null;
    }
  }, []);

  // ── Cross-pane linked scrolling ───────────────────────────────────────

  useEffect(() => {
    const handleLinkedScroll = (e: Event) => {
      const customEvent = e as CustomEvent;
      const { sourcePaneId, filePath: eventFilePath, topLine } = customEvent.detail;

      // Skip if same pane or different file
      if (sourcePaneId === paneId) return;
      if (!filePath || filePath !== eventFilePath) return;
      if (!viewRef.current) return;

      const view = viewRef.current;

      // topLine is 1-based; validate bounds.
      if (topLine < 1 || topLine > view.state.doc.lines) return;

      // Suppress this pane's next viewportChanged dispatch so the
      // programmatic scroll doesn't cause an echo loop (A → B → A → …).
      suppressScrollSync(paneId);

      // Get the layout block for the target line and scroll it to the top.
      const targetPos = view.state.doc.line(topLine).from;
      const block = view.lineBlockAt(targetPos);
      view.scrollDOM.scrollTo(0, block.top);
    };

    document.addEventListener('editor:linked-scroll', handleLinkedScroll);
    return () => document.removeEventListener('editor:linked-scroll', handleLinkedScroll);
  }, [paneId, filePath]); // eslint-disable-line react-hooks/exhaustive-deps -- viewRef.current is read inside but the ref object is stable

  // ── Linked scroll enabled state sync ─────────────────────────────────

  useEffect(() => {
    setLinkedScrollEnabled(isLinkedScrollEnabled);
  }, [isLinkedScrollEnabled]);

  return { handleScrollUpdate, cancelPendingFlush };
}
