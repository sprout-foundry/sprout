/**
 * useEditorDiagnostics — encapsulates diagnostic fetching logic for the editor.
 *
 * Fetches diagnostics from the semantic engine (TypeScript/Go) or falls back to
 * the basic diagnostics API. Uses debounced updates to avoid excessive re-renders.
 *
 * The fetch itself is trailing-edge debounced (FETCH_DEBOUNCE_MS): while typing,
 * no request fires until the user pauses 350ms, and rapid edits coalesce into a
 * single request carrying the LATEST content. Save triggers bypass the debounce
 * and run the fetch immediately.
 *
 * When an LSP client is connected for semantic languages, diagnostics are
 * handled via the LSP serverDiagnostics() extension and this hook skips the
 * fetch to avoid duplication.
 */

import type { EditorView } from '@codemirror/view';
import { useRef, useCallback, useEffect } from 'react';
import { resolveLanguageId } from '../extensions/languageRegistry';
import { clearDiagnostics, createDebouncedDiagnosticsUpdater } from '../extensions/lintDiagnostics';
import { getClientForLanguageSync } from '../extensions/lspExtensions';
import { ApiService } from '../services/api';
import type { EditorBuffer } from '../types/editor';
import { debugLog } from '../utils/log';

/**
 * Trailing-edge debounce window for edit-triggered diagnostic fetches.
 * While typing, no request fires until the user pauses this long; the latest
 * content wins. Save triggers bypass the debounce entirely.
 */
export const FETCH_DEBOUNCE_MS = 350;

/** Languages that support semantic diagnostics and LSP integration. */
function isSemanticLanguage(languageId: string): boolean {
  return (
    languageId === 'typescript' ||
    languageId === 'typescript-jsx' ||
    languageId === 'javascript' ||
    languageId === 'javascript-jsx' ||
    languageId === 'go'
  );
}

/** Trigger type for diagnostic requests */
export type DiagnosticTrigger = 'edit' | 'save';

export interface UseEditorDiagnosticsReturn {
  /** Fetch diagnostics for the given file/content and push them into the editor view */
  fetchDiagnostics: (filePath: string, content: string, trigger?: DiagnosticTrigger) => void;
  /** Stable ref to fetchDiagnostics (avoids forward-reference issues in consuming components) */
  fetchDiagnosticsRef: React.MutableRefObject<(filePath: string, content: string, trigger?: DiagnosticTrigger) => void>;
  /** The isSemanticLanguage helper, exposed for context menu checks */
  isSemanticLanguage: (languageId: string) => boolean;
}

/**
 * Hook that provides diagnostic fetching functionality for the editor.
 *
 * @param viewRef - Ref to the CodeMirror EditorView instance
 * @param buffer - Current buffer (may be undefined for empty panes)
 * @returns Object containing fetchDiagnostics, fetchDiagnosticsRef, and isSemanticLanguage
 */
