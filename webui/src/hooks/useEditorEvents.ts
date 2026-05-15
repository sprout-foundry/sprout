/**
 * useEditorEvents — manages global DOM event listeners for editor commands.
 *
 * Extracts the giant event listener effect from EditorPane:
 * - editor-goto-line
 * - editor-toggle-word-wrap
 * - editor-toggle-linked-scroll
 * - editor-toggle-minimap
 * - editor-toggle-relative-line-numbers
 * - editor-cycle-whitespace-rendering
 * - editor-undo / editor-redo
 * - editor-find / editor-find-replace
 * - editor-select-all
 * - editor-format-document
 * - editor-find-all-references
 * - editor-go-to-workspace-symbol
 * - editor-go-to-symbol
 *
 * Target: ~250 lines
 */

import { useEffect, useCallback, useRef } from 'react';
import type { EditorView } from '@codemirror/view';
import { Transaction } from '@codemirror/state';
import { undo, redo } from '@codemirror/commands';
import { openSearchPanel } from '@codemirror/search';

import type { EditorBuffer } from '../types/editor';
import { notificationBus } from '../services/notificationBus';
import { formatCodeWithConfigDiscovery } from '../services/formatter';
import { debugLog } from '../utils/log';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorEventsOptions {
  viewRef: React.MutableRefObject<EditorView | null>;
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>;
  handleGoToLine: (line: number) => void;
  onToggleWordWrap: () => void;
  onToggleMinimap: () => void;
  onToggleRelativeLineNumbers: () => void;
  onCycleWhitespaceRendering: () => void;
  toggleLinkedScroll: () => void;
  handleFindAllReferences: () => void;
  onGoToWorkspaceSymbol?: () => void;
}

/**
 * Hook that sets up all global DOM event listeners for editor commands.
 *
 * Each event is handled by a dedicated callback function. The listeners
 * are set up in a single useEffect for efficient cleanup.
 *
 * The handler uses a ref-based pattern to ensure it never needs to change
 * identity, preventing the effect from tearing down and re-adding all 15
 * event listeners on every render.
 *
 * @param options - Configuration options with refs and callbacks
 */
