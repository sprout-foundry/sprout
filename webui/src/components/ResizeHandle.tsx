import { useState, useRef, useCallback, useEffect } from 'react';
import type { CSSProperties, PointerEvent as ReactPointerEvent, MouseEvent as ReactMouseEvent } from 'react';
import './ResizeHandle.css';

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical'; // Direction of the split line
  onResize: (delta: number, totalDelta: number) => void; // Called with incremental and total pixel delta during drag
  onResizeStart?: () => void; // Called when drag starts
  onResizeEnd?: () => void; // Called when drag ends
  onDoubleClick?: () => void; // Called when handle is double-clicked
  className?: string;
  position?: 'relative' | 'absolute'; // CSS position of the handle (default: 'relative')
  style?: CSSProperties; // Optional inline styles
}

/**
 * ResizeHandle component for resizable split panes
 *
 * - Horizontal: Vertical divider (drag left/right to resize)
 * - Vertical: Horizontal divider (drag up/down to resize)
 *
 * Drag tracking uses POINTER events, not mouse events: iPadOS/iOS Safari
 * cancels the synthesized mouse-event stream as soon as a finger moves,
 * which made these handles mouse/trackpad-only on tablets (the handle
 * appeared "finicky" — it started a drag then immediately stalled).
 * Pointer events cover mouse, trackpad, touch, and pencil uniformly.
 * `touch-action: none` in ResizeHandle.css keeps the browser from
 * claiming the gesture for scrolling.
 */
function ResizeHandle({
  direction,
  onResize,
  onResizeStart,
  onResizeEnd,
  onDoubleClick,
  className = '',
  position = 'relative',
  style,
}: ResizeHandleProps): JSX.Element {
  const [isDragging, setIsDragging] = useState(false);
  const isDraggingRef = useRef(false);
  const dragStartPos = useRef<{ x: number; y: number } | null>(null);
  const lastDragPos = useRef<{ x: number; y: number } | null>(null);
  const handleRef = useRef<HTMLDivElement>(null);

  // PointerEvent is universal in shipping Safari/Chrome/Firefox; the mouse
  // fallback only matters for very old jsdom-style environments.
  const supportsPointer = typeof window !== 'undefined' && typeof window.PointerEvent === 'function';

  // Handle move during drag
  const handleMove = useCallback(
    (e: MouseEvent | PointerEvent) => {
      if (!isDraggingRef.current || !dragStartPos.current) return;

      const deltaX = e.clientX - (lastDragPos.current?.x ?? dragStartPos.current.x);
      const deltaY = e.clientY - (lastDragPos.current?.y ?? dragStartPos.current.y);
      const totalDeltaX = e.clientX - dragStartPos.current.x;
      const totalDeltaY = e.clientY - dragStartPos.current.y;

      // For horizontal split (vertical divider), use deltaX
      // For vertical split (horizontal divider), use deltaY
      const delta = direction === 'horizontal' ? deltaX : deltaY;
      const totalDelta = direction === 'horizontal' ? totalDeltaX : totalDeltaY;

      onResize(delta, totalDelta);

      lastDragPos.current = { x: e.clientX, y: e.clientY };
    },
    [direction, onResize],
  );

  // Handle drag end (pointer up OR pointercancel — iOS can cancel a
  // pointer mid-gesture, e.g. when the system intercepts the touch).
  const handleDragEnd = useCallback(() => {
    if (!isDraggingRef.current) return;
    isDraggingRef.current = false;
    setIsDragging(false);
    dragStartPos.current = null;
    lastDragPos.current = null;

    // Remove global event listeners
    if (supportsPointer) {
      window.removeEventListener('pointermove', handleMove);
      window.removeEventListener('pointerup', handleDragEnd);
      window.removeEventListener('pointercancel', handleDragEnd);
    } else {
      document.removeEventListener('mousemove', handleMove);
      document.removeEventListener('mouseup', handleDragEnd);
    }

    // Restore body styles
    document.body.style.userSelect = '';
    document.body.style.cursor = '';

    // Notify drag end
    onResizeEnd?.();
  }, [handleMove, onResizeEnd, supportsPointer]);

  const beginDrag = useCallback(
    (x: number, y: number) => {
      isDraggingRef.current = true;
      setIsDragging(true);
      dragStartPos.current = { x, y };
      lastDragPos.current = { x, y };

      // Notify drag start
      onResizeStart?.();

      if (supportsPointer) {
        window.addEventListener('pointermove', handleMove);
        window.addEventListener('pointerup', handleDragEnd);
        window.addEventListener('pointercancel', handleDragEnd);
      } else {
        document.addEventListener('mousemove', handleMove);
        document.addEventListener('mouseup', handleDragEnd);
      }

      // Prevent text selection during drag
      document.body.style.userSelect = 'none';
      document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize';
    },
    [direction, handleMove, handleDragEnd, onResizeStart, supportsPointer],
  );

  const handlePointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      // Capture the pointer so moves keep tracking even if the finger
      // drifts off the (thin) handle — critical on touch, where the hit
      // target is small relative to finger contact area.
      try {
        e.currentTarget.setPointerCapture(e.pointerId);
      } catch {
        // best-effort: capture is an optimization; window-level listeners
        // below already track moves outside the element.
      }
      beginDrag(e.clientX, e.clientY);
    },
    [beginDrag],
  );

  const handleMouseDown = useCallback(
    (e: ReactMouseEvent) => {
      e.preventDefault();
      beginDrag(e.clientX, e.clientY);
    },
    [beginDrag],
  );

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (isDraggingRef.current) {
        if (supportsPointer) {
          window.removeEventListener('pointermove', handleMove);
          window.removeEventListener('pointerup', handleDragEnd);
          window.removeEventListener('pointercancel', handleDragEnd);
        } else {
          document.removeEventListener('mousemove', handleMove);
          document.removeEventListener('mouseup', handleDragEnd);
        }
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        isDraggingRef.current = false;
      }
    };
  }, [handleMove, handleDragEnd, supportsPointer]);

  const dragProps = supportsPointer ? { onPointerDown: handlePointerDown } : { onMouseDown: handleMouseDown };

  return (
    <div
      ref={handleRef}
      className={`resize-handle resize-handle-${direction} ${isDragging ? 'resizing' : ''} ${className}`}
      onDoubleClick={onDoubleClick}
      style={{
        cursor: direction === 'horizontal' ? 'col-resize' : 'row-resize',
        position,
        zIndex: isDragging ? 100 : 1,
        ...style,
      }}
      {...dragProps}
    >
      {/* Visual indicator for resize handle */}
      <div className="resize-handle-indicator" />
    </div>
  );
}

export default ResizeHandle;
