/**
 * useEditorDiagnostics — encapsulates diagnostic fetching logic for the editor.
 *
 * Fetches diagnostics from the semantic engine (TypeScript/Go) or falls back to
 * the basic diagnostics API. Uses debounced updates to avoid excessive re-renders.
 *
 * When an LSP client is connected for semantic languages, diagnostics are
 * handled via the LSP serverDiagnostics() extension and this hook skips the
 * fetch to avoid duplication.
 *
 * Target: ~120 lines
 */

import { useRef, useCallback, useEffect } from 'react';
import type { EditorView } from '@codemirror/view';
import { clearDiagnostics, createDebouncedDiagnosticsUpdater } from '../extensions/lintDiagnostics';
import { resolveLanguageId } from '../extensions/languageRegistry';
import { getClientForLanguageSync } from '../extensions/lspExtensions';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import type { EditorBuffer } from '../types/editor';

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

  // Cleanup debounced timer on unmount
  useEffect(() => {
    return () => {
      debouncedDiag.current.cancel();
    };
  }, []);

  // Forward-reference ref to avoid circular dependency issues in consuming components
  // (e.g., EditorPane's loadFile callback needs to call fetchDiagnostics before
  // fetchDiagnostics is defined in the component body).
  const fetchDiagnosticsRef = useRef<(filePath: string, content: string, trigger?: DiagnosticTrigger) => void>(
    () => {
      /* noop */
    },
  );

  // Fetch diagnostics for the current file and push them into the editor
  const fetchDiagnostics = useCallback(
    async (filePath: string, content: string, trigger: DiagnosticTrigger = 'edit') => {
      if (!viewRef.current) return;

      const languageId = resolveLanguageId(
        buffer?.languageOverride,
        buffer?.file?.ext?.replace(/^\./, ''),
        buffer?.file?.name,
      ).languageId ?? '';

      // If LSP client is connected, it handles diagnostics via serverDiagnostics() extension
      // - skip old semantic diagnostics to avoid duplication
      if (isSemanticLanguage(languageId) && getClientForLanguageSync(languageId)) {
        debugLog('[fetchDiagnostics] LSP client active, skipping semantic diagnostics');
        return;
      }

      // Try semantic diagnostics first (TypeScript/Go)
      try {
        if (isSemanticLanguage(languageId)) {
          const semantic = await apiService.getSemanticDiagnostics(filePath, content, languageId, trigger);
          if (!viewRef.current) return; // Guard against unmount during async call
          if (semantic.capabilities?.diagnostics) {
            debugLog(`[fetchDiagnostics] semantic latency ${semantic.duration_ms ?? -1}ms (${languageId}, trigger=${trigger})`);
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

  // Keep ref in sync so consumers can call fetchDiagnostics via the ref
  fetchDiagnosticsRef.current = fetchDiagnostics;

  return {
    fetchDiagnostics,
    fetchDiagnosticsRef,
    isSemanticLanguage,
  };
}