export function useEditorEvents(options: UseEditorEventsOptions): void {
  // Store options in a ref so the handler can always read the latest values
  // without needing to recreate its identity. This prevents the useEffect
  // from tearing down and re-adding all 15 event listeners on every render.
  const optionsRef = useRef(options);
  optionsRef.current = options;

  // ---------------------------------------------------------------------------
  // Event handler (stable identity)
  // ---------------------------------------------------------------------------

  const handler = useCallback((e: Event) => {
    try {
      const {
        viewRef,
        bufferRef,
        handleGoToLine,
        onToggleWordWrap,
        onToggleMinimap,
        onToggleRelativeLineNumbers,
        onCycleWhitespaceRendering,
        toggleLinkedScroll,
        handleFindAllReferences,
        onGoToWorkspaceSymbol,
      } = optionsRef.current;

      if (e.type === 'editor-goto-line') {
        const customEvent = e as CustomEvent;
        if (customEvent.detail?.line) {
          handleGoToLine(customEvent.detail.line);
        }
      } else if (e.type === 'editor-toggle-word-wrap') {
        onToggleWordWrap();
      } else if (e.type === 'editor-toggle-linked-scroll') {
        toggleLinkedScroll();
      } else if (e.type === 'editor-toggle-minimap') {
        onToggleMinimap();
      } else if (e.type === 'editor-toggle-relative-line-numbers') {
        onToggleRelativeLineNumbers();
      } else if (e.type === 'editor-cycle-whitespace-rendering') {
        onCycleWhitespaceRendering();
      } else if (e.type === 'editor-undo') {
        if (viewRef.current) {
          undo(viewRef.current);
        }
      } else if (e.type === 'editor-redo') {
        if (viewRef.current) {
          redo(viewRef.current);
        }
      } else if (e.type === 'editor-find') {
        if (viewRef.current) {
          openSearchPanel(viewRef.current);
        }
      } else if (e.type === 'editor-find-replace') {
        if (viewRef.current) {
          openSearchPanel(viewRef.current);
          requestAnimationFrame(() => {
            const replaceInput = viewRef.current?.dom.querySelector<HTMLInputElement>('.cm-search input[name="replace"]');
            if (replaceInput) {
              replaceInput.focus();
              replaceInput.select();
            }
          });
        }
      } else if (e.type === 'editor-select-all') {
        if (viewRef.current) {
          viewRef.current.dispatch({
            selection: { anchor: 0, head: viewRef.current.state.doc.length },
            annotations: [Transaction.addToHistory.of(false)],
          });
        }
      } else if (e.type === 'editor-format-document') {
        const currentBuffer = bufferRef.current;
        if (viewRef.current && currentBuffer) {
          const content = viewRef.current.state.doc.toString();
          formatCodeWithConfigDiscovery(content, currentBuffer.file.path, currentBuffer.file.size).then((result) => {
            if (bufferRef.current?.id !== currentBuffer.id) return;
            if (result.error) {
              notificationBus.notify('warning', 'Format Document', `Format failed: ${result.error}`);
              return;
            }
            if (result.formatted !== content && viewRef.current) {
              // Bail out if the user edited while formatting was in progress
              if (viewRef.current.state.doc.toString() !== content) return;
              viewRef.current.dispatch({
                changes: {
                  from: 0,
                  to: viewRef.current.state.doc.length,
                  insert: result.formatted,
                },
                annotations: [Transaction.addToHistory.of(false)],
              });
            }
          });
        }
      } else if (e.type === 'editor-find-all-references') {
        handleFindAllReferences();
      } else if (e.type === 'editor-go-to-workspace-symbol') {
        onGoToWorkspaceSymbol?.();
      } else if (e.type === 'editor-go-to-symbol') {
        window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_goto_symbol' } }));
      }
    } catch (err) {
      debugLog('[useEditorEvents] Error handling editor event:', e.type, err);
      notificationBus.notify('error', 'Editor Command', `An unexpected error occurred: ${(err as Error).message}`);
    }
  }, []); // Empty deps: handler identity never changes, always reads from optionsRef

  // ---------------------------------------------------------------------------
  // Set up all event listeners
  // ---------------------------------------------------------------------------

  useEffect(() => {
    document.addEventListener('editor-goto-line', handler);
    document.addEventListener('editor-toggle-word-wrap', handler);
    document.addEventListener('editor-toggle-linked-scroll', handler);
    document.addEventListener('editor-toggle-minimap', handler);
    document.addEventListener('editor-toggle-relative-line-numbers', handler);
    document.addEventListener('editor-cycle-whitespace-rendering', handler);
    document.addEventListener('editor-undo', handler);
    document.addEventListener('editor-redo', handler);
    document.addEventListener('editor-find', handler);
    document.addEventListener('editor-find-replace', handler);
    document.addEventListener('editor-select-all', handler);
    document.addEventListener('editor-format-document', handler);
    document.addEventListener('editor-find-all-references', handler);
    document.addEventListener('editor-go-to-workspace-symbol', handler);
    document.addEventListener('editor-go-to-symbol', handler);

    return () => {
      document.removeEventListener('editor-goto-line', handler);
      document.removeEventListener('editor-toggle-word-wrap', handler);
      document.removeEventListener('editor-toggle-linked-scroll', handler);
      document.removeEventListener('editor-toggle-minimap', handler);
      document.removeEventListener('editor-toggle-relative-line-numbers', handler);
      document.removeEventListener('editor-cycle-whitespace-rendering', handler);
      document.removeEventListener('editor-undo', handler);
      document.removeEventListener('editor-redo', handler);
      document.removeEventListener('editor-find', handler);
      document.removeEventListener('editor-find-replace', handler);
      document.removeEventListener('editor-select-all', handler);
      document.removeEventListener('editor-format-document', handler);
      document.removeEventListener('editor-find-all-references', handler);
      document.removeEventListener('editor-go-to-workspace-symbol', handler);
      document.removeEventListener('editor-go-to-symbol', handler);
    };
  }, [handler]); // Handler has stable identity now, so this effect only runs once
}
