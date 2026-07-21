/**
 * useEditorUpdate — manages editor content updates and document change handling.
 *
 * Provides:
 * - Local content ref tracking for the editor
 * - Document change handling (updates buffer content, tracks modified state, triggers diagnostics)
 * - Integration with cursor updates and scroll sync
 *
 * @see EditorPane.tsx for the original implementation this hook extracts
 */

import type { ViewUpdate } from '@codemirror/view';
import { useRef, useCallback } from 'react';
import type { EditorBuffer } from '../types/editor';
import type { DiagnosticTrigger } from './useEditorDiagnostics';
import type { CMViewAPI } from './useCMView';

export interface UseEditorUpdateParams {
  /** Ref to the current buffer — avoids stale closures in the update listener */
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>;
  /** Local content state from parent component */
  localContent: string;
  /** Setter for localContent — used when document changes in the editor */
  setLocalContent: React.Dispatch<React.SetStateAction<string>>;
  /** CodeMirror view API ref — populated by EditorPane after `useCMView`
   *  returns. Reading `cmViewApiRef.current?.isExternalUpdate()` is safe at
   *  any time; before the API is mounted, it returns `false`. */
  cmViewApiRef: React.MutableRefObject<CMViewAPI | null>;
  /** Ref from useEditorDiagnostics — triggers diagnostics fetch on content change */
  fetchDiagnosticsRef: React.MutableRefObject<(filePath: string, content: string, trigger?: DiagnosticTrigger) => void>;
  /** From useEditorCursor — handles cursor position updates */
  handleCursorUpdate: (update: ViewUpdate) => void;
  /** From useEditorScrollSync — handles scroll position updates */
  handleScrollUpdate: (update: ViewUpdate) => void;
  /** From EditorManagerContext — updates buffer content in global state */
  updateBufferContent: (id: string, content: string) => void;
  /** From EditorManagerContext — tracks whether buffer has unsaved changes */
  setBufferModified: (id: string, modified: boolean) => void;
}

export interface UseEditorUpdateReturn {
  /** Ref to localContent — stable reference for callbacks without causing re-renders */
  localContentRef: React.MutableRefObject<string>;
  /** Attach this to the CodeMirror updateListener — handles all document changes */
  onUpdate: (update: ViewUpdate) => void;
}

/**
 * Hook for managing editor content updates and document change handling.
 *
 * Extracts the onUpdate callback logic from EditorPane.tsx into a cohesive unit.
 * Handles document changes by updating buffer content, tracking the modified state,
 * and triggering diagnostics for file buffers.
 *
 * Uses a ref to track localContent to avoid recreating the onUpdate callback
 * whenever content changes (which would cause an infinite loop of re-renders).
 *
 * Note: The localContent state is managed by the parent component to avoid
 * circular dependencies with useEditorFileIO.
 */
export function useEditorUpdate(params: UseEditorUpdateParams): UseEditorUpdateReturn {
  const {
    bufferRef,
    localContent,
    setLocalContent,
    cmViewApiRef,
    fetchDiagnosticsRef,
    handleCursorUpdate,
    handleScrollUpdate,
    updateBufferContent,
    setBufferModified,
  } = params;

  const localContentRef = useRef<string>(localContent);
  localContentRef.current = localContent;

  const onUpdate = useCallback(
    (update: ViewUpdate) => {
      // Forward to cursor tracking and scroll sync handlers
      handleCursorUpdate(update);

      // Handle document content changes
      if (update.docChanged && !cmViewApiRef.current?.isExternalUpdate()) {
        const newContent = update.state.doc.toString();

        // Only update state when content actually changed (prevents unnecessary re-renders)
        if (newContent !== localContentRef.current) {
          setLocalContent(newContent);
        }

        // Update buffer in global state
        const buf = bufferRef.current;
        if (buf) {
          // Only update if content actually changed to avoid unnecessary re-renders
          if (newContent !== buf.content) {
            updateBufferContent(buf.id, newContent);
            setBufferModified(buf.id, newContent !== buf.originalContent);

            // Trigger diagnostics for file buffers (excluding workspace buffers)
            if (buf.kind === 'file' && buf.file && !buf.file.path.startsWith('__workspace/')) {
              fetchDiagnosticsRef.current(buf.file.path, newContent, 'edit');
            }
          }
        }
      }

      // Forward to scroll sync handler
      handleScrollUpdate(update);
    },
    [
      bufferRef,
      fetchDiagnosticsRef,
      setLocalContent,
      cmViewApiRef,
      handleCursorUpdate,
      handleScrollUpdate,
      updateBufferContent,
      setBufferModified,
    ],
  );

  return { localContentRef, onUpdate };
}
