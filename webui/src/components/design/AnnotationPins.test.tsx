/**
 * AnnotationPins tests (SP-140-6 §6e, TODO item 6.5).
 *
 * Pins the pin-layer contract: annotations render at their normalized `at`
 * coordinates, open vs resolved are visually distinct, pin click focuses,
 * placement mode converts a layer click to a normalized point, and pin clicks
 * never place during placement mode.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { DesignFeedbackAnnotation } from '../../services/api/types';
import AnnotationPins from './AnnotationPins';

function annotation(overrides: Partial<DesignFeedbackAnnotation> = {}): DesignFeedbackAnnotation {
  return {
    id: 'a1',
    at: { x: 0.25, y: 0.75 },
    area: 'hierarchy',
    note: 'CTA reads secondary',
    resolved: false,
    created: '2026-09-19T09:00:00Z',
    ...overrides,
  };
}

describe('AnnotationPins', () => {
  it('renders one pin per annotation at its normalized coordinates', () => {
    const annotations = [
      annotation(),
      annotation({ id: 'a2', at: { x: 0.9, y: 0.1 }, area: 'contrast', resolved: true }),
    ];
    render(<AnnotationPins target="design/screens/login.html" annotations={annotations} />);
    const a1 = screen.getByTestId('design-pin-a1');
    expect(a1).toHaveStyle({ left: '25.00%', top: '75.00%' });
    expect(a1).toHaveAttribute('data-area', 'hierarchy');
    expect(a1).toHaveAttribute('data-resolved', 'false');
    const a2 = screen.getByTestId('design-pin-a2');
    expect(a2).toHaveStyle({ left: '90.00%', top: '10.00%' });
    expect(a2).toHaveAttribute('data-resolved', 'true');
    expect(a2).toHaveClass('design-pin-resolved');
    expect(a1).toHaveClass('design-pin-open');
  });

  it('clamps malformed coordinates into range instead of off-layer', () => {
    render(<AnnotationPins target="t" annotations={[annotation({ at: { x: 1.7, y: -0.5 } })]} />);
    expect(screen.getByTestId('design-pin-a1')).toHaveStyle({ left: '100.00%', top: '0.00%' });
  });

  it('a pin click reports that annotation id', () => {
    const onSelect = vi.fn();
    render(<AnnotationPins target="t" annotations={[annotation()]} onSelect={onSelect} />);
    fireEvent.click(screen.getByTestId('design-pin-a1'));
    expect(onSelect).toHaveBeenCalledWith('a1');
  });

  it('placement mode converts a layer click to a normalized point', () => {
    const onPlace = vi.fn();
    const { container } = render(<AnnotationPins target="t" annotations={[]} placeMode onPlace={onPlace} />);
    const layer = container.querySelector('[data-testid="design-pins"]') as HTMLElement;
    // jsdom has no layout: stub the rect the click geometry reads.
    vi.spyOn(layer, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 200, 100));
    fireEvent.click(layer, { clientX: 50, clientY: 25 });
    expect(onPlace).toHaveBeenCalledWith({ x: 0.25, y: 0.25 });
    expect(layer).toHaveAttribute('data-placing', 'true');
  });

  it('outside placement mode a layer click places nothing', () => {
    const onPlace = vi.fn();
    const { container } = render(<AnnotationPins target="t" annotations={[]} onPlace={onPlace} />);
    fireEvent.click(container.querySelector('[data-testid="design-pins"]') as HTMLElement);
    expect(onPlace).not.toHaveBeenCalled();
  });

  it('a pin click during placement mode selects the pin, not a new point', () => {
    const onSelect = vi.fn();
    const onPlace = vi.fn();
    const { container } = render(
      <AnnotationPins target="t" annotations={[annotation()]} placeMode onSelect={onSelect} onPlace={onPlace} />,
    );
    fireEvent.click(screen.getByTestId('design-pin-a1'));
    expect(onSelect).toHaveBeenCalledWith('a1');
    expect(onPlace).not.toHaveBeenCalled();
  });

  it('placement mode exposes the a11y affordances', () => {
    const { container } = render(<AnnotationPins target="t" annotations={[]} placeMode onPlace={() => {}} />);
    const layer = container.querySelector('[data-testid="design-pins"]') as HTMLElement;
    expect(layer).toHaveAttribute('role', 'button');
    expect(layer).toHaveAttribute('aria-label', 'Click to place the annotation pin');
  });
});
