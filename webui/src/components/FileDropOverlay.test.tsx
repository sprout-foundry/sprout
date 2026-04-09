// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
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

function renderInContainer(component: React.ReactElement): { container: HTMLDivElement; root: any } {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(component);
  });
  return { container, root };
}

// ---------------------------------------------------------------------------
// Tests: FileDropOverlay component
// ---------------------------------------------------------------------------

describe('FileDropOverlay', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    container?.remove();
  });

  // ── Visibility ───────────────────────────────────────────────────

  describe('visibility', () => {
    it('renders null when visible is false', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={false} />));
      expect(container.firstChild).toBeNull();
    });

    it('renders the overlay when visible is true', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      expect(container.firstChild).not.toBeNull();
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();
      expect(container.querySelector('.file-drop-overlay-text')?.textContent).toBe('Drop files to open');
    });

    it('removes overlay when visible changes from true to false', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();

      act(() => {
        root.render(<FileDropOverlay visible={false} />);
      });
      expect(container.firstChild).toBeNull();
    });

    it('shows overlay when visible changes from false to true', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={false} />));
      expect(container.querySelector('.file-drop-overlay')).toBeNull();

      act(() => {
        root.render(<FileDropOverlay visible={true} />);
      });
      expect(container.querySelector('.file-drop-overlay')).not.toBeNull();
    });
  });

  // ── Content ──────────────────────────────────────────────────────

  describe('content', () => {
    it('displays the drop icon', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const icon = container.querySelector('.file-drop-overlay-icon');
      expect(icon).not.toBeNull();
      expect(icon?.textContent).toBe('📄');
    });

    it('displays the "Drop files to open" text', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const text = container.querySelector('.file-drop-overlay-text');
      expect(text).not.toBeNull();
      expect(text?.textContent).toBe('Drop files to open');
    });
  });

  // ── Accessibility ────────────────────────────────────────────────

  describe('accessibility', () => {
    it('has role="status" for screen readers', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const overlay = container.querySelector('[role="status"]');
      expect(overlay).not.toBeNull();
    });

    it('has aria-live="polite" for live region announcements', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const overlay = container.querySelector('[role="status"]');
      expectAttribute(overlay, 'aria-live', 'polite');
    });

    it('has descriptive aria-label', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const overlay = container.querySelector('[role="status"]');
      expectAttribute(overlay, 'aria-label', 'File drop zone active');
    });
  });

  // ── DOM structure ────────────────────────────────────────────────

  describe('DOM structure', () => {
    it('has the correct CSS class for the overlay container', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const overlay = container.querySelector('.file-drop-overlay');
      expect(overlay).not.toBeNull();
      expect(overlay?.className).toContain('file-drop-overlay');
    });

    it('has the content container with correct class', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const content = container.querySelector('.file-drop-overlay-content');
      expect(content).not.toBeNull();
    });

    it('has the icon container with correct class', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const icon = container.querySelector('.file-drop-overlay-icon');
      expect(icon).not.toBeNull();
      expect(icon?.textContent).toBe('📄');
    });

    it('has the text container with correct class', () => {
      ({ container, root } = renderInContainer(<FileDropOverlay visible={true} />));
      const text = container.querySelector('.file-drop-overlay-text');
      expect(text).not.toBeNull();
      expect(text?.textContent).toBe('Drop files to open');
    });
  });
});
