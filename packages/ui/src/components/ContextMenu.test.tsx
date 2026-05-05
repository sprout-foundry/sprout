// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import ContextMenu from './ContextMenu';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

// Capture originals so we can restore them in afterAll
const originalRAF = globalThis.requestAnimationFrame;
const originalCAF = globalThis.cancelAnimationFrame;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock requestAnimationFrame to run synchronously so ContextMenu's event
  // listeners are attached immediately after render (no async delay).
  (globalThis as any).requestAnimationFrame = (cb: FrameRequestCallback) => cb(0);
  (globalThis as any).cancelAnimationFrame = () => {};
});

afterAll(() => {
  globalThis.requestAnimationFrame = originalRAF;
  globalThis.cancelAnimationFrame = originalCAF;
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ContextMenu', () => {
  const onClose = jest.fn();

  it('returns null when isOpen is false', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: false,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Menu Item' }))
      );
    });

    // Portal renders to document.body, not container
    expect(document.querySelector('.context-menu')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('renders children inside a portal on document.body when isOpen is true', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('button', { children: 'Menu Item' }))
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.querySelector('button')).not.toBeNull();
    expect(menu?.querySelector('button')?.textContent).toBe('Menu Item');
  });

  it('applies x/y positioning via inline style', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 200,
          y: 300,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.getAttribute('style')).toContain('left: 200px');
    expect(menu?.getAttribute('style')).toContain('top: 300px');
  });

  it('applies default zIndex of 1400', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.getAttribute('style')).toContain('z-index: 1400');
  });

  it('applies custom zIndex', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
          zIndex: 2000,
        }, createElement('div', { children: 'Item' }))
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.getAttribute('style')).toContain('z-index: 2000');
  });

  it('applies custom className alongside context-menu class', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
          className: 'my-custom-menu',
        }, createElement('div', { children: 'Item' }))
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.classList.contains('my-custom-menu')).toBe(true);
  });

  it('closes on outside click', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not close when clicking inside the menu', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { 'data-testid': 'menu-item', children: 'Item' }))
      );
    });

    const menu = document.querySelector('[data-testid="menu-item"]');
    act(() => {
      menu?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes on Escape key press', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not close on other key presses', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes on window blur', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      window.dispatchEvent(new Event('blur'));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes on scroll', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      window.dispatchEvent(new Event('scroll', { bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not attach listeners when isOpen is false', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: false,
          x: 100,
          y: 100,
          onClose,
        }, createElement('div', { children: 'Item' }))
      );
    });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      window.dispatchEvent(new Event('blur'));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('handles isOpen toggle from true to false and back', () => {
    const renderMenu = (open: boolean) => {
      act(() => {
        root.render(
          // @ts-expect-error — createElement accepts children as rest args
          createElement(ContextMenu, {
            isOpen: open,
            x: 100,
            y: 100,
            onClose,
          }, createElement('div', { children: 'Item' }))
        );
      });
    };

    renderMenu(true);
    expect(document.querySelector('.context-menu')).not.toBeNull();

    renderMenu(false);
    expect(document.querySelector('.context-menu')).toBeNull();

    renderMenu(true);
    expect(document.querySelector('.context-menu')).not.toBeNull();
  });

  it('renders multiple children', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(ContextMenu, {
          isOpen: true,
          x: 100,
          y: 100,
          onClose,
        },
          createElement('button', { children: 'Option 1' }),
          createElement('div', { className: 'context-menu-divider' }),
          createElement('button', { children: 'Option 2' })
        )
      );
    });

    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu?.querySelectorAll('button')).toHaveLength(2);
    expect(menu?.querySelector('.context-menu-divider')).not.toBeNull();
  });
});
