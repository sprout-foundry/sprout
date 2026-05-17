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

import type { ChangeSpec } from '@codemirror/state';
import { Prec } from '@codemirror/state';
import { EditorView, type DOMEventHandlers } from '@codemirror/view';

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
        event.dataTransfer.effectAllowed = 'copyMove';
      }

      // Return false to let the native drag proceed (CM6 will handle it)
      return false;
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

      // Validate that the stored positions still correspond to the selected text.
      // This handles the case where the document was modified between dragstart and drop.
      const currentText = view.state.sliceDoc(state.from, state.to);
      if (currentText !== state.text) {
        dragStateMap.delete(view);
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

      // Build the transaction with appropriate changes.
      //
      // CM6 applies multiple changes in a single transaction by sorting them
      // by `from` position and mapping each change through previous ones
      // automatically. All positions must be in ORIGINAL document coordinates.
      //
      // For move operations we need two changes (delete source + insert at
      // destination). The order in the changes array does not matter — CM6
      // sorts them. We just need to use original coords everywhere.
      const changes: ChangeSpec[] = [];

      if (isCopy) {
        // Copy mode: just insert the text at drop position (original coords)
        changes.push({
          from: dropPos,
          to: dropPos,
          insert: text,
        });
      } else {
        // Move mode: delete source + insert at destination, all in original coords
        changes.push({
          from: srcFrom,
          to: srcTo,
          insert: '',
        });
        changes.push({
          from: dropPos,
          to: dropPos,
          insert: text,
        });
      }

      // For the selection, we need the POST-change position of the inserted
      // text. CM6 maps positions through changes, so we compute the expected
      // final position based on whether the insertion is before or after
      // the deletion.
      let selAnchor: number;
      let selHead: number;

      if (isCopy) {
        selAnchor = dropPos;
        selHead = dropPos + text.length;
      } else if (dropPos < srcFrom) {
        // Insertion is before the deletion. The inserted text stays at dropPos.
        selAnchor = dropPos;
        selHead = dropPos + text.length;
      } else {
        // dropPos > srcTo (guaranteed by the guard above).
        // CM6 sorts: deletion (srcFrom..srcTo) comes before insertion
        // (dropPos..dropPos). After the deletion removes (srcTo-srcFrom)
        // characters, the insertion point shifts left by that amount.
        selAnchor = dropPos - (srcTo - srcFrom);
        selHead = selAnchor + text.length;
      }

      // Apply the changes using the view from the event
      view.dispatch({
        changes,
        // Select the inserted text at its final position
        selection: {
          anchor: selAnchor,
          head: selHead,
        },
        scrollIntoView: true,
        userEvent: isCopy ? 'input.drop' : 'move.drop',
      });

      // Clear the drag state for this view
      dragStateMap.delete(view);

      return true;
    },

    dragend: (_event: DragEvent, view: EditorView) => {
      // Clear drag state on dragend (handles cancelled drags)
      if (dragStateMap.has(view)) {
        dragStateMap.delete(view);
        return true;
      }
      return false;
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
export const dragDropMovePlugin = Prec.high(EditorView.domEventHandlers(createDragDropHandlers()));
