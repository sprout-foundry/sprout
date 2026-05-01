/**
 * useEditorLSP — manages LSP connection state tracking for the footer indicator.
 *
 * Extracts LSP state tracking logic from EditorPane:
 * - State: lspState, lspLanguage
 * - The useEffect subscribing to LSP connection state changes
 * - The useMemo for languageInfo (resolved language ID with auto-detection flag)
 * - handleLanguageChange callback
 *
 * Target: ~150 lines
 */

import { useEffect, useMemo, useCallback, useState } from 'react';

import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import { resolveLanguageId } from '../extensions/languageRegistry';
import type { EditorBuffer } from '../types/editor';
import type { LSPConnectionState } from '../services/lspClientService';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface LanguageInfo {
  languageId: string | null;
  isAutoDetected: boolean;
}

export interface UseEditorLSPReturn {
  lspState: LSPConnectionState;
  lspLanguage: string | null;
  languageInfo: LanguageInfo;
  handleLanguageChange: (languageId: string | null) => void;
}

/**
 * Hook that manages LSP connection state tracking and language resolution.
 *
 * @param buffer - Current buffer
 * @param setBufferLanguageOverride - Function to set language override
 */
export function useEditorLSP(
  buffer: EditorBuffer | null | undefined,
  setBufferLanguageOverride: (bufferId: string, languageId: string | null) => void,
): UseEditorLSPReturn {
  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  const [lspState, setLspState] = useState<LSPConnectionState>('disconnected');
  const [lspLanguage, setLspLanguage] = useState<string | null>(null);

  // ---------------------------------------------------------------------------
  // Compute language info
  // ---------------------------------------------------------------------------

  const languageInfo = useMemo<LanguageInfo>(() => {
    if (!buffer || !buffer.file) return { languageId: null as string | null, isAutoDetected: false };
    return resolveLanguageId(buffer.languageOverride ?? null, buffer.file?.ext?.replace(/^\./, ''), buffer.file.name);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // ---------------------------------------------------------------------------
  // Language change handler
  // ---------------------------------------------------------------------------

  const handleLanguageChange = useCallback(
    (languageId: string | null) => {
      if (!buffer) return;
      setBufferLanguageOverride(buffer.id, languageId);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [buffer?.id, setBufferLanguageOverride],
  );

  // ---------------------------------------------------------------------------
  // Subscribe to LSP connection state changes
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const langId = languageInfo.languageId;
    if (!langId || !LSP_SUPPORTED_LANGUAGES.has(langId)) {
      setLspLanguage(null);
      return;
    }

    setLspLanguage(langId);

    const lspService = getLSPClientService();
    setLspState(lspService.getLSPState(langId));

    const unsubscribe = lspService.onStateChange((languageId, state) => {
      if (languageId === langId) {
        setLspState(state);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [languageInfo.languageId]);

  return {
    lspState,
    lspLanguage,
    languageInfo,
    handleLanguageChange,
  };
}
