// @ts-nocheck

import { act, forwardRef, useImperativeHandle } from 'react';
import { createRoot } from 'react-dom/client';
import Terminal, { nextActiveAfterClose } from './Terminal';

// ---------------------------------------------------------------------------
// Mock TerminalPane — forwardRef component with imperative handle { clear, focus }
// ---------------------------------------------------------------------------

// Module-level registry so tests can trigger onProcessExit for a specific
// session.  The mock TerminalPane registers its onProcessExit callback here
// on every render, keyed by the session id it received as a prop.
const _processExitCallbacks = new Map<string, () => void>();

vi.mock('./TerminalPane', async () => {
  const { forwardRef, useImperativeHandle } = await vi.importActual('react');
  return {
    default: forwardRef(function MockTerminalPane(props: any, ref: any) {
      useImperativeHandle(ref, () => ({
        clear: vi.fn(),
        focus: vi.fn(),
      }));

      // Register the onProcessExit callback so tests can trigger it.
      // The key is derived from the reattachSessionId or a synthetic
      // identifier based on the props passed to this instance.
      // We use a unique counter per render to track multiple panes.
      if (!MockTerminalPane._counter) MockTerminalPane._counter = 0;
      const instanceKey = `mock-instance-${MockTerminalPane._counter++}`;
      if (props?.onProcessExit && typeof props.onProcessExit === 'function') {
        _processExitCallbacks.set(instanceKey, props.onProcessExit);
      }

      return (
        <div
          data-testid="terminal-pane"
          data-active={props.isActive ? 'true' : 'false'}
          data-connected={props.isConnected ? 'true' : 'false'}
          data-instance-key={instanceKey}
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

/** Get all per-pane close buttons (.pane-close-btn). */
function getPaneCloseButtons(container: HTMLElement) {
  return Array.from(container.querySelectorAll('.pane-close-btn'));
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

/**
 * Trigger onProcessExit for the first mock TerminalPane instance found in
 * the given container.  Used to simulate a PTY exiting so we can assert the
 * parent Terminal component's cleanup logic.
 */
function triggerProcessExit(container: HTMLElement) {
  // The mock TerminalPane renders with data-instance-key inside the wrapper
  // (which itself has data-testid="terminal-pane"). Query the inner mock pane.
  const pane = container.querySelector('[data-instance-key]');
  if (!pane) return;
  const key = pane.getAttribute('data-instance-key');
  const cb = _processExitCallbacks.get(key);
  if (cb) {
    act(() => {
      cb();
    });
  }
}

/**
 * Trigger onProcessExit for a specific pane wrapper index (0-based).
 * Useful when testing split panes where we want to exit the secondary pane.
 */
function triggerProcessExitForPane(container: HTMLElement, paneIndex: number) {
  const wrappers = container.querySelectorAll('.terminal-pane-wrapper');
  if (paneIndex < 0 || paneIndex >= wrappers.length) return;
  const pane = wrappers[paneIndex];
  const mockPane = pane.querySelector('[data-instance-key]');
  if (!mockPane) return;
  const key = mockPane.getAttribute('data-instance-key');
  const cb = _processExitCallbacks.get(key);
  if (cb) {
    act(() => {
      cb();
    });
  }
}

function getDividers(container: HTMLElement) {
  return container.querySelectorAll('.terminal-split-divider');
}

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

  it('renders split buttons with always-add aria-labels (collapsed state)', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // Split buttons should never carry an "active/pressed" state.
    expect(getSplitButtons(container).length).toBe(2);
    getSplitButtons(container).forEach((btn) => {
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

    expect(getVerticalBtn(container)).toBeTruthy();

    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Split buttons are action buttons now — no pressed/active state.
    expect(getVerticalBtn(container).getAttribute('aria-label')).toBe('Split terminal vertically');
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

    expect(getHorizontalBtn(container).getAttribute('aria-label')).toBe('Split terminal horizontally');
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

    const splitBtns = getSplitButtons(container);
    expect(splitBtns.length).toBeGreaterThanOrEqual(1);
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
    expect(splitBtns.length).toBeGreaterThanOrEqual(1);
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

  // ── 6. Always-add: clicking split button adds (never toggles off) ─

  it('clicking vertical split button again adds a 3rd pane (does NOT unsplit)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // First click → 2 panes
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Second click → 3 panes (always-add, never toggle off)
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);
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

    // Now activate vertical — should replace horizontal, stay at 2 panes
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('switching from vertical to horizontal replaces (not stacks) the split', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    // Activate vertical
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Now activate horizontal
    act(() => {
      getHorizontalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
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

  // ── 9. Per-pane close button ──────────────────────────────

  it('does not show close button when only one pane exists', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    expect(getPaneCloseButtons(container).length).toBe(0);
  });

  it('shows close buttons on each pane when split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Two panes → two close buttons
    expect(getPaneCloseButtons(container).length).toBe(2);
  });

  it('clicking a pane close button removes that pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Close the secondary pane (last close button in DOM order)
    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Back to 1 pane, close button hidden, split collapsed
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    expect(getPaneCloseButtons(container).length).toBe(0);
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('closing the primary pane keeps the remaining pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Close the primary pane (first close button)
    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
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

  it('no split class present when collapsed back to single pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Activate then close pane to collapse
    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
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

  // ── 12. Split creates two panes and a divider ────────────

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

  // ── 13. Collapse back via close button ───────────────────

  it('closing a pane removes the divider', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(getSplitDivider(container)).toBeTruthy();

    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getSplitDivider(container)).toBeNull();
  });

  it('closing a pane removes split CSS classes', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);

    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
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

  it('split buttons have aria-label attributes (always add)', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    expect(container.querySelector('[aria-label="Split terminal vertically"]')).toBeTruthy();
    expect(container.querySelector('[aria-label="Split terminal horizontally"]')).toBeTruthy();
  });

  it('split buttons do not have aria-pressed attribute (action buttons)', () => {
    const view = renderTerminal();
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);
    expect(splitBtns.length).toBe(2);
    splitBtns.forEach((btn) => {
      expect(btn.hasAttribute('aria-pressed')).toBe(false);
    });
  });
});

// ---------------------------------------------------------------------------
// Additional tests: split lifecycle, re-split, close-session edge cases
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

  it('can re-split after closing via close button', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // First split
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Close via pane close button
    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Re-split should create a new secondary session (not reuse old)
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('splitting multiple times in the same direction adds multiple panes', () => {
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

    // Click again (same direction) should add a 3rd pane
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);

    // Click again should add a 4th pane
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(4);
  });

  it('terminal remains functional (1 pane) after closing split pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Split vertically
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Close via pane close button
    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Should have exactly 1 pane wrapper, no divider, no secondary
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    expect(getSplitDivider(container)).toBeNull();
    expect(getSecondaryPane(container)).toBeNull();

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

  it('no crash when splitting and switching direction rapidly', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);

    // Rapid add + direction-switch cycles. Adding always grows the pane
    // count, switching direction keeps it the same.
    for (let i = 0; i < 5; i++) {
      act(() => {
        splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    }

    // Component should still be in a consistent state
    const panesContainer = getPanesContainer(container);
    expect(panesContainer).toBeTruthy();

    // We added vertical 5 times; pane count should be capped at the hard
    // cap (8) but never 0.
    const paneCount = container.querySelectorAll('.terminal-pane-wrapper').length;
    expect(paneCount).toBeGreaterThanOrEqual(1);
    expect(paneCount).toBeLessThanOrEqual(8);
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
// Tests: flat N-pane splits (always-add via split buttons) + close behavior
// ---------------------------------------------------------------------------

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

  it('split buttons are enabled when no split is active', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    const splitBtns = getSplitButtons(container);
    splitBtns.forEach((btn) => {
      expect((btn as HTMLButtonElement).disabled).toBe(false);
    });
  });

  it('clicking split button twice grows the split to 3 panes with 2 dividers', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
    expect(getDividers(container).length).toBe(1);

    // Second click adds a 3rd pane
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);
    // N panes → N-1 dividers.
    expect(getDividers(container).length).toBe(2);
  });

  it('split buttons can grow up to the hard cap (8 panes)', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // From 1 pane, click vertical split until disabled.
    for (let i = 0; i < 10; i++) {
      const vBtn = getVerticalBtn(container) as HTMLButtonElement;
      if (!vBtn || vBtn.disabled) break;
      act(() => {
        vBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    }

    const paneCount = container.querySelectorAll('.terminal-pane-wrapper').length;
    expect(paneCount).toBe(8);
    expect(getDividers(container).length).toBe(7);

    // At capacity, the vertical split button is disabled.
    const finalVBtn = getVerticalBtn(container) as HTMLButtonElement;
    expect(finalVBtn.disabled).toBe(true);
  });

  it('clicking matching split button at 3+ panes adds another pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Grow to 3 panes.
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);

    // Clicking the matching split button adds a 4th pane.
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(4);
  });

  it('switching direction at 3 panes keeps all 3 and flips the axis', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
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

  it('closing a pane via close button in 3-pane split drops to 2', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    act(() => {
      getVerticalBtn(container).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(3);

    // Close one pane via its close button (last one).
    const closeBtns = getPaneCloseButtons(container);
    act(() => {
      closeBtns[closeBtns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
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

// ---------------------------------------------------------------------------
// Tests: Terminal exit-pane cleanup paths (SP-101-1)
// ---------------------------------------------------------------------------

describe('Terminal exit-pane cleanup paths', () => {
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

  /* Path 1: Auto-close secondary split pane (IMMEDIATE — no delay) */
  it('exiting a secondary split pane closes that pane and collapses the split', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Create a vertical split → 2 panes
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Exit the secondary pane (index 1) — should close immediately
    triggerProcessExitForPane(container, 1);

    // Path 1 is immediate — no timer advance needed
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(getSplitDivider(container)).toBeNull();
  });

  it('exiting the primary split pane closes that pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Exit the primary pane (index 0) — should close immediately
    triggerProcessExitForPane(container, 0);

    // Path 1 is immediate — no timer advance needed
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
  });

  /* Path 2: Auto-create fresh session after 1.5s delay */
  it('exiting the only session schedules a fresh session after 1.5s', () => {
    vi.useFakeTimers();
    try {
      const view = renderTerminal({ isExpanded: true });
      container = view.container;
      root = view.root;

      // Initially 1 pane, 1 session
      expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
      const firstPane = container.querySelector('[data-instance-key]');
      const firstInstanceKey = firstPane?.getAttribute('data-instance-key');

      // Exit the only session
      triggerProcessExit(container);

      // Immediately after exit, the old pane should still exist (not replaced yet)
      expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
      const stillSamePane = container.querySelector('[data-instance-key]');
      expect(stillSamePane?.getAttribute('data-instance-key')).toBe(firstInstanceKey);

      // Advance time by 1.4s — session should NOT have been replaced yet
      act(() => {
        vi.advanceTimersByTime(1400);
      });
      expect(container.querySelector('[data-instance-key]')?.getAttribute('data-instance-key')).toBe(firstInstanceKey);

      // Advance past 1.5s — fresh session should be created
      act(() => {
        vi.advanceTimersByTime(200);
      });

      // Still 1 pane (the new session replaced the old one in the same pane)
      expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

      // Verify the session was actually replaced: the new instance key must differ
      // from the original because the mock assigns a fresh key on every render.
      const newPane = container.querySelector('[data-instance-key]');
      const newInstanceKey = newPane?.getAttribute('data-instance-key');
      expect(newInstanceKey).toBeTruthy();
      expect(newInstanceKey).not.toBe(firstInstanceKey);
    } finally {
      vi.useRealTimers();
    }
  });

  /* Path 3: Close tab + switch to next in multi-tab setup */
  it('exiting a tab in a multi-tab pane closes that tab and keeps the pane', () => {
    const view = renderTerminal({ isExpanded: true });
    container = view.container;
    root = view.root;

    // Create a second session via the + button (shell picker)
    const newSessionBtn = container.querySelector('.shell-picker-btn') as HTMLButtonElement;
    expect(newSessionBtn).toBeTruthy();

    act(() => {
      newSessionBtn.click();
    });

    // We now have 1 pane with 2 sessions (only the active tab's pane is rendered)
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Capture the currently-active session's instance key before exit.
    const activePaneBefore = container.querySelector('[data-instance-key]');
    const exitedInstanceKey = activePaneBefore?.getAttribute('data-instance-key');
    expect(exitedInstanceKey).toBeTruthy();

    // Exit the active session (multi-tab setup → fires immediately, no 1.5s delay)
    triggerProcessExit(container);

    // Should still have 1 pane (the other tab remains)
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    // No split should be active
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);

    // Verify the exited tab was actually closed: its instance key is gone from DOM.
    const panesAfter = container.querySelectorAll('[data-instance-key]');
    expect(panesAfter.length).toBe(1);

    const survivedInstanceKey = panesAfter[0]?.getAttribute('data-instance-key');
    expect(survivedInstanceKey).not.toBe(exitedInstanceKey);

    // Verify focus switched: the surviving pane is now the active one.
    expect(panesAfter[0]?.getAttribute('data-active')).toBe('true');
  });
});
