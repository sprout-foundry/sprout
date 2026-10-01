import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import ResizeHandle from './ResizeHandle';

describe('ResizeHandle keyboard', () => {
  it('is a focusable, labelled separator', () => {
    render(<ResizeHandle direction="horizontal" onResize={vi.fn()} ariaLabel="Resize sidebar" />);
    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    expect(handle.getAttribute('tabindex')).toBe('0');
    expect(handle.getAttribute('aria-orientation')).toBe('vertical');
  });

  it('moves along its axis with the arrow keys, farther with Shift', () => {
    const onResize = vi.fn();
    const onResizeEnd = vi.fn();
    render(<ResizeHandle direction="horizontal" onResize={onResize} onResizeEnd={onResizeEnd} />);
    const handle = screen.getByRole('separator');
    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    fireEvent.keyDown(handle, { key: 'ArrowLeft', shiftKey: true });
    fireEvent.keyDown(handle, { key: 'ArrowUp' });
    expect(onResize.mock.calls).toEqual([
      [10, 10],
      [-50, -50],
    ]);
    expect(onResizeEnd).toHaveBeenCalledTimes(2);
  });

  it('runs the reset action on Enter', () => {
    const onDoubleClick = vi.fn();
    render(<ResizeHandle direction="vertical" onResize={vi.fn()} onDoubleClick={onDoubleClick} />);
    fireEvent.keyDown(screen.getByRole('separator'), { key: 'Enter' });
    expect(onDoubleClick).toHaveBeenCalled();
  });
});
