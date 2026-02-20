import React, { useState, useRef, useCallback, useEffect } from 'react';
import './ResizeHandle.css';

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical'; // Direction of the split line
  onResize: (delta: number) => void;    // Called with pixel delta during drag
  onResizeEnd?: () => void;             // Called when drag ends
  className?: string;
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
  className = ''
}) => {
  const [isDragging, setIsDragging] = useState(false);
  const dragStartPos = useRef<{ x: number; y: number } | null>(null);
  const handleRef = useRef<HTMLDivElement>(null);

  // Handle mouse down on resize handle
  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDragging(true);
    dragStartPos.current = { x: e.clientX, y: e.clientY };

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
    if (!isDragging || !dragStartPos.current) return;

    const deltaX = e.clientX - dragStartPos.current.x;
    const deltaY = e.clientY - dragStartPos.current.y;

    // For horizontal split (vertical divider), use deltaX
    // For vertical split (horizontal divider), use deltaY
    const delta = direction === 'horizontal' ? deltaX : deltaY;

    onResize(delta);

    // Update start position for next move
    dragStartPos.current = { x: e.clientX, y: e.clientY };
  }, [isDragging, direction, onResize]);

  // Handle mouse up to end drag
  const handleMouseUp = useCallback(() => {
    setIsDragging(false);
    dragStartPos.current = null;

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
      if (isDragging) {
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
      }
    };
  }, [isDragging, handleMouseMove, handleMouseUp]);

  return (
    <div
      ref={handleRef}
      className={`resize-handle resize-handle-${direction} ${isDragging ? 'resizing' : ''} ${className}`}
      onMouseDown={handleMouseDown}
      style={{
        cursor: direction === 'horizontal' ? 'col-resize' : 'row-resize',
        position: 'relative',
        zIndex: isDragging ? 100 : 1
      }}
    >
      {/* Visual indicator for resize handle */}
      <div className="resize-handle-indicator" />
    </div>
  );
};

export default ResizeHandle;
