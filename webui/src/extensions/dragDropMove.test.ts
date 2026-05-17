/**
 * dragDropMove.test.ts — Unit tests for the drag-drop move extension.
 *
 * Uses a minimal real CodeMirror EditorView (no DOM) so that dispatched
 * transactions are actually applied to the document state. This lets us
 * verify the resulting document content, not just that dispatch was called.
 */

// Polyfill Range.prototype.getClientRects for jsdom (CodeMirror needs it)
if (typeof Range !== 'undefined' && !Range.prototype.getClientRects) {
  Range.prototype.getClientRects = function () {
    return [];
  };
}

import { EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { createDragDropHandlers } from './dragDropMove';

// ── Helper: create a minimal real EditorView ────────────────────────
// We use a real EditorView so that dispatch() applies changes to the
// document state, allowing us to verify resulting content. The view
// is constructed with minimal extensions and a mock DOM element.

function createRealView(
  content: string = 'hello world',
  selectionFrom: number = 0,
  selectionTo: number = 5,
): EditorView {
  // Create a stub DOM element with enough shape for EditorView
  const dom = {
    style: {} as CSSStyleDeclaration,
    ownerDocument: {
      defaultView: null as unknown as Window,
      documentElement: { style: { setProperty: () => {} } } as HTMLElement,
    } as unknown as Document,
    appendChild: () => {},
    removeChild: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    getBoundingClientRect: () => ({ left: 0, top: 0, right: 800, bottom: 600 }),
    querySelectorAll: () => [] as Element[],
    querySelector: () => null as Element | null,
    childNodes: [] as ChildNode[],
    firstChild: null as ChildNode | null,
    className: '',
    setAttribute: () => {},
    removeAttribute: () => {},
    contains: () => false,
    dispatchEvent: () => false,
    nodeName: 'DIV',
    nodeType: 1,
    parentNode: null as Node | null,
    nextSibling: null as Node | null,
    previousSibling: null as Node | null,
  };

  const state = EditorState.create({
    doc: content,
    selection: { anchor: selectionFrom, head: selectionTo },
  });

  const view = new EditorView({
    state,
    dom,
  });

  // Override posAtCoords since we have no real DOM layout.
  // Tests will control this via the withDropPos helper.
  return view;
}

// ── Helper: override posAtCoords for a specific test ────────────────

function withDropPos(view: EditorView, pos: number | null): EditorView {
  // We monkey-patch posAtCoords to return a controlled position
  (view as unknown as Record<string, unknown>).posAtCoords = () => pos;
  return view;
}

// ── Mock DragEvent factory ──────────────────────────────────────────

function createMockDragEvent(overrides: Partial<DragEvent> = {}): DragEvent {
  const preventDefault = vi.fn();
  const stopPropagation = vi.fn();
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

// ── Mock view factory (for tests that don't need document verification) ──

function createMockView(
  content: string = 'hello world',
  selectionFrom: number = 0,
  selectionTo: number = 5,
): EditorView {
  const state = EditorState.create({
    doc: content,
    selection: { anchor: selectionFrom, head: selectionTo },
  });

  return {
    state,
    dispatch: vi.fn(),
    posAtCoords: vi.fn().mockReturnValue(0),
  } as unknown as EditorView;
}

// ══════════════════════════════════════════════════════════════════════
// Tests
// ══════════════════════════════════════════════════════════════════════

describe('dragDropMove handlers', () => {
  // -----------------------------------------------------------------------
  // dragstart handler
  // -----------------------------------------------------------------------

  describe('dragstart', () => {
    it('stores selection when dragging with a selection', () => {
      const view = createMockView('hello world');
      const event = createMockDragEvent();

      const handlers = createDragDropHandlers();
      const result = handlers.dragstart?.(event, view);

      // Return false to let the native drag proceed
      expect(result).toBe(false);
      // Setting effectAllowed is fine - doesn't cancel the drag
      expect(event.dataTransfer?.effectAllowed).toBe('copyMove');
    });

    it('does nothing when there is no selection', () => {
      const state = EditorState.create({
        doc: 'hello world',
        selection: { anchor: 0, head: 0 },
      });
      const viewWithNoSelection = {
        state,
        dispatch: vi.fn(),
        posAtCoords: vi.fn(),
      } as unknown as EditorView;

      const event = createMockDragEvent();
      const handlers = createDragDropHandlers();
      const result = handlers.dragstart?.(event, viewWithNoSelection);

      expect(result).toBe(false);
    });
  });

  // -----------------------------------------------------------------------
  // dragover handler
  // -----------------------------------------------------------------------

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

  // -----------------------------------------------------------------------
  // drop handler — document content verification tests
  // -----------------------------------------------------------------------

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
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      withDropPos(view, null);
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(false);
      expect(view.dispatch).not.toHaveBeenCalled();
    });

    it('does nothing when document was modified between dragstart and drop', () => {
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Modify the document between dragstart and drop
      view.dispatch({ changes: { from: 0, to: 5, insert: 'XXXXX' } });

      // Drop should be a no-op because stored positions are stale
      withDropPos(view, 11);
      const result = handlers.drop?.(createMockDragEvent(), view);
      expect(result).toBe(false);
    });

    it('does nothing when drop is within source selection', () => {
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 3 (within source 0-5)
      withDropPos(view, 3);
      const before = view.state.doc.toString();
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(false);
      expect(view.state.doc.toString()).toBe(before);
    });

    it('does nothing when drop is at exact source boundary', () => {
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 5 (exact end of source selection)
      withDropPos(view, 5);
      const before = view.state.doc.toString();
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(false);
      expect(view.state.doc.toString()).toBe(before);
    });

    it('copies text to end of document when Alt key is held', () => {
      // Doc: "hello world", selection "hello" (0-5)
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 11 (end of doc) with Alt key
      withDropPos(view, 11);
      const result = handlers.drop?.(createMockDragEvent({ altKey: true }), view);

      expect(result).toBe(true);
      // Copy: original kept, text inserted at end
      expect(view.state.doc.toString()).toBe('hello worldhello');
    });

    it('copies text to middle of document when Alt key is held', () => {
      // Doc: "hello world", selection "hello" (0-5)
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 6 with Alt key
      withDropPos(view, 6);
      const result = handlers.drop?.(createMockDragEvent({ altKey: true }), view);

      expect(result).toBe(true);
      // Copy: original kept, "hello" inserted before "w"
      expect(view.state.doc.toString()).toBe('hello helloworld');
    });

    it('moves text from start to end of document', () => {
      // Doc: "hello world", selection "hello" (0-5)
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 11 (end of doc)
      withDropPos(view, 11);
      const result = handlers.drop?.(createMockDragEvent({ altKey: false }), view);

      expect(result).toBe(true);
      // "hello" moved to end, " world" left at start
      expect(view.state.doc.toString()).toBe(' worldhello');
    });

    it('moves text from end to start of document', () => {
      // Doc: "hello world", selection "world" (6-11)
      const view = createRealView('hello world', 6, 11);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 0 (start of doc)
      withDropPos(view, 0);
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(true);
      // "world" moved to start, "hello " left behind
      expect(view.state.doc.toString()).toBe('worldhello ');
    });

    it('moves text from middle to end across multiline document', () => {
      // Doc: "line one\ntwo\nthree" (18 chars), selection "two" (9-12)
      const view = createRealView('line one\ntwo\nthree', 9, 12);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 18 (past end of doc — end of "three")
      withDropPos(view, 18);
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(true);
      // "two" removed from middle, inserted at end
      expect(view.state.doc.toString()).toBe('line one\n\nthreetwo');
    });

    it('moves text from start to middle of document', () => {
      // Doc: "abcdefghij", selection "abc" (0-3)
      const view = createRealView('abcdefghij', 0, 3);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 7 (original position, before adjustment)
      withDropPos(view, 7);
      const result = handlers.drop?.(createMockDragEvent(), view);

      expect(result).toBe(true);
      // "abc" removed from start (doc becomes "defghij"), then inserted at adjusted pos (7-3=4)
      // "defg" + "abc" + "hij" = "defgabchij"
      expect(view.state.doc.toString()).toBe('defgabchij');
    });

    it('selects the moved text after drop', () => {
      // Doc: "hello world", selection "hello" (0-5)
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 11
      withDropPos(view, 11);
      handlers.drop?.(createMockDragEvent(), view);

      // Selection should be on the moved text
      const sel = view.state.selection.main;
      expect(view.state.sliceDoc(sel.from, sel.to)).toBe('hello');
    });

    it('selects the copied text after Alt-drop', () => {
      // Doc: "hello world", selection "hello" (0-5)
      const view = createRealView('hello world', 0, 5);
      const handlers = createDragDropHandlers();
      handlers.dragstart?.(createMockDragEvent(), view);

      // Drop at position 11 with Alt
      withDropPos(view, 11);
      handlers.drop?.(createMockDragEvent({ altKey: true }), view);

      // Selection should be on the copied text at the destination
      const sel = view.state.selection.main;
      expect(view.state.sliceDoc(sel.from, sel.to)).toBe('hello');
    });
  });

  // -----------------------------------------------------------------------
  // dragend handler
  // -----------------------------------------------------------------------

  describe('dragend', () => {
    it('clears drag state', () => {
      const view = createMockView('hello world');
      const handlers = createDragDropHandlers();

      handlers.dragstart?.(createMockDragEvent(), view);

      const endEvent = createMockDragEvent();
      const result = handlers.dragend?.(endEvent, view);

      expect(result).toBe(true);
      // Drag state should be cleared — next drop should do nothing
      const dropResult = handlers.drop?.(createMockDragEvent(), view);
      expect(dropResult).toBe(false);
    });

    it('returns false when there is no drag state', () => {
      const view = createMockView('hello world');
      const handlers = createDragDropHandlers();

      const result = handlers.dragend?.(createMockDragEvent(), view);

      expect(result).toBe(false);
    });
  });
});
