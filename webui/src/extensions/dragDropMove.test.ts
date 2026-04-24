/**
 * dragDropMove.test.ts — Unit tests for the drag-drop move extension.
 */

import { EditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { createDragDropHandlers } from './dragDropMove';

// Mock EditorView factory
function createMockView(content: string = 'hello world', selectionFrom: number = 0, selectionTo: number = 5): EditorView {
  const state = EditorState.create({
    doc: content,
    selection: { anchor: selectionFrom, head: selectionTo },
  });

  return {
    state,
    dispatch: jest.fn(),
    posAtCoords: jest.fn().mockImplementation(function (this: EditorView, coords: { x: number; y: number }) {
      // Simple mock: return position based on y coordinate
      // For simplicity in tests, we just return a position relative to y
      // In real code, posAtCoords uses actual DOM layout
      const doc = this.state.doc;
      const lines = doc.toString().split('\n');
      // Map y to approximate line
      const lineIndex = Math.floor(coords.y / 20); // Assume 20px per line
      if (lineIndex >= lines.length) return doc.length;
      let pos = 0;
      for (let i = 0; i < lineIndex; i++) {
        pos += lines[i].length + 1; // +1 for newline
      }
      return Math.min(pos, doc.length);
    }),
  } as unknown as EditorView;
}

// Mock DragEvent factory
function createMockDragEvent(overrides: Partial<DragEvent> = {}): DragEvent {
  const preventDefault = jest.fn();
  const stopPropagation = jest.fn();
  const dataTransfer = {
    effectAllowed: 'none' as DataTransferEffectAllowed,
    dropEffect: 'none' as DataTransferDropEffect,
    ...overrides.dataTransfer,
  };

  return {
    clientX: 100,
    clientY: 100,
    altKey: false,
    preventDefault,
    stopPropagation,
    dataTransfer: dataTransfer as DataTransfer,
    ...overrides,
  } as DragEvent;
}

describe('dragDropMove handlers', () => {
  // -------------------------------------------------------------------------
  // dragstart handler
  // -------------------------------------------------------------------------

  describe('dragstart', () => {
    it('stores selection when dragging with a selection', () => {
      const view = createMockView('hello world');
      const event = createMockDragEvent();

      const handlers = createDragDropHandlers();
      const result = handlers.dragstart?.(event, view);

      expect(result).toBe(true);
      // Verify event handlers were called
      expect(event.preventDefault).toHaveBeenCalled();
      expect(event.stopPropagation).toHaveBeenCalled();
      // Verify dataTransfer.effectAllowed was set (use any since type casting can vary)
      expect(event.dataTransfer?.effectAllowed).toBe('copyMove');
    });

    it('does nothing when there is no selection', () => {
      const view = createMockView('hello world');
      // Clear selection by setting cursor at 0
      const state = EditorState.create({
        doc: 'hello world',
        selection: { anchor: 0, head: 0 },
      });

      const viewWithNoSelection = {
        state,
        dispatch: jest.fn(),
        posAtCoords: jest.fn(),
      } as unknown as EditorView;

      const event = createMockDragEvent();
      const handlers = createDragDropHandlers();
      const result = handlers.dragstart?.(event, viewWithNoSelection);

      expect(result).toBe(false);
    });
  });

  // -------------------------------------------------------------------------
  // dragover handler
  // -------------------------------------------------------------------------

  describe('dragover', () => {
    it('allows drop and sets move effect by default', () => {
      const view = createMockView();
      const event = createMockDragEvent();

      const handlers = createDragDropHandlers();
      const result = handlers.dragover?.(event, view);

      expect(result).toBe(true);
      expect(event.preventDefault).toHaveBeenCalled();
      expect(event.dataTransfer?.dropEffect).toBe('move');
    });

    it('sets copy effect when Alt key is held', () => {
      const view = createMockView();
      const event = createMockDragEvent({ altKey: true });

      const handlers = createDragDropHandlers();
      const result = handlers.dragover?.(event, view);

      expect(result).toBe(true);
      expect(event.dataTransfer?.dropEffect).toBe('copy');
    });
  });

  // -------------------------------------------------------------------------
  // drop handler
  // -------------------------------------------------------------------------

  describe('drop', () => {
    it('does nothing when there is no drag state', () => {
      const view = createMockView();
      const event = createMockDragEvent();

      const handlers = createDragDropHandlers();
      const result = handlers.drop?.(event, view);

      expect(result).toBe(false);
      expect(view.dispatch).not.toHaveBeenCalled();
    });

    it('does nothing when drop position is outside editor', () => {
      const view = createMockView();
      // First set up drag state via dragstart
      const dragEvent = createMockDragEvent();
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(dragEvent, view);

      // Now create drop event with coords that return null
      const dropEvent = createMockDragEvent();
      // Mock posAtCoords to return null
      (view.posAtCoords as jest.Mock).mockReturnValue(null);

      const result = handlers.drop?.(dropEvent, view);

      expect(result).toBe(false);
    });

    it('does nothing when drop is within source selection', () => {
      const view = createMockView('hello world'); // "hello world" is at positions 0-11
      // First set up drag state via dragstart
      const dragEvent = createMockDragEvent();
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(dragEvent, view);

      // drop at position 5 (within "hello")
      const dropEvent = createMockDragEvent();
      (view.posAtCoords as jest.Mock).mockReturnValue(5);

      const result = handlers.drop?.(dropEvent, view);

      expect(result).toBe(false);
      expect(view.dispatch).not.toHaveBeenCalled();
    });

    it('copies text when Alt key is held on drop', () => {
      const view = createMockView('hello world');
      // First set up drag state via dragstart - selection "hello" (positions 0-5)
      const dragEvent = createMockDragEvent();
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(dragEvent, view);

      // Drop at position 12 (end of document) with Alt key
      const dropEvent = createMockDragEvent({ altKey: true });
      (view.posAtCoords as jest.Mock).mockReturnValue(12);

      const result = handlers.drop?.(dropEvent, view);

      expect(result).toBe(true);
      expect(view.dispatch).toHaveBeenCalledTimes(1);

      // Verify the dispatch was called with insert-only changes
      const dispatchCall = (view.dispatch as jest.Mock).mock.calls[0][0];
      expect(dispatchCall.changes).toHaveLength(1);
      expect(dispatchCall.changes[0].insert).toBe('hello');
    });

    it('moves text when no Alt key is held on drop', () => {
      const view = createMockView('hello world');
      // First set up drag state via dragstart - selection "hello" (positions 0-5)
      const dragEvent = createMockDragEvent();
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(dragEvent, view);

      // Drop at position 12 (end of document) - moving to end
      const dropEvent = createMockDragEvent({ altKey: false });
      (view.posAtCoords as jest.Mock).mockReturnValue(12);

      const result = handlers.drop?.(dropEvent, view);

      expect(result).toBe(true);
      expect(view.dispatch).toHaveBeenCalledTimes(1);

      // Verify the dispatch was called with delete+insert changes
      const dispatchCall = (view.dispatch as jest.Mock).mock.calls[0][0];
      expect(dispatchCall.changes).toHaveLength(2);
    });

    it('adjusts drop position correctly when dropping after source in move mode', () => {
      // Select text at start (positions 0-5 = "hello")
      const view = createMockView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 10 (inside " world") - clearly after source (0-5)
      const dropEvent = createMockDragEvent({ altKey: false });
      (view.posAtCoords as jest.Mock).mockReturnValue(10);

      const result = handlers.drop?.(dropEvent, view);

      expect(result).toBe(true);
    });

    it('drops before source correctly', () => {
      // Select text at the end (positions 6-11 = "world")
      const view = createMockView('hello world', 6, 11);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 0 (clearly before the source selection at 6-11)
      const dropEvent = createMockDragEvent();
      (view.posAtCoords as jest.Mock).mockReturnValue(0);

      const result = handlers.drop?.(dropEvent, view);

      // Should succeed: drop position 0 is outside source range 6-11
      expect(result).toBe(true);
    });
  });

  // -------------------------------------------------------------------------
  // dragend handler
  // -------------------------------------------------------------------------

  describe('dragend', () => {
    it('clears drag state', () => {
      const view = createMockView('hello world');
      const handlers = createDragDropHandlers();

      // Set up drag state
      const dragEvent = createMockDragEvent();
      handlers.dragstart?.(dragEvent, view);

      // Fire dragend
      const endEvent = createMockDragEvent();
      const result = handlers.dragend?.(endEvent, view);

      expect(result).toBe(true);
      // Drag state should be cleared - next drop should do nothing
      const dropEvent = createMockDragEvent();
      const dropResult = handlers.drop?.(dropEvent, view);
      expect(dropResult).toBe(false);
    });
  });
});