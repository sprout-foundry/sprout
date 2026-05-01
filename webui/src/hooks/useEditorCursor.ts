/**
 * useEditorCursor — manages cursor position tracking and selection state for the editor.
 *
 * Provides:
 * - Cursor position tracking (line/column) persistence to buffer state
 * - Selection info tracking (character count, selection count)
 * - Selection state reset on file load
 *
 * @see EditorPane.tsx for the original implementation this hook extracts
 */

import { useState, useCallback } from 'react';
import type { EditorBuffer } from '../types/editor';
import type { ViewUpdate } from '@codemirror/view';
import { debugLog } from '../utils/log';

export interface SelectionInfo {
  charCount: number;
  selectionCount: number;
}

export interface UseEditorCursorOptions {
  /** Ref to the current buffer — avoids stale closures in the update listener */
  bufferRef: React.RefObject<EditorBuffer | null | undefined>;
  /** From EditorManagerContext — updates cursor position in buffer state */
  updateBufferCursor: (bufferId: string, pos: { line: number; column: number }) => void;
}

export interface UseEditorCursorReturn {
  /** Current selection info (null when no text is selected) */
  selectionInfo: SelectionInfo | null;
  /** Setter for selection info — used by file load to reset selection state */
  setSelectionInfo: React.Dispatch<React.SetStateAction<SelectionInfo | null>>;
  /** Handle a CodeMirror editor update — extracts cursor position and selection info */
  handleCursorUpdate: (update: ViewUpdate) => void;
}

/**
 * Hook for managing cursor position tracking and selection state.
 *
 * Extracts cursor position (line/column) and selection info from CodeMirror
 * update events, persisting cursor position to buffer state and maintaining
 * local selection info state for UI display (e.g., footer status).
 */
export function useEditorCursor(options: UseEditorCursorOptions): UseEditorCursorReturn {
  const { bufferRef, updateBufferCursor } = options;

  const [selectionInfo, setSelectionInfo] = useState<SelectionInfo | null>(null);

  const handleCursorUpdate = useCallback(
    (update: ViewUpdate) => {
      // Update cursor position on ANY selection change (cursor moves, clicks, typing)
      if (update.selectionSet) {
        const buf = bufferRef.current;
        if (buf) {
          try {
            const selection = update.state.selection.main;
            if (selection) {
              const line = update.state.doc.lineAt(selection.head).number;
              const column = selection.head - update.state.doc.lineAt(selection.head).from;
              updateBufferCursor(buf.id, { line, column });
            }
          } catch (err) {
            debugLog('Cursor position update skipped:', err);
          }
        }

        // Update selection info on selection change
        const sel = update.state.selection;
        const ranges = sel.ranges;
        if (ranges.length > 1) {
          // Multiple selections — show count and total chars
          const totalChars = ranges.reduce((sum, range) => sum + (range.to - range.from), 0);
          setSelectionInfo({ charCount: totalChars, selectionCount: ranges.length });
        } else if (ranges.length === 1 && !ranges[0].empty) {
          // Single non-empty selection — show character count
          const charCount = ranges[0].to - ranges[0].from;
          setSelectionInfo({ charCount, selectionCount: 1 });
        } else {
          // No selection (just a cursor)
          setSelectionInfo(null);
        }
      }
    },
    [bufferRef, updateBufferCursor],
  );

  return { selectionInfo, setSelectionInfo, handleCursorUpdate };
}
