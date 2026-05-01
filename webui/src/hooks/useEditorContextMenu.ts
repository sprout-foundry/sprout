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

/**
 * Hook that manages context menu state and handlers.
 *
 * @param buffer - Current buffer
 * @param viewRef - Ref to CodeMirror EditorView
 */
export function useEditorContextMenu(
  buffer: EditorBuffer | null | undefined,
  viewRef: React.MutableRefObject<EditorView | null>,
): UseEditorContextMenuReturn {
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
    if (!buffer || !buffer.file || buffer.file.isDir) return '';
    if (buffer.kind !== 'file') return '';
    const langId = resolveLanguageId(
      buffer.languageOverride,
      buffer.file.ext?.replace(/^\./, ''),
      buffer.file.name,
    ).languageId ?? '';
    return langId;
  }, [buffer]);

  // ---------------------------------------------------------------------------
  // Context menu handlers
  // ---------------------------------------------------------------------------

  const handleEditorContextMenu = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (!buffer || !buffer.file || buffer.file.isDir) return;
      if (buffer.kind !== 'file') return;
      const hasSelection = !!viewRef.current && !viewRef.current.state.selection.main.empty;
      const langId = getLanguageId();
      setContextMenu({ x: e.clientX, y: e.clientY, hasSelection, languageId: langId });
    },
    [buffer, viewRef, getLanguageId],
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
    if (!buffer || !buffer.file) return;
    window.dispatchEvent(
      new CustomEvent('sprout:reveal-in-explorer', {
        detail: { path: buffer.file.path },
      }),
    );
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyRelativePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    copyToClipboard(buffer.file.path).catch((err) => {
      debugLog('Clipboard write failed for relative path:', err);
    });
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyAbsolutePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    const root = workspaceRoot.replace(/\/+$/, '');
    copyToClipboard(`${root}/${buffer.file.path}`).catch((err) => {
      debugLog('Clipboard write failed for absolute path:', err);
    });
    hideContextMenu();
  }, [buffer, workspaceRoot, hideContextMenu]);

  const handleGoToDefinitionFromMenu = useCallback(() => {
    hideContextMenu();
    // Dispatch event to trigger go-to-definition via EditorPane
    document.dispatchEvent(new CustomEvent('editor-go-to-definition-from-menu'));
  }, [hideContextMenu]);

  const handleFindAllReferencesFromMenu = useCallback(() => {
    hideContextMenu();
    // Dispatch event to trigger find-all-references via EditorPane
    document.dispatchEvent(new CustomEvent('editor-find-all-references-from-menu'));
  }, [hideContextMenu]);

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
