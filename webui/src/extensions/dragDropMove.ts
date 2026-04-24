/**
 * dragDropMove.ts — CodeMirror 6 extension for drag-and-drop text movement.
 *
 * Allows users to drag selected text within the editor and drop it at a new position.
 * - Hold Alt while dropping to copy (keep original)
 * - Without Alt, move the text to the new position (delete original)
 *
 * Architecture:
 * - A ViewPlugin is NOT used; instead we use EditorView.domEventHandlers
 *   to attach DOM event handlers for dragstart, dragover, drop, dragend.
 * - Drag state is stored in a WeakMap keyed by EditorView, ensuring each
 *   editor instance has its own drag state. This prevents state corruption
 *   when multiple editor panes exist (each with their own EditorView).
 */

import { EditorView, type DOMEventHandlers } from '@codemirror/view';
import { Transaction, type ChangeSpec } from '@codemirror/state';

// ── Types ───────────────────────────────────────────────────────────

interface DragState {
  /** Starting position of the selection (from). */
  from: number;
  /** Ending position of the selection (to). */
  to: number;
  /** Selected text content. */
  text: string;
}

// ── Per-editor drag state ───────────────────────────────────────────
// Stored per-editor-instance using WeakMap. This ensures each EditorView
// has its own drag state, preventing corruption when multiple editors exist.

const dragStateMap = new WeakMap<EditorView, DragState>();

// ── DOM Event Handlers ─────────────────────────────────────────

/**
 * Create the drag-and-drop event handlers for a CodeMirror editor.
 *
 * We return an object conforming to DOMEventHandlers so it can be
 * passed directly to EditorView.domEventHandlers.
 */
function createDragDropHandlers(): DOMEventHandlers<null> {
  return {
    dragstart: (event: DragEvent, view: EditorView) => {
      const selection = view.state.selection.main;

      // Only intercept if there's a non-empty selection
      if (selection.empty) {
        return false;
      }

      // Store the selection range and content in per-editor state
      const text = view.state.sliceDoc(selection.from, selection.to);
      dragStateMap.set(view, {
        from: selection.from,
        to: selection.to,
        text,
      });

      // Signal move+copy support to browser
      if (event.dataTransfer) {
        (event.dataTransfer as DataTransfer).effectAllowed = 'copyMove';
      }

      // Prevent default browser drag behavior - we handle it ourselves
      event.preventDefault();
      event.stopPropagation();

      return true;
    },

    dragover: (event: DragEvent, _view: EditorView) => {
      // Allow drop
      event.preventDefault();

      // Set drop effect based on Alt key
      if (event.dataTransfer) {
        // Alt key = copy mode, otherwise move
        event.dataTransfer.dropEffect = event.altKey ? 'copy' : 'move';
      }

      return true;
    },

    drop: (event: DragEvent, view: EditorView) => {
      // Only handle if we have stored drag state for this view
      const state = dragStateMap.get(view);
      if (!state) {
        return false;
      }

      // Get drop position from mouse coordinates
      const dropPos = view.posAtCoords({ x: event.clientX, y: event.clientY });

      // If coordinates are outside the editor, do nothing
      if (dropPos === null) {
        dragStateMap.delete(view);
        return false;
      }

      const { from: srcFrom, to: srcTo, text } = state;

      // If drop position is within the source selection, do nothing
      // (user dropped back on same text)
      if (dropPos >= srcFrom && dropPos <= srcTo) {
        dragStateMap.delete(view);
        return false;
      }

      // Determine if this is a copy or move operation
      const isCopy = event.altKey;

      // Calculate adjusted position:
      // If dropping AFTER the source and this is a move (deletion),
      // we need to account for the deleted text shifting positions.
      let adjustedDropPos = dropPos;
      if (!isCopy && dropPos > srcTo) {
        adjustedDropPos = dropPos - (srcTo - srcFrom);
      } else if (!isCopy && dropPos > srcFrom) {
        // Dropping within the source range - adjust to start of selection
        adjustedDropPos = srcFrom;
      }

      // Build the transaction with appropriate changes
      const changes: ChangeSpec[] = [];

      if (isCopy) {
        // Copy mode: just insert the text at drop position
        changes.push({
          from: adjustedDropPos,
          to: adjustedDropPos,
          insert: text,
        });
      } else {
        // Move mode: delete source, then insert at destination
        // Use a single dispatch with both changes to avoid flicker

        if (adjustedDropPos < srcFrom) {
          // Dropping before source: insert first, then delete the original
          // After inserting at adjustedDropPos, the original text shifts by text.length positions
          // So we delete from (adjustedDropPos + text.length) for (srcTo - srcFrom) characters
          changes.push({
            from: adjustedDropPos,
            to: adjustedDropPos,
            insert: text,
          });
          changes.push({
            from: adjustedDropPos + text.length,
            to: adjustedDropPos + text.length + (srcTo - srcFrom),
            insert: '',
          });
        } else {
          // Dropping after source: delete first, then insert
          // (adjustedDropPos already accounts for the shift)
          changes.push({
            from: srcFrom,
            to: srcTo,
            insert: '',
          });
          changes.push({
            from: adjustedDropPos,
            to: adjustedDropPos,
            insert: text,
          });
        }
      }

      // Apply the changes using the view from the event
      view.dispatch({
        changes,
        // Don't record this in undo history
        annotations: Transaction.addToHistory.of(false),
        // Select the inserted text
        selection: {
          anchor: adjustedDropPos,
          head: adjustedDropPos + text.length,
        },
        scrollIntoView: true,
      });

      // Clear the drag state for this view
      dragStateMap.delete(view);

      return true;
    },

    dragend: (_event: DragEvent, view: EditorView) => {
      // Clear drag state on dragend (handles cancelled drags)
      dragStateMap.delete(view);
      return true;
    },
  };
}

// ── Exported helpers for testing ────────────────────────────────────────

/**
 * Export the handler factory for testing.
 * This function creates fresh DOMEventHandlers that share module-level state.
 */
export { createDragDropHandlers };

// ── Exported plugin ───────────────────────────────────────────────────

/**
 * CodeMirror 6 extension for drag-and-drop text movement within the editor.
 *
 * Usage:
 *   import { dragDropMovePlugin } from './extensions/dragDropMove';
 *   const editor = new EditorView({
 *     extensions: [dragDropMovePlugin, ...],
 *   });
 *
 * Behavior:
 * - Drag selected text to move it to a new position
 * - Hold Alt while dropping to copy (keep original text)
 * - Dropping on the same selection is a no-op
 */
export const dragDropMovePlugin = EditorView.domEventHandlers(createDragDropHandlers());