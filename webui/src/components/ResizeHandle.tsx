import React, { useState, useRef, useCallback, useEffect } from 'react';
import './ResizeHandle.css';

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical'; // Direction of the split line
  onResize: (delta: number, totalDelta: number) => void;    // Called with incremental and total pixel delta during drag
  onResizeEnd?: () => void;             // Called when drag ends
  onDoubleClick?: () => void;           // Called when handle is double-clicked
  className?: string;
  position?: 'relative' | 'absolute';   // CSS position of the handle (default: 'relative')
  style?: React.CSSProperties;          // Optional inline styles
}

/**
 * ResizeHandle component for resizable split panes
 *
 * - Horizontal: Vertical divider (drag left/right to resize)
 * - Vertical: Horizontal divider (drag up/down to resize)
 */
const ResizeHandle: React.FC<ResizeHandleProps> = ({
  direction,
  onResize,
  onResizeEnd,
  onDoubleClick,
  className = '',
  position = 'relative',
  style,
}) => {
  const [isDragging, setIsDragging] = useState(false);
  const isDraggingRef = useRef(false);
  const dragStartPos = useRef<{ x: number; y: number } | null>(null);
  const lastDragPos = useRef<{ x: number; y: number } | null>(null);
  const handleRef = useRef<HTMLDivElement>(null);

  // Handle mouse down on resize handle
  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDraggingRef.current = true;
    setIsDragging(true);
    dragStartPos.current = { x: e.clientX, y: e.clientY };
    lastDragPos.current = { x: e.clientX, y: e.clientY };

    // Add global event listeners for drag
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);

    // Prevent text selection during drag
    document.body.style.userSelect = 'none';
    document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize';
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [direction]);

  // Handle mouse move during drag
  const handleMouseMove = useCallback((e: MouseEvent) => {
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
  }, [direction, onResize]);

  // Handle mouse up to end drag
  const handleMouseUp = useCallback(() => {
    isDraggingRef.current = false;
    setIsDragging(false);
    dragStartPos.current = null;
    lastDragPos.current = null;

    // Remove global event listeners
    document.removeEventListener('mousemove', handleMouseMove);
    document.removeEventListener('mouseup', handleMouseUp);

    // Restore body styles
    document.body.style.userSelect = '';
    document.body.style.cursor = '';

    // Notify drag end
    onResizeEnd?.();
  }, [handleMouseMove, onResizeEnd]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (isDraggingRef.current) {
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, [handleMouseMove, handleMouseUp]);

  return (
    <div
      ref={handleRef}
      className={`resize-handle resize-handle-${direction} ${isDragging ? 'resizing' : ''} ${className}`}
      onMouseDown={handleMouseDown}
      onDoubleClick={onDoubleClick}
      style={{
        cursor: direction === 'horizontal' ? 'col-resize' : 'row-resize',
        position,
        zIndex: isDragging ? 100 : 1,
        ...style,
      }}
    >
      {/* Visual indicator for resize handle */}
      <div className="resize-handle-indicator" />
    </div>
  );
};

export default ResizeHandle;
