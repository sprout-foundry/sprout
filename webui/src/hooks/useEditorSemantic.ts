/**
 * useEditorSemantic — manages semantic navigation features (go-to-definition, find-references).
 *
 * Extracts semantic navigation logic from EditorPane:
 * - handleGoToDefinition (API call + cross-file navigation)
 * - handleFindAllReferences (API call + overlay state)
 * - handleSelectReference (navigate to reference in any file)
 * - handleSelectWorkspaceSymbol (navigate to workspace symbol)
 * - handleGoToLine (basic line navigation)
 * - State: showGoToWorkspaceSymbol, showFindRefs, refsSymbolName, refsResults, refsLoading
 * - Refs: bufferStateRef, localContentRef
 *
 * Target: ~300 lines
 */

import { useState, useRef, useCallback } from 'react';
import type { EditorView } from '@codemirror/view';
import type { ReferenceInfo } from '../components/FindAllReferencesOverlay';
import { ApiService } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { resolveLanguageId } from '../extensions/languageRegistry';
import { debugLog } from '../utils/log';
import type { EditorBuffer } from '../types/editor';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorSemanticReturn {
  // State
  showGoToWorkspaceSymbol: boolean;
  showFindRefs: boolean;
  refsSymbolName: string;
  refsResults: ReferenceInfo[];
  refsLoading: boolean;

  // Setters
  setShowGoToWorkspaceSymbol: (v: boolean) => void;
  setShowFindRefs: (v: boolean) => void;

  // Handlers
  handleGoToLine: (line: number) => void;
  handleGoToDefinition: () => void;
  handleFindAllReferences: () => void;
  handleSelectReference: (filePath: string, line: number) => void;
  handleSelectWorkspaceSymbol: (filePath: string, line?: number) => void;

  // Refs
  bufferStateRef: React.MutableRefObject<EditorBuffer | null>;
  localContentRef: React.MutableRefObject<string>;
}

/**
 * Hook that manages semantic navigation features.
 *
 * @param viewRef - Ref to the CodeMirror EditorView
 * @param bufferRef - Ref to current buffer
 * @param localContent - Current editor content
 * @param isSemanticLanguage - Function to check if language supports semantics
 * @param openWorkspaceBuffer - Function to open a buffer in the workspace
 */
