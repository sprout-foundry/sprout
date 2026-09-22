/**
 * ModeSwitcher — the top-left brand-mark dropdown.
 *
 * What this file pins: the menu portals to document.body (the sidebar clips
 * via overflow: hidden, so an in-place menu is severed when the rail is
 * collapsed), the trigger's rect anchors it, and the disclosure contract —
 * open/close, Escape, click-outside (which must treat the portaled menu as
 * "inside"), and selection.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import ModeSwitcher from './ModeSwitcher';

const codeMode = {
  id: 'code',
  label: 'Code',
  icon: () => null,
  hint: 'Chat, editor, git, terminal',
  available: () => true,
  Shell: () => null,
};
const designMode = {
  id: 'design',
  label: 'Design',
  icon: () => null,
  hint: 'Flows, screens, tokens',
  available: () => true,
  Shell: () => null,
};

let container: HTMLDivElement;
let root: Root;
let bodyMenu: HTMLDivElement | null;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  bodyMenu = null;
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  bodyMenu?.remove();
  bodyMenu = null;
});

function renderSwitcher(extraProps = {}) {
  act(() => {
    root.render(
      createElement(ModeSwitcher, {
        modes: [codeMode, designMode],
        activeId: 'code',
        onSelect: vi.fn(),
        trigger: ({ open }) => createElement('span', { className: open ? 'brand is-open' : 'brand' }),
        triggerLabel: 'Switch mode',
        ...extraProps,
      }),
    );
  });
}

function trigger() {
  return container.querySelector('[data-testid="sidebar-brand-trigger"]') as HTMLButtonElement;
}

function menu() {
  return (document.querySelector('[data-testid="sidebar-brand-menu"]') as HTMLDivElement) ?? null;
}

describe('ModeSwitcher', () => {
  it('renders a closed trigger with no menu', () => {
    renderSwitcher();
    expect(menu()).toBeNull();
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
  });

  it('opens the menu in document.body, outside the trigger tree', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const open = menu();
    expect(open).not.toBeNull();
    // The whole point of the portal: the menu must NOT be inside the
    // component's own subtree, or the clipping sidebar would sever it.
    expect(open!.getRootNode()).toBe(document);
    expect(container.contains(open!)).toBe(false);
    expect(trigger().getAttribute('aria-expanded')).toBe('true');
  });

  it('anchors the menu from the trigger rect with inline viewport coords', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // jsdom reports zero rects: top = bottom + gap (6), left = 0.
    const style = menu()!.getAttribute('style') ?? '';
    expect(style).toContain('top:');
    expect(style).toContain('left:');
  });

  it('lists every mode and marks the active one', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(document.querySelector('[data-testid="sidebar-brand-option-code"]')).not.toBeNull();
    expect(document.querySelector('[data-testid="sidebar-brand-option-design"]')).not.toBeNull();
    expect(document.querySelector('[data-testid="sidebar-brand-option-code"]')!.getAttribute('aria-selected')).toBe(
      'true',
    );
  });

  it('fires onSelect and closes when a different mode is picked', () => {
    const onSelect = vi.fn();
    renderSwitcher({ onSelect });
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    act(() => {
      document
        .querySelector('[data-testid="sidebar-brand-option-design"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('design');
    expect(menu()).toBeNull();
  });

  it('picking the active mode closes without firing onSelect', () => {
    const onSelect = vi.fn();
    renderSwitcher({ onSelect });
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    act(() => {
      document
        .querySelector('[data-testid="sidebar-brand-option-code"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSelect).not.toHaveBeenCalled();
    expect(menu()).toBeNull();
  });

  it('closes on Escape', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(menu()).not.toBeNull();

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    expect(menu()).toBeNull();
  });

  it('closes on a click outside both the trigger and the portaled menu', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(menu()).not.toBeNull();

    const outside = document.createElement('div');
    document.body.appendChild(outside);
    act(() => {
      outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    outside.remove();

    expect(menu()).toBeNull();
  });

  it('does NOT close on a click inside the portaled menu', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const open = menu()!;
    act(() => {
      open.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(menu()).not.toBeNull();
  });

  it('toggles closed when the trigger is clicked again', () => {
    renderSwitcher();
    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(menu()).not.toBeNull();

    act(() => {
      trigger().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(menu()).toBeNull();
  });

  it('renders a static mark (no button, no menu) for a single-mode workspace', () => {
    renderSwitcher({ modes: [codeMode] });

    expect(trigger()).toBeNull();
    const staticMark = container.querySelector('[data-testid="sidebar-brand"] .mode-switcher-trigger--static');
    expect(staticMark).not.toBeNull();
  });
});
