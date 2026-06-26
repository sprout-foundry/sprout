// @ts-nocheck

import { act, forwardRef, useImperativeHandle } from 'react';
import { createRoot } from 'react-dom/client';
import Terminal from './Terminal';

// ---------------------------------------------------------------------------
// Mock TerminalPane — forwardRef component with imperative handle { clear, focus }
// ---------------------------------------------------------------------------

vi.mock('./TerminalPane', async () => {
  const { forwardRef, useImperativeHandle } = await vi.importActual('react');
  return {
    default: forwardRef(function MockTerminalPane({ isActive, isConnected }: any, ref: any) {
      // NOTE: vi.fn() inside useImperativeHandle creates fresh mock instances on
      // every re-render. This is acceptable because no test asserts on imperative
      // handle call counts — all assertions check DOM state. If future tests need
      // to verify clear()/focus() calls, the mock factory will need restructuring
      // to use stable references (e.g., storing mocks on a shared object that both
      // the factory and test scope can access).
      useImperativeHandle(ref, () => ({
        clear: vi.fn(),
        focus: vi.fn(),
      }));

      return (
        <div
          data-testid="terminal-pane"
          data-active={isActive ? 'true' : 'false'}
          data-connected={isConnected ? 'true' : 'false'}
        />
      );
    }),
  };
});

// ---------------------------------------------------------------------------
// Mock TerminalTabBar — simple presentational component
// ---------------------------------------------------------------------------

vi.mock('./TerminalTabBar', () => {
  return function MockTerminalTabBar(props: any) {
    return <div data-testid="terminal-tab-bar" data-active={props.activeSessionId} />;
  };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderTerminal(props: Record<string, any> = {}) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(<Terminal isConnected={true} isExpanded={false} {...props} />);
  });

  return { container, root };
}