export function useEditorSemantic(
  viewRef: React.MutableRefObject<EditorView | null>,
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>,
  localContent: string,
  isSemanticLanguage: (languageId: string) => boolean,
  openWorkspaceBuffer: (buffer: {
    kind: 'file' | 'diff';
    path: string;
    title: string;
    ext?: string;
    content?: string;
    metadata?: Record<string, unknown>;
    isPinned?: boolean;
    isClosable?: boolean;
  }) => void,
): UseEditorSemanticReturn {
  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  const [showGoToWorkspaceSymbol, setShowGoToWorkspaceSymbol] = useState<boolean>(false);
  const [showFindRefs, setShowFindRefs] = useState<boolean>(false);
  const [refsSymbolName, setRefsSymbolName] = useState<string>('');
  const [refsResults, setRefsResults] = useState<ReferenceInfo[]>([]);
  const [refsLoading, setRefsLoading] = useState<boolean>(false);

  // ---------------------------------------------------------------------------
  // Refs
  // ---------------------------------------------------------------------------

  const apiService = useRef(ApiService.getInstance()).current;
  const bufferStateRef = useRef<EditorBuffer | null>(null);
  const localContentRef = useRef(localContent);
  localContentRef.current = localContent;

  // ---------------------------------------------------------------------------
  // Go to line
  // ---------------------------------------------------------------------------

  const handleGoToLine = useCallback((lineNum: number) => {
    if (!viewRef.current) return;

    const dispatch = viewRef.current;
    const state = dispatch.state;
    const doc = state.doc;

    if (doc.lines === 0) return;

    const line = Math.min(Math.max(lineNum - 1, 0), doc.lines - 1);
    const pos = doc.line(line + 1).from;

    dispatch.dispatch({
      selection: { anchor: pos, head: pos },
      scrollIntoView: true,
    });

    dispatch.focus();
  }, []);

  // ---------------------------------------------------------------------------
  // Go to definition
  // ---------------------------------------------------------------------------

  const handleGoToDefinition = useCallback(async () => {
    const buf = bufferRef.current;
    if (!viewRef.current || !buf || buf.kind !== 'file' || !buf.file || buf.file.path.startsWith('__workspace/')) {
      return;
    }

    const languageId =
      resolveLanguageId(buf.languageOverride, buf.file.ext?.replace(/^\./, ''), buf.file.name).languageId ?? '';
    if (!isSemanticLanguage(languageId)) {
      notificationBus.notify(
        'info',
        'Go to Definition',
        'Semantic definition is currently available for TypeScript/JavaScript and Go files.',
      );
      return;
    }

    const selection = viewRef.current.state.selection.main;
    const lineInfo = viewRef.current.state.doc.lineAt(selection.head);
    const line = lineInfo.number;
    const column = selection.head - lineInfo.from + 1;

    try {
      const result = await apiService.getSemanticDefinition(
        buf.file.path,
        localContentRef.current,
        languageId,
        line,
        column,
      );
      if (!result.capabilities?.definition) {
        notificationBus.notify(
          'warning',
          'Go to Definition',
          'Semantic engine is not available for this language in this environment.',
        );
        return;
      }

      const def = result.definition;
      if (!def || !def.path) {
        notificationBus.notify('info', 'Go to Definition', 'No definition found at cursor.');
        return;
      }

      if (def.path === buf.file.path) {
        handleGoToLine(def.line);
        return;
      }

      const fileName = def.path.split('/').pop() || def.path;
      const dotIndex = fileName.lastIndexOf('.');
      const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

      openWorkspaceBuffer({
        kind: 'file',
        path: def.path,
        title: fileName,
        ext,
      });

      requestAnimationFrame(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line: def.line } }));
      });
    } catch (err) {
      debugLog('[useEditorSemantic] Go to definition failed:', err);
      notificationBus.notify('warning', 'Go to Definition', 'Failed to resolve definition.');
    }
  }, [apiService, openWorkspaceBuffer, handleGoToLine, isSemanticLanguage]);

  // ---------------------------------------------------------------------------
  // Find all references
  // ---------------------------------------------------------------------------

  const handleFindAllReferences = useCallback(async () => {
    const buf = bufferRef.current;
    if (!viewRef.current || !buf || buf.kind !== 'file' || !buf.file || buf.file.path.startsWith('__workspace/')) {
      return;
    }

    const languageId =
      resolveLanguageId(buf.languageOverride, buf.file.ext?.replace(/^\./, ''), buf.file.name).languageId ?? '';
    if (!isSemanticLanguage(languageId)) {
      notificationBus.notify(
        'info',
        'Find All References',
        'Semantic references are currently available for TypeScript/JavaScript and Go files.',
      );
      return;
    }

    const selection = viewRef.current.state.selection.main;
    const lineInfo = viewRef.current.state.doc.lineAt(selection.head);
    const line = lineInfo.number;
    const column = selection.head - lineInfo.from + 1;

    setShowFindRefs(true);
    setRefsLoading(true);
    setRefsSymbolName('');
    setRefsResults([]);

    try {
      const result = await apiService.getSemanticReferences(
        buf.file.path,
        localContentRef.current,
        languageId,
        line,
        column,
      );
      setRefsLoading(false);

      if (!result.capabilities?.references) {
        notificationBus.notify(
          'warning',
          'Find All References',
          'Semantic references are not available for this language in this environment.',
        );
        setShowFindRefs(false);
        return;
      }

      if (result.error || !result.references?.locations?.length) {
        setRefsResults([]);
        setRefsSymbolName('');
        return;
      }

      setRefsSymbolName(result.references.symbolName || '');
      setRefsResults(result.references.locations);
    } catch (err) {
      debugLog('[useEditorSemantic] Find all references failed:', err);
      setRefsLoading(false);
      notificationBus.notify('warning', 'Find All References', 'Failed to find references.');
      setShowFindRefs(false);
    }
  }, [apiService, isSemanticLanguage]);

  // ---------------------------------------------------------------------------
  // Select reference
  // ---------------------------------------------------------------------------

  const handleSelectReference = useCallback(
    (filePath: string, line: number) => {
      const buf = bufferRef.current;
      if (!buf) return;

      if (filePath === buf.file.path) {
        handleGoToLine(line);
        viewRef.current?.focus();
        return;
      }

      const fileName = filePath.split('/').pop() || filePath;
      const dotIndex = fileName.lastIndexOf('.');
      const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

      openWorkspaceBuffer({
        kind: 'file',
        path: filePath,
        title: fileName,
        ext,
      });

      requestAnimationFrame(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
      });
    },
    [handleGoToLine, openWorkspaceBuffer],
  );

  // ---------------------------------------------------------------------------
  // Select workspace symbol
  // ---------------------------------------------------------------------------

  const handleSelectWorkspaceSymbol = useCallback(
    (filePath: string, line?: number) => {
      const fileName = filePath.split('/').pop() || filePath;
      const dotIndex = fileName.lastIndexOf('.');
      const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

      openWorkspaceBuffer({
        kind: 'file',
        path: filePath,
        title: fileName,
        ext,
      });

      if (line !== undefined && line !== null) {
        requestAnimationFrame(() => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
        });
      }
    },
    [openWorkspaceBuffer],
  );

  return {
    showGoToWorkspaceSymbol,
    showFindRefs,
    refsSymbolName,
    refsResults,
    refsLoading,
    setShowGoToWorkspaceSymbol,
    setShowFindRefs,
    handleGoToLine,
    handleGoToDefinition,
    handleFindAllReferences,
    handleSelectReference,
    handleSelectWorkspaceSymbol,
    bufferStateRef,
    localContentRef,
  };
}
