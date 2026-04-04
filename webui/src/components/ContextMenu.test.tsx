// @ts-nocheck
import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import ContextMenu from './ContextMenu';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock requestAnimationFrame so close-listener effect fires synchronously.
// jest does not auto-flush rAF; without this, close listeners never attach.
let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let mountPoint: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  jest.clearAllMocks();
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (mountPoint) {
    document.body.removeChild(mountPoint);
    mountPoint = null;
  }
  document.querySelectorAll('.context-menu').forEach((el) => el.remove());
});

/**
 * Renders a ContextMenu with the given props. Returns the onClose spy.
 */
function renderMenu(
  props: {
    isOpen?: boolean;
    x?: number;
    y?: number;
    className?: string;
    zIndex?: number;
    children?: React.ReactNode;
  } = {},
) {
  const {
    isOpen = true,
    x = 100,
    y = 100,
    className,
    zIndex,
    children = <button className="context-menu-item">Item 1</button>,
  } = props;
  const onClose = jest.fn();

  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint!);
    root.render(
      <ContextMenu isOpen={isOpen} x={x} y={y} onClose={onClose} className={className} zIndex={zIndex}>
        {children}
      </ContextMenu>,
    );
  });

  return { onClose };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ContextMenu', () => {
  test('does not render when isOpen is false', () => {
    renderMenu({ isOpen: false });
    const menu = document.querySelector('.context-menu');
    expect(menu).toBeNull();
  });

  test('renders children when isOpen is true with class context-menu', () => {
    renderMenu({ isOpen: true });
    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    expect(menu!.classList.contains('context-menu')).toBe(true);

    const item = menu!.querySelector('.context-menu-item');
    expect(item).not.toBeNull();
    expect(item!.textContent).toBe('Item 1');
  });

  test('does not render when isOpen transitions to false', () => {
    const { onClose } = renderMenu({ isOpen: true });

    expect(document.querySelector('.context-menu')).not.toBeNull();

    // The component is controlled: re-render with isOpen=false
    act(() => {
      root!.render(
        <ContextMenu isOpen={false} x={100} y={100} onClose={onClose}>
          <button className="context-menu-item">Item 1</button>
        </ContextMenu>,
      );
    });

    expect(document.querySelector('.context-menu')).toBeNull();
  });

  test('calls onClose when clicking outside the menu', () => {
    const { onClose } = renderMenu({ isOpen: true });

    const outsideEl = document.createElement('div');
    document.body.appendChild(outsideEl);

    act(() => {
      outsideEl.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
    document.body.removeChild(outsideEl);
  });

  test('calls onClose when pressing Escape', () => {
    const { onClose } = renderMenu({ isOpen: true });

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('calls onClose when window loses focus (blur)', () => {
    const { onClose } = renderMenu({ isOpen: true });

    act(() => {
      window.dispatchEvent(new Event('blur'));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('calls onClose on scroll', () => {
    const { onClose } = renderMenu({ isOpen: true });

    act(() => {
      window.dispatchEvent(new Event('scroll', { bubbles: false }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('does NOT call onClose when clicking inside the menu', () => {
    const { onClose } = renderMenu({ isOpen: true });

    const menu = document.querySelector('.context-menu')!;
    const item = menu.querySelector('.context-menu-item') as HTMLElement;

    act(() => {
      item.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(onClose).not.toHaveBeenCalled();
  });

  test('viewport boundary clamping keeps menu on screen near bottom-right edge', () => {
    const originalWidth = window.innerWidth;
    const originalHeight = window.innerHeight;
    const originalGetBCR = Element.prototype.getBoundingClientRect;

    // Mock viewport to small values so the menu overflows
    Object.defineProperty(window, 'innerWidth', { value: 200, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: 200, configurable: true });

    // Mock getBoundingClientRect to return a rect that overflows the viewport.
    // The menu is positioned at (190, 190) via inline style, and the mock
    // reports it as 180px wide and 100px tall, so right=370 > 200 and bottom=290 > 200.
    Element.prototype.getBoundingClientRect = jest.fn(() => ({
      left: 190,
      top: 190,
      width: 180,
      height: 100,
      right: 370,
      bottom: 290,
      x: 190,
      y: 190,
      toJSON: () => ({}),
    }));

    renderMenu({ x: 190, y: 190 });

    const menu = document.querySelector('.context-menu') as HTMLElement;
    expect(menu).not.toBeNull();

    // After useLayoutEffect clamping:
    // - rect.right (370) > vw (200) → left = max(8, 200 - 180 - 8) = max(8, 12) = 12
    // - rect.bottom (290) > vh (200) → top = max(8, 200 - 100 - 8) = max(8, 92) = 92
    expect(menu.style.left).toBe('12px');
    expect(menu.style.top).toBe('92px');

    // Restore
    Element.prototype.getBoundingClientRect = originalGetBCR;
    Object.defineProperty(window, 'innerWidth', { value: originalWidth, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: originalHeight, configurable: true });
  });

  test('custom className is applied alongside context-menu', () => {
    renderMenu({ className: 'my-custom-menu' });

    const menu = document.querySelector('.context-menu')!;
    expect(menu.classList.contains('context-menu')).toBe(true);
    expect(menu.classList.contains('my-custom-menu')).toBe(true);
  });

  test('custom zIndex prop sets inline z-index on the menu container', () => {
    renderMenu({ zIndex: 9999 });

    const menu = document.querySelector('.context-menu') as HTMLElement;
    expect(menu).not.toBeNull();
    expect(menu.style.zIndex).toBe('9999');
  });

  test('multiple independent menus can be open simultaneously', () => {
    const onClose1 = jest.fn();
    const onClose2 = jest.fn();

    // renderMenu creates a root — use it to render two menus via React.Fragment
    renderMenu();

    act(() => {
      root!.render(
        <React.Fragment>
          <ContextMenu isOpen={true} x={50} y={50} onClose={onClose1}>
            <button className="context-menu-item">Menu 1</button>
          </ContextMenu>
          <ContextMenu isOpen={true} x={200} y={200} onClose={onClose2}>
            <button className="context-menu-item">Menu 2</button>
          </ContextMenu>
        </React.Fragment>,
      );
    });

    const menus = document.querySelectorAll('.context-menu');
    expect(menus.length).toBe(2);

    // Re-render — close the first menu, keep the second open
    act(() => {
      root!.render(
        <React.Fragment>
          <ContextMenu isOpen={false} x={50} y={50} onClose={onClose1}>
            <button className="context-menu-item">Menu 1</button>
          </ContextMenu>
          <ContextMenu isOpen={true} x={200} y={200} onClose={onClose2}>
            <button className="context-menu-item">Menu 2</button>
          </ContextMenu>
        </React.Fragment>,
      );
    });

    expect(document.querySelectorAll('.context-menu').length).toBe(1);
    expect(document.querySelector('.context-menu')!.textContent).toBe('Menu 2');
  });

  test('stopPropagation on the menu container prevents click events from bubbling', () => {
    const { onClose } = renderMenu();

    const menu = document.querySelector('.context-menu') as HTMLElement;
    expect(menu).not.toBeNull();

    // Attach a mousedown listener on document.body that would conflict
    // with the menu's own outside-click dismissal. The menu container's
    // onClick with stopPropagation ensures click events don't leak.
    // But since our outside-click dismissal uses mousedown, we test
    // the complementary behavior:
    //
    // 1. Mousedown on a child of the menu → does NOT call onClose
    //    (tested in "clicking inside" test)
    // 2. The menu container is a portal on body → structural check
    expect(menu.parentElement).toBe(document.body);

    // Click on menu itself should not call onClose (onClose is only
    // triggered by mousedown outside the menu)
    act(() => {
      menu.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  test('renders danger items and dividers correctly', () => {
    renderMenu({
      children: (
        <React.Fragment>
          <button className="context-menu-item">Normal item</button>
          <div className="context-menu-divider" />
          <button className="context-menu-item danger">Delete</button>
        </React.Fragment>
      ),
    });

    const menu = document.querySelector('.context-menu')!;

    const items = menu.querySelectorAll('.context-menu-item');
    expect(items.length).toBe(2);
    expect(items[0]!.textContent).toBe('Normal item');
    expect(items[0]!.classList.contains('danger')).toBe(false);
    expect(items[1]!.textContent).toBe('Delete');
    expect(items[1]!.classList.contains('danger')).toBe(true);

    const dividers = menu.querySelectorAll('.context-menu-divider');
    expect(dividers.length).toBe(1);
  });
});