export function useEditorDiagnostics(
  viewRef: React.MutableRefObject<EditorView | null>,
  buffer?: EditorBuffer | null,
): UseEditorDiagnosticsReturn {
  // API service singleton — same pattern as EditorPane
  const apiService = useRef(ApiService.getInstance()).current;

  // Debounced diagnostics updater — coalesces rapid diagnostic pushes
  const debouncedDiag = useRef(createDebouncedDiagnosticsUpdater(500));

  // Trailing-edge fetch debounce state: the latest edit request waiting for the
  // pause window to elapse before it is dispatched to the backend.
  const pendingFetchRef = useRef<{ filePath: string; content: string; trigger: DiagnosticTrigger } | null>(null);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Path of the buffer currently on screen. Updated every render so async
  // dispatch can verify the file it targets is still the visible buffer: the
  // CodeMirror view survives buffer switches (only real pane destruction nulls
  // viewRef), so without this check a debounced fetch for file A could paint
  // A's diagnostics onto file B's editor.
  const bufferPathRef = useRef<string | null>(null);
  bufferPathRef.current = buffer?.file?.path ?? null;

  // Monotonic sequence for in-flight diagnostic fetches: only the newest
  // request may apply its result. Prevents a slow edit-triggered response from
  // overwriting a newer save-triggered response.
  const requestSeqRef = useRef(0);

  // Cleanup timers on unmount
  useEffect(() => {
    return () => {
      debouncedDiag.current.cancel();
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
        debounceTimerRef.current = null;
      }
      pendingFetchRef.current = null;
    };
  }, []);

  // Forward-reference ref to avoid circular dependency issues in consuming components
  // (e.g., EditorPane's loadFile callback needs to call fetchDiagnostics before
  // fetchDiagnostics is defined in the component body).
  const fetchDiagnosticsRef = useRef<(filePath: string, content: string, trigger?: DiagnosticTrigger) => void>(() => {
    /* noop */
  });

  // Perform the actual diagnostic fetch (semantic, then basic fallback) and push
  // the results into the editor. Called by the debounced fetchDiagnostics below.
  const doFetch = useCallback(
    async (filePath: string, content: string, trigger: DiagnosticTrigger) => {
      if (!viewRef.current) return; // Guard against unmount before the fetch starts

      // The debounce timer may fire after the user switched to another buffer.
      // The view survives buffer switches, so applying A's result here would
      // paint stale diagnostics onto B's editor. Bail when the target file is
      // no longer the visible buffer.
      if (bufferPathRef.current !== null && filePath !== bufferPathRef.current) return;
      const seq = ++requestSeqRef.current;

      const languageId =
        resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name)
          .languageId ?? '';

      // Try semantic diagnostics first (TypeScript/Go)
      try {
        // If LSP client is connected, it handles diagnostics via serverDiagnostics() extension
        // - skip old semantic diagnostics to avoid duplication
        if (isSemanticLanguage(languageId) && getClientForLanguageSync(languageId)) {
          debugLog('[fetchDiagnostics] LSP client active, skipping semantic diagnostics');
          return;
        }

        if (isSemanticLanguage(languageId)) {
          const semantic = await apiService.getSemanticDiagnostics(filePath, content, languageId, trigger);
          if (!viewRef.current) return; // Guard against unmount during async call
          if (seq !== requestSeqRef.current) return; // A newer request superseded this one
          if (bufferPathRef.current !== null && filePath !== bufferPathRef.current) return; // Buffer switched mid-flight
          if (semantic.capabilities?.diagnostics) {
            debugLog(
              `[fetchDiagnostics] semantic latency ${semantic.duration_ms ?? -1}ms (${languageId}, trigger=${trigger})`,
            );
            if (semantic.diagnostics && semantic.diagnostics.length > 0) {
              debouncedDiag.current.update(viewRef.current, semantic.diagnostics);
            } else {
              clearDiagnostics(viewRef.current);
            }
            return;
          }
        }
      } catch (err) {
        debugLog('[fetchDiagnostics] semantic diagnostics unavailable, falling back:', err);
      }

      // Fallback to basic diagnostics
      try {
        const result = await apiService.getDiagnostics(filePath, content);
        if (!viewRef.current) return; // Guard against unmount during async call
        if (seq !== requestSeqRef.current) return; // A newer request superseded this one
        if (bufferPathRef.current !== null && filePath !== bufferPathRef.current) return; // Buffer switched mid-flight
        if (result.diagnostics && result.diagnostics.length > 0) {
          debouncedDiag.current.update(viewRef.current, result.diagnostics);
        } else {
          clearDiagnostics(viewRef.current);
        }
      } catch (err) {
        debugLog('[fetchDiagnostics] best-effort diagnostic fetch failed:', err);
        clearDiagnostics(viewRef.current);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- apiService is a stable singleton ref; viewRef is stable across renders
    [buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name],
  );

  // Debounced entry point: edit triggers are coalesced with a trailing-edge
  // debounce — while typing, no request fires until the user pauses 350ms, and
  // the latest content wins. Save triggers bypass the delay and run immediately.
  const fetchDiagnostics = useCallback(
    (filePath: string, content: string, trigger: DiagnosticTrigger = 'edit') => {
      if (!viewRef.current) return;

      if (trigger === 'save') {
        // Save bypasses the debounce: cancel any pending edit fetch and run now.
        if (debounceTimerRef.current !== null) {
          clearTimeout(debounceTimerRef.current);
          debounceTimerRef.current = null;
        }
        pendingFetchRef.current = null;
        void doFetch(filePath, content, trigger);
        return;
      }

      // 'edit' → trailing-edge debounce: keep the latest request and reset the
      // timer so rapid keystrokes coalesce into a single fetch.
      pendingFetchRef.current = { filePath, content, trigger };
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
      debounceTimerRef.current = setTimeout(() => {
        debounceTimerRef.current = null;
        const pending = pendingFetchRef.current;
        pendingFetchRef.current = null;
        if (pending) {
          void doFetch(pending.filePath, pending.content, pending.trigger);
        }
      }, FETCH_DEBOUNCE_MS);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- viewRef is stable across renders; doFetch carries the buffer-derived deps
    [doFetch],
  );

  // Keep ref in sync so consumers can call fetchDiagnostics via the ref
  fetchDiagnosticsRef.current = fetchDiagnostics;

  return {
    fetchDiagnostics,
    fetchDiagnosticsRef,
    isSemanticLanguage,
  };
}
