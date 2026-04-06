// @ts-nocheck

import React from 'react';
import { render } from '@testing-library/react';
import FileDropOverlay from './FileDropOverlay';

// Helper — replicate toHaveAttribute since jest-dom matchers aren't auto-loaded
function expectAttribute(element: Element | null, attr: string, value?: string): void {
  expect(element).not.toBeNull();
  const actual = (element as Element).getAttribute(attr);
  if (value !== undefined) {
    expect(actual).toBe(value);
  } else {
    expect(actual).not.toBeNull();
  }
}

// ---------------------------------------------------------------------------
// Tests: FileDropOverlay component
// ---------------------------------------------------------------------------

describe('FileDropOverlay', () => {
  // ── Visibility ───────────────────────────────────────────────────

  describe('visibility', () => {
    it('renders null when visible is false', () => {
      const { container } = render(<FileDropOverlay visible={false} />);
      expect(container.firstChild).toBeNull();
    });

    it('renders the overlay when visible is true', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      expect(container.firstChild).not.toBeNull();
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();
      expect(container.querySelector('.file-drop-overlay-text')?.textContent).toBe('Drop files to open');
    });

    it('removes overlay when visible changes from true to false', () => {
      const { container, rerender } = render(<FileDropOverlay visible={true} />);
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();

      rerender(<FileDropOverlay visible={false} />);
      expect(container.firstChild).toBeNull();
    });

    it('shows overlay when visible changes from false to true', () => {
      const { container, rerender } = render(<FileDropOverlay visible={false} />);
      expect(container.querySelector('.file-drop-overlay')).toBeNull();

      rerender(<FileDropOverlay visible={true} />);
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();
    });
  });

  // ── Content ──────────────────────────────────────────────────────

  describe('content', () => {
    it('displays the drop icon', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const icon = container.querySelector('.file-drop-overlay-icon');
      expect(icon).not.toBeNull();
      expect(icon?.textContent).toBe('📄');
    });

    it('displays the "Drop files to open" text', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const text = container.querySelector('.file-drop-overlay-text');
      expect(text).not.toBeNull();
      expect(text?.textContent).toBe('Drop files to open');
    });
  });

  // ── Accessibility ────────────────────────────────────────────────

  describe('accessibility', () => {
    it('has role="status" for screen readers', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const overlay = container.querySelector('[role="status"]');
      expect(overlay).not.toBeNull();
    });

    it('has aria-live="polite" for live region announcements', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const overlay = container.querySelector('[role="status"]');
      expectAttribute(overlay, 'aria-live', 'polite');
    });

    it('has descriptive aria-label', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const overlay = container.querySelector('[role="status"]');
      expectAttribute(overlay, 'aria-label', 'File drop zone active');
    });
  });

  // ── DOM structure ────────────────────────────────────────────────

  describe('DOM structure', () => {
    it('has the correct CSS class for the overlay container', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const overlay = container.querySelector('.file-drop-overlay');
      expect(overlay).not.toBeNull();
      expect(overlay?.className).toContain('file-drop-overlay');
    });

    it('has the content container with correct class', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const content = container.querySelector('.file-drop-overlay-content');
      expect(content).not.toBeNull();
    });

    it('has the icon container with correct class', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const icon = container.querySelector('.file-drop-overlay-icon');
      expect(icon).not.toBeNull();
      expect(icon?.textContent).toBe('📄');
    });

    it('has the text container with correct class', () => {
      const { container } = render(<FileDropOverlay visible={true} />);
      const text = container.querySelector('.file-drop-overlay-text');
      expect(text).not.toBeNull();
      expect(text?.textContent).toBe('Drop files to open');
    });
  });
});