function expandTerminal(container: HTMLElement) {
  const expandBtn = container.querySelector('[title="Expand terminal"]');
  if (expandBtn) {
    act(() => {
      expandBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
  }
}

function collapseTerminal(container: HTMLElement) {
  const collapseBtn = container.querySelector('[title="Collapse terminal"]');
  if (collapseBtn) {
    act(() => {
      collapseBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
  }
}

/**
 * Get all split buttons (vertical is first, horizontal is second).
 * Ordered by their position in the DOM: vertical split button (Columns2 icon)
 * is rendered first, horizontal split button (Rows2 icon) is rendered second.
 *
 * Always re-query — toggling split moves the actions cluster between panes,
 * so any captured element ref goes stale after a state change.
 */
function getSplitButtons(container: HTMLElement) {
  return Array.from(container.querySelectorAll('.split-btn'));
}

function getVerticalBtn(container: HTMLElement): HTMLElement {
  return getSplitButtons(container)[0] as HTMLElement;
}

function getHorizontalBtn(container: HTMLElement): HTMLElement {
  return getSplitButtons(container)[1] as HTMLElement;
}

function getPanesContainer(container: HTMLElement) {
  return container.querySelector('.terminal-panes-container');
}

function getSplitDivider(container: HTMLElement) {
  return container.querySelector('.terminal-split-divider');
}

function getSecondaryPane(container: HTMLElement) {
  // Secondary pane wrapper is the second .terminal-pane-wrapper
  const wrappers = container.querySelectorAll('.terminal-pane-wrapper');
  return wrappers.length > 1 ? wrappers[1] : null;
}

function dispatchTerminalAction(action: string) {
  const event = new CustomEvent('sprout:terminal-action', {
    detail: { action },
  });
  window.dispatchEvent(event);
}

const _flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

describe('Terminal split functionality', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    // Reset custom CSS variable if set by previous test
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root.unmount();
      });
    }
    if (container) {
      container.remove();
    }
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  // ── 1. Initial state: No split active ────────────────────

  it('does not have split active by default (collapsed state)', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);
    // Both buttons should exist but neither should be active
    splitBtns.forEach((btn) => {
      expect(btn.classList.contains('split-btn-active')).toBe(false);
    });
  });

  it('does not have split active by default (expanded state)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const panesContainer = getPanesContainer(container);
    expect(panesContainer).toBeTruthy();
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);

    expect(getSplitDivider(container)).toBeNull();
    expect(getSecondaryPane(container)).toBeNull();
  });

  // ── 2. Vertical split via button click ───────────────────

  it('activates vertical split when vertical split button is clicked', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // The vertical split button initially has aria-label="Split terminal vertically"
    expect(getVerticalBtn(container)).toBeTruthy();

    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // After clicking, the button should now have split-btn-active class
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(true);
    expect(getVerticalBtn(container).getAttribute('aria-label')).toBe('Unsplit terminal');
    expect(getVerticalBtn(container).getAttribute('aria-pressed')).toBe('true');
  });

  it('shows vertical split layout in panes container after vertical split', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Click vertical split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  // ── 3. Horizontal split via button click ─────────────────

  it('activates horizontal split when horizontal split button is clicked', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    expect(getHorizontalBtn(container)).toBeTruthy();

    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getHorizontalBtn(container).classList.contains('split-btn-active')).toBe(true);
    expect(getHorizontalBtn(container).getAttribute('aria-label')).toBe('Unsplit terminal');
    expect(getHorizontalBtn(container).getAttribute('aria-pressed')).toBe('true');

    // Vertical button should NOT be active
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(false);
  });

  it('shows horizontal split layout in panes container after horizontal split', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  // ── 4. Vertical split via custom event ───────────────────

  it('activates vertical split via sprout:terminal-action custom event', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // The first split-btn is vertical, second is horizontal
    const splitBtns = getSplitButtons(container);
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[0].getAttribute('aria-pressed')).toBe('true');
  });

  it('shows vertical split layout after custom event', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
  });

  // ── 5. Horizontal split via custom event ─────────────────

  it('activates horizontal split via sprout:terminal-action custom event', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const splitBtns = getSplitButtons(container);
    expect(splitBtns[1].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[1].getAttribute('aria-pressed')).toBe('true');
  });

  it('shows horizontal split layout after custom event', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
  });

  // ── 6. Toggle off: clicking same split button deactivates ─

  it('deactivates vertical split when clicking the same button again', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // First click: activate
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(true);

    // Second click: aria-label is now "Unsplit terminal".
    // The button moves between panes on toggle (actions cluster anchors to
    // the rightmost/top-most pane), so re-query each time.
    act(() => {
      (container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement).dispatchEvent(
        new MouseEvent('click', { bubbles: true }),
      );
    });

    // Button should no longer be active. After unsplit, the vertical-split
    // button lives on pane[0]'s tab bar with its original label.
    const vBtn = getVerticalBtn(container);
    expect(vBtn.classList.contains('split-btn-active')).toBe(false);
    expect(vBtn.getAttribute('aria-pressed')).toBe('false');
    expect(vBtn.getAttribute('aria-label')).toBe('Split terminal vertically');
  });

  it('deactivates horizontal split when clicking the same button again', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // Activate
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getHorizontalBtn(container).classList.contains('split-btn-active')).toBe(true);

    // Deactivate — the active button's aria-label is "Unsplit terminal".
    // Find by class, re-query each step (the actions cluster moves between
    // panes on toggle).
    act(() => {
      const splitBtns = getSplitButtons(container);
      const activeBtn = splitBtns.find((btn) => btn.classList.contains('split-btn-active')) as HTMLElement;
      activeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // After unsplit, no split button should carry the active class.
    expect(getSplitButtons(container).some((btn) => btn.classList.contains('split-btn-active'))).toBe(false);
  });

  it('removes split layout when toggling off (expanded)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate vertical split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Deactivate — re-query the unsplit button each step (it moves between
    // panes when the actions cluster's host pane changes).
    act(() => {
      (container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement).dispatchEvent(
        new MouseEvent('click', { bubbles: true }),
      );
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  // ── 7. Switch between splits ─────────────────────────────

  it('switching from horizontal to vertical replaces (not stacks) the split', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // Activate horizontal
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getHorizontalBtn(container).classList.contains('split-btn-active')).toBe(true);
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(false);

    // Now activate vertical — should replace horizontal
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(true);
    expect(getHorizontalBtn(container).classList.contains('split-btn-active')).toBe(false);
  });

  it('switching from vertical to horizontal replaces (not stacks) the split', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // Activate vertical
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(true);

    // Now activate horizontal
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getHorizontalBtn(container).classList.contains('split-btn-active')).toBe(true);
    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(false);
  });

  it('switching split direction updates CSS classes correctly (expanded)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate vertical
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    let panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);

    // Switch to horizontal
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  // ── 8. Split creates secondary session ───────────────────

  it('vertical split creates a secondary session', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Before split: one pane wrapper (primary)
    const wrappersBefore = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersBefore.length).toBe(1);

    // Activate vertical split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // After split: two pane wrappers
    const wrappersAfter = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersAfter.length).toBe(2);
  });

  it('horizontal split creates a secondary session', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const wrappersBefore = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersBefore.length).toBe(1);

    // Activate horizontal split
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const wrappersAfter = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersAfter.length).toBe(2);
  });

  it('switching split directions reuses the same secondary pane (no third pane)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);

    // Activate vertical
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Switch to horizontal — should still be 2 panes, not 3
    act(() => {
      splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  // ── 9. Unsplit hides secondary pane and divider ──────────

  it('unsplitting hides the secondary pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Deactivate split
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Back to 1 pane
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    expect(getSecondaryPane(container)).toBeNull();
  });

  it('unsplitting hides the divider', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getSplitDivider(container)).toBeTruthy();

    // Deactivate
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getSplitDivider(container)).toBeNull();
  });

  // ── 10. Split CSS classes ────────────────────────────────

  it('vertical split adds terminal-split-vertical class to panes container', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
  });

  it('horizontal split adds terminal-split-horizontal class to panes container', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
  });

  it('no split class present when split is deactivated', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate then deactivate
    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const splitBtns = getSplitButtons(container);
    const activeBtn = splitBtns.find((btn) => btn.classList.contains('split-btn-active')) as HTMLElement;
    act(() => {
      activeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  it('divider has terminal-split-divider-vertical class during vertical split', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const divider = getSplitDivider(container);
    expect(divider).toBeTruthy();
    expect(divider?.classList.contains('terminal-split-divider-vertical')).toBe(true);
    expect(divider?.classList.contains('terminal-split-divider-horizontal')).toBe(false);
  });

  it('divider has terminal-split-divider-horizontal class during horizontal split', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const divider = getSplitDivider(container);
    expect(divider).toBeTruthy();
    expect(divider?.classList.contains('terminal-split-divider-horizontal')).toBe(true);
    expect(divider?.classList.contains('terminal-split-divider-vertical')).toBe(false);
  });

  // ── 11. Divider presence ─────────────────────────────────

  it('split divider is NOT present when no split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    expect(getSplitDivider(container)).toBeNull();
  });

  it('split divider IS present when vertical split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    expect(getSplitDivider(container)).toBeTruthy();
  });

  it('split divider IS present when horizontal split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    expect(getSplitDivider(container)).toBeTruthy();
  });

  // ── 12. Secondary pane close button ──────────────────────
  // NOTE: Secondary panes no longer have close buttons — splitting
  // is toggled via the split button. These tests now verify that
  // toggling via the split button works correctly.

  it('splitting creates two panes and a divider', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
    expect(getSplitDivider(container)).toBeTruthy();
  });

  // ── 13. Unsplitting via split button toggle ──────────────

  it('toggling split off removes the secondary pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Verify split is active
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Toggle off via the same split button
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    expect(unsplitBtn).toBeTruthy();

    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Split should be removed
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
  });

  it('toggling split off removes the divider', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    expect(getSplitDivider(container)).toBeTruthy();

    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getSplitDivider(container)).toBeNull();
  });

  it('toggling split off removes split CSS classes', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);

    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  it('toggling split off deactivates split button state', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    expect(getVerticalBtn(container).classList.contains('split-btn-active')).toBe(true);

    act(() => {
      (container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement).dispatchEvent(
        new MouseEvent('click', { bubbles: true }),
      );
    });

    // Both buttons should be inactive — re-query, since the actions cluster
    // moves between panes on toggle.
    getSplitButtons(container).forEach((btn) => {
      expect(btn.classList.contains('split-btn-active')).toBe(false);
    });
  });

  // ── 14. Edge cases ───────────────────────────────────────

  it('custom event with unknown action does not affect split state', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('unknown_action');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
    expect(getSplitDivider(container)).toBeNull();
  });

  it('custom event without detail does not crash', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:terminal-action'));
    });

    // Should not crash; split state unchanged
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('custom event with null detail does not crash', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: null }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('split state persists after re-expanding the terminal', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate vertical split
    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Collapse the terminal
    collapseTerminal(container);

    // Panes container still exists even when collapsed (it's in terminal-body)
    // but terminal-body has display:none when collapsed

    // Re-expand
    expandTerminal(container);

    // Split should still be active
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('split state persists through clear button click', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate vertical split
    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Click the clear button (Trash2 icon)
    const clearBtn = container.querySelector('.clear-btn') as HTMLElement;
    expect(clearBtn).toBeTruthy();

    act(() => {
      clearBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Split should still be active
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  // ── 15. Split button accessibility ───────────────────────

  it('split buttons have aria-pressed attribute', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);
    expect(splitBtns.length).toBe(2);
    splitBtns.forEach((btn) => {
      expect(btn.hasAttribute('aria-pressed')).toBe(true);
      expect(btn.getAttribute('aria-pressed')).toBe('false');
    });
  });

  it('vertical split button aria-pressed becomes true when active', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getVerticalBtn(container).getAttribute('aria-pressed')).toBe('true');
  });

  it('horizontal split button aria-pressed becomes true when active', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getHorizontalBtn(container).getAttribute('aria-pressed')).toBe('true');
  });

  it('split buttons have aria-label attributes', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    expect(container.querySelector('[aria-label="Split terminal vertically"]')).toBeTruthy();
    expect(container.querySelector('[aria-label="Split terminal horizontally"]')).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Additional tests: split lifecycle, unsplit + re-split, close-session edge cases
// ---------------------------------------------------------------------------

describe('Terminal split lifecycle and edge cases', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root.unmount();
      });
    }
    if (container) {
      container.remove();
    }
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  it('can re-split after unsplitting via split button toggle', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // First split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unsplit via button toggle
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Re-split should create a new secondary session (not reuse old)
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('can re-split after unsplitting via split button toggle', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // First split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unsplit via button toggle
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Re-split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('splitting multiple times in a row does not create duplicate panes', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Re-query the vertical split button after every state change — the
    // actions cluster moves between panes when split toggles, so the DOM
    // node for the same logical button changes.

    // Split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Click again (same direction) should unsplit
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Click again should re-split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Never more than 2
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
  });

  it('terminal remains functional (1 pane) after unsplitting', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Split vertically
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unsplit via button toggle
    const unsplitBtn = container.querySelector('[aria-label="Unsplit terminal"]') as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Should have exactly 1 pane wrapper, no divider, no secondary
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    expect(getSplitDivider(container)).toBeNull();
    expect(getSecondaryPane(container)).toBeNull();

    // Both split buttons should be inactive
    const splitBtns = getSplitButtons(container);
    splitBtns.forEach((btn) => {
      expect(btn.classList.contains('split-btn-active')).toBe(false);
    });

    // Pans container should have no split classes
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);

    // Should be able to split again afterward
    act(() => {
      dispatchTerminalAction('split_horizontal');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('no crash when splitting and unsplitting rapidly', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);

    // Rapid toggle cycles
    for (let i = 0; i < 10; i++) {
      act(() => {
        splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
      act(() => {
        splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    }

    // Component should still be in a consistent state
    // At this point horizontal was clicked last, so it should be active
    const panesContainer = getPanesContainer(container);
    expect(panesContainer).toBeTruthy();

    // We should have either 1 or 2 panes (never 0 or 3)
    const paneCount = container.querySelectorAll('.terminal-pane-wrapper').length;
    expect([1, 2]).toContain(paneCount);
  });

  it('unmounting while split is active cleans up properly', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate split
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unmount should not throw
    act(() => {
      root.unmount();
    });
    root = null;

    // Body styles should be cleaned up (not set by split toggle, but verify no crash)
    expect(document.body.style.userSelect).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Tests: terminal height persistence to localStorage
// ---------------------------------------------------------------------------

describe('Terminal height persistence', () => {
  let container: HTMLDivElement;
  let root: any;

  const STORAGE_KEY = 'sprout-terminal-height';

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    localStorage.removeItem(STORAGE_KEY);
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root.unmount();
      });
    }
    if (container) {
      container.remove();
    }
    localStorage.removeItem(STORAGE_KEY);
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  it('initializes with default height (400px) when localStorage is empty', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('400px');

    // localStorage should NOT have been written just from mounting
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('reads persisted height from localStorage on mount', () => {
    localStorage.setItem(STORAGE_KEY, '250');

    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('250px');
  });

  it('clamps persisted value below minimum to minimum (120px)', () => {
    localStorage.setItem(STORAGE_KEY, '50');

    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('120px');
  });

  it('clamps persisted invalid value to default (400px)', () => {
    localStorage.setItem(STORAGE_KEY, 'not-a-number');

    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('400px');
  });

  it('persists height to localStorage after resize drag completes', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Simulate a resize drag via the resize handle
    const resizeHandle = container.querySelector('.terminal-resize-handle') as HTMLElement;
    expect(resizeHandle).toBeTruthy();

    // We can't easily simulate full drag in jsdom withoutMouseMove on document,
    // but we can verify the component renders at the initial height and the
    // localStorage key does not exist until a drag completes.
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();

    // Verify initial height
    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('400px');

    // Simulate mousedown on resize handle and immediately mouseup
    // This simulates a "resize" that doesn't move, so height stays at 400
    act(() => {
      resizeHandle.dispatchEvent(
        new MouseEvent('mousedown', {
          bubbles: true,
          clientX: 0,
          clientY: 600,
        }),
      );
    });

    // The resize sets isResizingVertical, now trigger mouseup
    act(() => {
      document.dispatchEvent(
        new MouseEvent('mouseup', {
          clientX: 0,
          clientY: 600,
        }),
      );
    });

    // After drag completes, height should be persisted
    expect(localStorage.getItem(STORAGE_KEY)).toBe('400');
  });

  it('sets correct CSS variable when expanded with persisted height', () => {
    localStorage.setItem(STORAGE_KEY, '300');

    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const reservedHeight = document.documentElement.style.getPropertyValue('--sprout-terminal-reserved-height');
    expect(reservedHeight).toBe('300px');
  });

  it('falls back to collapsed height CSS variable when not expanded', () => {
    localStorage.setItem(STORAGE_KEY, '500');

    const view = renderTerminal({ isExpanded: false });
    container = view.container;
    root = view.root;

    const reservedHeight = document.documentElement.style.getPropertyValue('--sprout-terminal-reserved-height');
    // When collapsed, should use collapsedHeight (42px on non-mobile), not the persisted height
    expect(reservedHeight).toBe('42px');
  });
});

// ---------------------------------------------------------------------------
// Tests: flat N-pane splits + generalized close behavior
// ---------------------------------------------------------------------------

function getAddPaneBtn(container: HTMLElement) {
  return container.querySelector('.add-pane-btn') as HTMLButtonElement | null;
}

function getDividers(container: HTMLElement) {
  return container.querySelectorAll('.terminal-split-divider');
}

describe('Terminal flat N-pane splits', () => {
  let container: HTMLDivElement;
  let root: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root.unmount();
      });
    }
    if (container) {
      container.remove();
    }
    document.documentElement.style.removeProperty('--sprout-terminal-reserved-height');
  });

  it('hides the +pane button when split is not active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    expect(getAddPaneBtn(container)).toBeNull();
  });

  it('shows the +pane button when split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const addBtn = getAddPaneBtn(container);
    expect(addBtn).toBeTruthy();
    expect(addBtn?.disabled).toBe(false);
  });

  it('clicking +pane grows the split to 3 panes with 2 dividers', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
    expect(getDividers(container).length).toBe(1);

    const addBtn = getAddPaneBtn(container) as HTMLButtonElement;
    act(() => {
      addBtn.click();
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);
    // N panes → N-1 dividers.
    expect(getDividers(container).length).toBe(2);
  });

  it('+pane can grow up to the hard cap (8 panes)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // From 2 panes, click +pane until disabled or until we hit 8.
    for (let i = 0; i < 10; i++) {
      const addBtn = getAddPaneBtn(container) as HTMLButtonElement;
      if (!addBtn || addBtn.disabled) break;
      act(() => {
        addBtn.click();
      });
    }

    const paneCount = container.querySelectorAll('.terminal-pane-wrapper').length;
    expect(paneCount).toBe(8);
    expect(getDividers(container).length).toBe(7);

    // At capacity, the +pane button is disabled.
    const finalAddBtn = getAddPaneBtn(container) as HTMLButtonElement;
    expect(finalAddBtn.disabled).toBe(true);
  });

  it('clicking matching split button at 3+ panes is a no-op (avoids destructive collapse)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Grow to 3 panes.
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    const addBtn = getAddPaneBtn(container) as HTMLButtonElement;
    act(() => {
      addBtn.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);

    // Clicking the matching split button stays at 3 panes — without this
    // guard the legacy 1↔2 toggle would silently destroy two terminals.
    const splitBtns = getSplitButtons(container);
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);
  });

  it('switching direction at 3 panes keeps all 3 and flips the axis', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    const addBtn = getAddPaneBtn(container) as HTMLButtonElement;
    act(() => {
      addBtn.click();
    });

    // Now flip to horizontal.
    const splitBtns = getSplitButtons(container);
    act(() => {
      splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('closing the last session in pane 2 of 3 removes that pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    const addBtn = getAddPaneBtn(container) as HTMLButtonElement;
    act(() => {
      addBtn.click();
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);

    // The mock TerminalPane exposes a close button via secondary-close-btn,
    // but Terminal.tsx renders showCloseButton={false} on the real pane.
    // Drive the close through the `kill` terminal action, which closes the
    // active session in the focused pane (which is the newly-added pane 2).
    act(() => {
      dispatchTerminalAction('kill');
    });

    // Down to 2 panes, still split.
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
  });

  it('killing the last remaining split pane drops us back to 1 pane unsplit', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Focused pane is the secondary; killing it should collapse the split.
    act(() => {
      dispatchTerminalAction('kill');
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Tests: nextActiveAfterClose helper
// ---------------------------------------------------------------------------

import { nextActiveAfterClose } from './Terminal';

describe('nextActiveAfterClose', () => {
  // No pinning — display order matches insertion order.
  it('picks the right-neighbour when closing a middle tab', () => {
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: false },
      { id: 'c', name: 'C', is_pinned: false },
    ];
    expect(nextActiveAfterClose(sessions, 'b')).toBe('c');
  });

  it('picks the previous-neighbour when closing the last tab', () => {
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: false },
      { id: 'c', name: 'C', is_pinned: false },
    ];
    expect(nextActiveAfterClose(sessions, 'c')).toBe('b');
  });

  it('picks the next display-tab when closing the first tab', () => {
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: false },
      { id: 'c', name: 'C', is_pinned: false },
    ];
    expect(nextActiveAfterClose(sessions, 'a')).toBe('b');
  });

  // Pinning — display order differs from array order.
  it('honors display order when a pinned tab sits before an unpinned one', () => {
    // Array order: [A, B(pinned), C]; display order: [B, A, C].
    // Closing A (display position 1) should focus C (display position 2),
    // NOT B (the pinned tab to the left at display position 0).
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: true },
      { id: 'c', name: 'C', is_pinned: false },
    ];
    expect(nextActiveAfterClose(sessions, 'a')).toBe('c');
  });

  it('falls back to the previous tab when closing the rightmost display tab', () => {
    // Display order: [B(pinned), A, C]. Closing C → focus A.
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: true },
      { id: 'c', name: 'C', is_pinned: false },
    ];
    expect(nextActiveAfterClose(sessions, 'c')).toBe('a');
  });

  it('keeps pinned-first when closing a pinned tab', () => {
    // Display order: [B(pinned), D(pinned), A, C]. Closing B → focus D.
    const sessions = [
      { id: 'a', name: 'A', is_pinned: false },
      { id: 'b', name: 'B', is_pinned: true },
      { id: 'c', name: 'C', is_pinned: false },
      { id: 'd', name: 'D', is_pinned: true },
    ];
    expect(nextActiveAfterClose(sessions, 'b')).toBe('d');
  });

  it('returns the closed id back when no session remains', () => {
    const sessions = [{ id: 'a', name: 'A', is_pinned: false }];
    expect(nextActiveAfterClose(sessions, 'a')).toBe('a');
  });
});
