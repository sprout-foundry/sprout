/**
 * useEditorContextMenu — manages context menu state and handlers.
 *
 * Extracts context menu logic from EditorPane:
 * - State: contextMenu, workspaceRoot
 * - Handlers: handleEditorContextMenu, handleCopySelection, handleRevealInExplorer
 * - Handlers: handleCopyRelativePath, handleCopyAbsolutePath
 * - Handlers: handleGoToDefinitionFromMenu, handleFindAllReferencesFromMenu
 * - Workspace root fetch effect
 * - Prettier config fetcher setup
 *
 * Target: ~250 lines
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { EditorView } from '@codemirror/view';

import { ApiService } from '../services/api';
import { setConfigFetcher } from '../services/formatter';
import { copyToClipboard } from '../utils/clipboard';
import { debugLog, warn } from '../utils/log';
import { resolveLanguageId } from '../extensions/languageRegistry';
import type { EditorBuffer } from '../types/editor';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ContextMenuState {
  x: number;
  y: number;
  hasSelection: boolean;
  languageId: string;
}

export interface UseEditorContextMenuReturn {
  contextMenu: ContextMenuState | null;
  workspaceRoot: string;
  hideContextMenu: () => void;
  handleEditorContextMenu: (e: React.MouseEvent) => void;
  handleCopySelection: () => void;
  handleRevealInExplorer: () => void;
  handleCopyRelativePath: () => void;
  handleCopyAbsolutePath: () => void;
  handleGoToDefinitionFromMenu: () => void;
  handleFindAllReferencesFromMenu: () => void;
}

export interface UseEditorContextMenuCallbacks {
  onGoToDefinition?: () => void;
  onFindAllReferences?: () => void;
}

/**
 * Hook that manages context menu state and handlers.
 *
 * @param buffer - Current buffer (kept for legacy compatibility but not used in deps)
 * @param bufferRef - Ref to current buffer (avoids callback instability on content changes)
 * @param viewRef - Ref to CodeMirror EditorView
 * @param callbacks - Optional callbacks for semantic actions
 */
export function useEditorContextMenu(
  _buffer: EditorBuffer | null | undefined,
  bufferRef: React.RefObject<EditorBuffer | null | undefined>,
  viewRef: React.MutableRefObject<EditorView | null>,
  callbacks?: UseEditorContextMenuCallbacks,
): UseEditorContextMenuReturn {
  // ---------------------------------------------------------------------------
  // Destructure callbacks for stable dependency arrays
  // ---------------------------------------------------------------------------
  const { onGoToDefinition, onFindAllReferences } = callbacks ?? {};

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [workspaceRoot, setWorkspaceRoot] = useState<string>('');

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  const apiService = useRef(ApiService.getInstance()).current;

  const hideContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  const getLanguageId = useCallback((): string => {
    const buf = bufferRef.current;
    if (!buf || !buf.file || buf.file.isDir) return '';
    if (buf.kind !== 'file') return '';
    const langId = resolveLanguageId(
      buf.languageOverride,
      buf.file.ext?.replace(/^\./, ''),
      buf.file.name,
    ).languageId ?? '';
    return langId;
  }, []);

  // ---------------------------------------------------------------------------
  // Context menu handlers
  // ---------------------------------------------------------------------------

  const handleEditorContextMenu = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const buf = bufferRef.current;
      if (!buf || !buf.file || buf.file.isDir) return;
      if (buf.kind !== 'file') return;
      const hasSelection = !!viewRef.current && !viewRef.current.state.selection.main.empty;
      const langId = getLanguageId();
      setContextMenu({ x: e.clientX, y: e.clientY, hasSelection, languageId: langId });
    },
    [viewRef, getLanguageId],
  );

  const handleCopySelection = useCallback(() => {
    if (!viewRef.current) return;
    const state = viewRef.current.state;
    const text = state.sliceDoc(state.selection.main.from, state.selection.main.to);
    copyToClipboard(text).catch((err) => {
      debugLog('Clipboard write failed for selection:', err);
    });
    hideContextMenu();
  }, [viewRef, hideContextMenu]);

  const handleRevealInExplorer = useCallback(() => {
    const buf = bufferRef.current;
    if (!buf || !buf.file) return;
    window.dispatchEvent(
      new CustomEvent('sprout:reveal-in-explorer', {
        detail: { path: buf.file.path },
      }),
    );
    hideContextMenu();
  }, [hideContextMenu]);

  const handleCopyRelativePath = useCallback(() => {
    const buf = bufferRef.current;
    if (!buf || !buf.file) return;
    copyToClipboard(buf.file.path).catch((err) => {
      debugLog('Clipboard write failed for relative path:', err);
    });
    hideContextMenu();
  }, [hideContextMenu]);

  const handleCopyAbsolutePath = useCallback(() => {
    const buf = bufferRef.current;
    if (!buf || !buf.file) return;
    const root = workspaceRoot.replace(/\/+$/, '');
    copyToClipboard(`${root}/${buf.file.path}`).catch((err) => {
      debugLog('Clipboard write failed for absolute path:', err);
    });
    hideContextMenu();
  }, [workspaceRoot, hideContextMenu]);

  const handleGoToDefinitionFromMenu = useCallback(() => {
    hideContextMenu();
    onGoToDefinition?.();
  }, [hideContextMenu, onGoToDefinition]);

  const handleFindAllReferencesFromMenu = useCallback(() => {
    hideContextMenu();
    onFindAllReferences?.();
  }, [hideContextMenu, onFindAllReferences]);

  // ---------------------------------------------------------------------------
  // Workspace root fetch
  // ---------------------------------------------------------------------------

  useEffect(() => {
    apiService
      .getWorkspace()
      .then((ws) => {
        setWorkspaceRoot(ws.workspace_root || '');
      })
      .catch((err) => {
        warn(`Failed to fetch workspace root: ${err instanceof Error ? err.message : String(err)}`);
      });

    // Set up Prettier config fetcher for formatter service
    setConfigFetcher(async (filePath: string) => {
      return apiService.getPrettierConfig(filePath);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    contextMenu,
    workspaceRoot,
    hideContextMenu,
    handleEditorContextMenu,
    handleCopySelection,
    handleRevealInExplorer,
    handleCopyRelativePath,
    handleCopyAbsolutePath,
    handleGoToDefinitionFromMenu,
    handleFindAllReferencesFromMenu,
  };
}
