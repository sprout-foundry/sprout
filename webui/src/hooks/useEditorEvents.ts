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

import { undo, redo } from '@codemirror/commands';
import { openSearchPanel } from '@codemirror/search';
import { Transaction } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';
import { useEffect, useCallback, useRef } from 'react';
import { formatCodeWithConfigDiscovery } from '../services/formatter';
import { notificationBus } from '../services/notificationBus';
import type { EditorBuffer } from '../types/editor';
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
  onToggleInlayHints?: () => void;
  onToggleSignatureHelp?: () => void;
  onCycleTabSize?: () => void;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
  onResetZoom?: () => void;
  onToggleFormatOnSave?: () => void;
  onOpenLivePreview?: () => void;
  onToggleMarkdownPreview?: () => void;
  /** Ref tracking whether this pane is the active one. When false, the
   *  document-level event listeners early-return so commands (format, undo,
   *  goto-line, etc.) only affect the focused pane, not every mounted pane. */
  isActiveRef: React.MutableRefObject<boolean>;
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
        isActiveRef,
      } = optionsRef.current;

      // Guard: only the active editor pane should respond to document-level
      // command events. Without this check, every mounted EditorPane (split
      // panes, background tabs) processes the same command — format, undo,
      // goto-line, etc. would fire across all panes simultaneously.
      if (!isActiveRef.current) return;

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
            const replaceInput = viewRef.current?.dom.querySelector<HTMLInputElement>(
              '.cm-search input[name="replace"]',
            );
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
      } else if (e.type === 'editor-toggle-inlay-hints') {
        optionsRef.current.onToggleInlayHints?.();
      } else if (e.type === 'editor-toggle-signature-help') {
        optionsRef.current.onToggleSignatureHelp?.();
      } else if (e.type === 'editor-cycle-tab-size') {
        optionsRef.current.onCycleTabSize?.();
      } else if (e.type === 'editor-zoom-in') {
        optionsRef.current.onZoomIn?.();
      } else if (e.type === 'editor-zoom-out') {
        optionsRef.current.onZoomOut?.();
      } else if (e.type === 'editor-reset-zoom') {
        optionsRef.current.onResetZoom?.();
      } else if (e.type === 'editor-toggle-format-on-save') {
        optionsRef.current.onToggleFormatOnSave?.();
      } else if (e.type === 'editor-open-live-preview') {
        optionsRef.current.onOpenLivePreview?.();
      } else if (e.type === 'editor-toggle-markdown-preview') {
        optionsRef.current.onToggleMarkdownPreview?.();
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
    const events = [
      'editor-goto-line',
      'editor-toggle-word-wrap',
      'editor-toggle-linked-scroll',
      'editor-toggle-minimap',
      'editor-toggle-relative-line-numbers',
      'editor-cycle-whitespace-rendering',
      'editor-undo',
      'editor-redo',
      'editor-find',
      'editor-find-replace',
      'editor-select-all',
      'editor-format-document',
      'editor-find-all-references',
      'editor-go-to-workspace-symbol',
      'editor-go-to-symbol',
      'editor-toggle-inlay-hints',
      'editor-toggle-signature-help',
      'editor-cycle-tab-size',
      'editor-zoom-in',
      'editor-zoom-out',
      'editor-reset-zoom',
      'editor-toggle-format-on-save',
      'editor-open-live-preview',
      'editor-toggle-markdown-preview',
    ];
    for (const ev of events) document.addEventListener(ev, handler);
    return () => {
      for (const ev of events) document.removeEventListener(ev, handler);
    };
  }, [handler]); // Handler has stable identity now, so this effect only runs once
}
