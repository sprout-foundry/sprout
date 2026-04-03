// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import Terminal from './Terminal';

// ---------------------------------------------------------------------------
// Mock TerminalPane — forwardRef component with imperative handle { clear, focus }
// ---------------------------------------------------------------------------

jest.mock('./TerminalPane', () => {
  const React = require('react');
  return React.forwardRef(function MockTerminalPane(
    { isActive, isConnected, showCloseButton, onClose }: any,
    ref: any
  ) {
    // NOTE: jest.fn() inside useImperativeHandle creates fresh mock instances on
    // every re-render. This is acceptable because no test asserts on imperative
    // handle call counts — all assertions check DOM state. If future tests need
    // to verify clear()/focus() calls, the mock factory will need restructuring
    // to use stable references (e.g., storing mocks on a shared object that both
    // the factory and test scope can access).
    React.useImperativeHandle(ref, () => ({
      clear: jest.fn(),
      focus: jest.fn(),
    }));

    return (
      <div
        data-testid="terminal-pane"
        data-active={isActive ? 'true' : 'false'}
        data-connected={isConnected ? 'true' : 'false'}
      >
        {showCloseButton && (
          <button
            className="terminal-pane-close"
            data-testid="secondary-close-btn"
            onClick={onClose}
          >
            ✕
          </button>
        )}
      </div>
    );
  });
});

// ---------------------------------------------------------------------------
// Mock TerminalTabBar — simple presentational component
// ---------------------------------------------------------------------------

jest.mock('./TerminalTabBar', () => {
  return function MockTerminalTabBar(props: any) {
    return (
      <div data-testid="terminal-tab-bar" data-active={props.activeSessionId} />
    );
  };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderTerminal(props: Record<string, any> = {}) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  act(() => {
    root.render(
      <Terminal isConnected={true} isExpanded={false} {...props} />
    );
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
 */
function getSplitButtons(container: HTMLElement) {
  return Array.from(container.querySelectorAll('.split-btn'));
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

function getSecondaryCloseBtn(container: HTMLElement) {
  return container.querySelector('[data-testid="secondary-close-btn"]');
}

function dispatchTerminalAction(action: string) {
  const event = new CustomEvent('ledit:terminal-action', {
    detail: { action },
  });
  window.dispatchEvent(event);
}

const flushPromises = async () => {
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
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
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
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
  });

  // ── 1. Initial state: No split active ────────────────────

  it('does not have split active by default (collapsed state)', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);
    // Both buttons should exist but neither should be active
    splitBtns.forEach((btn) => {
      expect(btn.classList.contains('split-btn-active')).toBe(false);
    });
  });

  it('does not have split active by default (expanded state)', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const panesContainer = getPanesContainer(container);
    expect(panesContainer).toBeTruthy();
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);

    expect(getSplitDivider(container)).toBeNull();
    expect(getSecondaryPane(container)).toBeNull();
  });

  // ── 2. Vertical split via button click ───────────────────

  it('activates vertical split when vertical split button is clicked', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    // The vertical split button initially has aria-label="Split terminal vertically"
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    expect(verticalBtn).toBeTruthy();

    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // After clicking, the button should now have split-btn-active class
    expect(verticalBtn.classList.contains('split-btn-active')).toBe(true);
    expect(verticalBtn.getAttribute('aria-label')).toBe('Unsplit terminal');
    expect(verticalBtn.getAttribute('aria-pressed')).toBe('true');
  });

  it('shows vertical split layout in panes container after vertical split', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Click vertical split
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  // ── 3. Horizontal split via button click ─────────────────

  it('activates horizontal split when horizontal split button is clicked', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    ) as HTMLElement;
    expect(horizontalBtn).toBeTruthy();

    act(() => {
      horizontalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(horizontalBtn.classList.contains('split-btn-active')).toBe(true);
    expect(horizontalBtn.getAttribute('aria-label')).toBe('Unsplit terminal');
    expect(horizontalBtn.getAttribute('aria-pressed')).toBe('true');

    // Vertical button should NOT be active
    const verticalBtn = container.querySelector('.split-btn') as HTMLElement;
    expect(verticalBtn.classList.contains('split-btn-active')).toBe(false);
  });

  it('shows horizontal split layout in panes container after horizontal split', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    ) as HTMLElement;
    act(() => {
      horizontalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  // ── 4. Vertical split via custom event ───────────────────

  it('activates vertical split via ledit:terminal-action custom event', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // The first split-btn is vertical, second is horizontal
    const splitBtns = getSplitButtons(container);
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[0].getAttribute('aria-pressed')).toBe('true');
  });

  it('shows vertical split layout after custom event', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
  });

  // ── 5. Horizontal split via custom event ─────────────────

  it('activates horizontal split via ledit:terminal-action custom event', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const splitBtns = getSplitButtons(container);
    expect(splitBtns[1].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[1].getAttribute('aria-pressed')).toBe('true');
  });

  it('shows horizontal split layout after custom event', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
  });

  // ── 6. Toggle off: clicking same split button deactivates ─

  it('deactivates vertical split when clicking the same button again', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;

    // First click: activate
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(verticalBtn.classList.contains('split-btn-active')).toBe(true);

    // Second click: aria-label is now "Unsplit terminal"
    const unsplitBtn = container.querySelector(
      '[aria-label="Unsplit terminal"]'
    ) as HTMLElement;

    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Button should no longer be active
    expect(unsplitBtn.classList.contains('split-btn-active')).toBe(false);
    expect(unsplitBtn.getAttribute('aria-pressed')).toBe('false');
    // Label should revert back
    expect(unsplitBtn.getAttribute('aria-label')).toBe('Split terminal vertically');
  });

  it('deactivates horizontal split when clicking the same button again', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    ) as HTMLElement;

    // Activate
    act(() => {
      horizontalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(horizontalBtn.classList.contains('split-btn-active')).toBe(true);

    // Deactivate — now aria-label is "Unsplit terminal"
    // There are two buttons with "Unsplit terminal" when horizontal is active
    // We need to find the horizontal one (the second .split-btn)
    const splitBtns = getSplitButtons(container);
    const activeBtn = splitBtns.find(
      (btn) => btn.classList.contains('split-btn-active')
    ) as HTMLElement;

    act(() => {
      activeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(activeBtn.classList.contains('split-btn-active')).toBe(false);
  });

  it('removes split layout when toggling off (expanded)', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Activate vertical split
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Deactivate
    const unsplitBtn = container.querySelector(
      '[aria-label="Unsplit terminal"]'
    ) as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  // ── 7. Switch between splits ─────────────────────────────

  it('switching from horizontal to vertical replaces (not stacks) the split', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);

    // Activate horizontal
    act(() => {
      splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(splitBtns[1].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(false);

    // Now activate vertical — should replace horizontal
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[1].classList.contains('split-btn-active')).toBe(false);
  });

  it('switching from vertical to horizontal replaces (not stacks) the split', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);

    // Activate vertical
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(true);

    // Now activate horizontal
    act(() => {
      splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(splitBtns[1].classList.contains('split-btn-active')).toBe(true);
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(false);
  });

  it('switching split direction updates CSS classes correctly (expanded)', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);

    // Activate vertical
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    let panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);

    // Switch to horizontal
    act(() => {
      splitBtns[1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  // ── 8. Split creates secondary session ───────────────────

  it('vertical split creates a secondary session', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Before split: one pane wrapper (primary)
    const wrappersBefore = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersBefore.length).toBe(1);

    // Activate vertical split
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // After split: two pane wrappers
    const wrappersAfter = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersAfter.length).toBe(2);
  });

  it('horizontal split creates a secondary session', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const wrappersBefore = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersBefore.length).toBe(1);

    // Activate horizontal split
    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    ) as HTMLElement;
    act(() => {
      horizontalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const wrappersAfter = container.querySelectorAll('.terminal-pane-wrapper');
    expect(wrappersAfter.length).toBe(2);
  });

  it('switching split directions reuses the same secondary pane (no third pane)', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Activate split
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Deactivate split
    const unsplitBtn = container.querySelector(
      '[aria-label="Unsplit terminal"]'
    ) as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Back to 1 pane
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
    expect(getSecondaryPane(container)).toBeNull();
  });

  it('unsplitting hides the divider', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Activate split
    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getSplitDivider(container)).toBeTruthy();

    // Deactivate
    const unsplitBtn = container.querySelector(
      '[aria-label="Unsplit terminal"]'
    ) as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getSplitDivider(container)).toBeNull();
  });

  // ── 10. Split CSS classes ────────────────────────────────

  it('vertical split adds terminal-split-vertical class to panes container', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);
  });

  it('horizontal split adds terminal-split-horizontal class to panes container', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(true);
  });

  it('no split class present when split is deactivated', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Activate then deactivate
    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const splitBtns = getSplitButtons(container);
    const activeBtn = splitBtns.find(
      (btn) => btn.classList.contains('split-btn-active')
    ) as HTMLElement;
    act(() => {
      activeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  it('divider has terminal-split-divider-vertical class during vertical split', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const divider = getSplitDivider(container);
    expect(divider).toBeTruthy();
    expect(divider?.classList.contains('terminal-split-divider-vertical')).toBe(true);
    expect(divider?.classList.contains('terminal-split-divider-horizontal')).toBe(false);
  });

  it('divider has terminal-split-divider-horizontal class during horizontal split', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    expect(getSplitDivider(container)).toBeNull();
  });

  it('split divider IS present when vertical split is active', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    expect(getSplitDivider(container)).toBeTruthy();
  });

  it('split divider IS present when horizontal split is active', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    expect(getSplitDivider(container)).toBeTruthy();
  });

  // ── 12. Secondary pane close button ──────────────────────

  it('secondary pane has a close button when split is active', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // The mocked TerminalPane renders a close button when showCloseButton=true
    const closeBtn = getSecondaryCloseBtn(container);
    expect(closeBtn).toBeTruthy();
  });

  it('primary pane does NOT have a close button (showCloseButton is false)', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Primary pane is the first .terminal-pane-wrapper
    const primaryWrapper = container.querySelector('.terminal-pane-wrapper');
    const closeBtnInPrimary = primaryWrapper?.querySelector('.terminal-pane-close');
    expect(closeBtnInPrimary).toBeNull();
  });

  // ── 13. Closing secondary pane unsplits ──────────────────

  it('clicking close on secondary pane removes the split', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    // Verify split is active
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Click the close button on the secondary pane
    const closeBtn = getSecondaryCloseBtn(container);
    expect(closeBtn).toBeTruthy();

    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Split should be removed
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
  });

  it('closing secondary pane removes the divider', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_horizontal');
    });

    expect(getSplitDivider(container)).toBeTruthy();

    const closeBtn = getSecondaryCloseBtn(container);
    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(getSplitDivider(container)).toBeNull();
  });

  it('closing secondary pane removes split CSS classes', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(true);

    const closeBtn = getSecondaryCloseBtn(container);
    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
  });

  it('closing secondary pane deactivates split button state', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('split_vertical');
    });

    const splitBtns = getSplitButtons(container);
    expect(splitBtns[0].classList.contains('split-btn-active')).toBe(true);

    const closeBtn = getSecondaryCloseBtn(container);
    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Both buttons should be inactive
    splitBtns.forEach((btn) => {
      expect(btn.classList.contains('split-btn-active')).toBe(false);
    });
  });

  // ── 14. Edge cases ───────────────────────────────────────

  it('custom event with unknown action does not affect split state', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      dispatchTerminalAction('unknown_action');
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
    expect(panesContainer?.classList.contains('terminal-split-horizontal')).toBe(false);
    expect(getSplitDivider(container)).toBeNull();
  });

  it('custom event without detail does not crash', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      window.dispatchEvent(new CustomEvent('ledit:terminal-action'));
    });

    // Should not crash; split state unchanged
    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('custom event with null detail does not crash', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    act(() => {
      window.dispatchEvent(
        new CustomEvent('ledit:terminal-action', { detail: null })
      );
    });

    const panesContainer = getPanesContainer(container);
    expect(panesContainer?.classList.contains('terminal-split-vertical')).toBe(false);
  });

  it('split state persists after re-expanding the terminal', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);
    expect(splitBtns.length).toBe(2);
    splitBtns.forEach((btn) => {
      expect(btn.hasAttribute('aria-pressed')).toBe(true);
      expect(btn.getAttribute('aria-pressed')).toBe('false');
    });
  });

  it('vertical split button aria-pressed becomes true when active', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(verticalBtn.getAttribute('aria-pressed')).toBe('true');
  });

  it('horizontal split button aria-pressed becomes true when active', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    ) as HTMLElement;
    act(() => {
      horizontalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(horizontalBtn.getAttribute('aria-pressed')).toBe('true');
  });

  it('split buttons have aria-label attributes', () => {
    const result = renderTerminal();
    container = result.container;
    root = result.root;

    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    );
    const horizontalBtn = container.querySelector(
      '[aria-label="Split terminal horizontally"]'
    );

    expect(verticalBtn).toBeTruthy();
    expect(horizontalBtn).toBeTruthy();
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
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
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
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
  });

  it('can re-split after unsplitting via secondary pane close button', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;

    // First split
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unsplit via secondary pane close
    const closeBtn = getSecondaryCloseBtn(container);
    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Re-split should create a new secondary session (not reuse old)
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('can re-split after unsplitting via split button toggle', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const verticalBtn = container.querySelector(
      '[aria-label="Split terminal vertically"]'
    ) as HTMLElement;

    // First split
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Unsplit via button toggle
    const unsplitBtn = container.querySelector(
      '[aria-label="Unsplit terminal"]'
    ) as HTMLElement;
    act(() => {
      unsplitBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Re-split
    act(() => {
      verticalBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);
  });

  it('splitting multiple times in a row does not create duplicate panes', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const splitBtns = getSplitButtons(container);

    // Split
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Click again (same direction) should unsplit
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);

    // Click again should re-split
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Never more than 2
    act(() => {
      splitBtns[0].dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(1);
  });

  it('terminal remains functional (1 pane) after closing secondary pane', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    // Split vertically
    act(() => {
      dispatchTerminalAction('split_vertical');
    });
    expect(container.querySelectorAll('.terminal-pane-wrapper').length).toBe(2);

    // Close secondary pane
    const closeBtn = getSecondaryCloseBtn(container);
    act(() => {
      closeBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
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
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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

  const STORAGE_KEY = 'ledit-terminal-height';

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    localStorage.removeItem(STORAGE_KEY);
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
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
    document.documentElement.style.removeProperty('--ledit-terminal-reserved-height');
  });

  it('initializes with default height (400px) when localStorage is empty', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('400px');

    // localStorage should NOT have been written just from mounting
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it('reads persisted height from localStorage on mount', () => {
    localStorage.setItem(STORAGE_KEY, '250');

    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('250px');
  });

  it('clamps persisted value below minimum to minimum (120px)', () => {
    localStorage.setItem(STORAGE_KEY, '50');

    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('120px');
  });

  it('clamps persisted invalid value to default (400px)', () => {
    localStorage.setItem(STORAGE_KEY, 'not-a-number');

    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const terminalEl = container.querySelector('.terminal-container') as HTMLElement;
    expect(terminalEl.style.height).toBe('400px');
  });

  it('persists height to localStorage after resize drag completes', () => {
    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

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
        })
      );
    });

    // The resize sets isResizingVertical, now trigger mouseup
    act(() => {
      document.dispatchEvent(
        new MouseEvent('mouseup', {
          clientX: 0,
          clientY: 600,
        })
      );
    });

    // After drag completes, height should be persisted
    expect(localStorage.getItem(STORAGE_KEY)).toBe('400');
  });

  it('sets correct CSS variable when expanded with persisted height', () => {
    localStorage.setItem(STORAGE_KEY, '300');

    const result = renderTerminal({ isExpanded: true });
    container = result.container;
    root = result.root;

    const reservedHeight = document.documentElement.style.getPropertyValue('--ledit-terminal-reserved-height');
    expect(reservedHeight).toBe('300px');
  });

  it('falls back to collapsed height CSS variable when not expanded', () => {
    localStorage.setItem(STORAGE_KEY, '500');

    const result = renderTerminal({ isExpanded: false });
    container = result.container;
    root = result.root;

    const reservedHeight = document.documentElement.style.getPropertyValue('--ledit-terminal-reserved-height');
    // When collapsed, should use collapsedHeight (42px on non-mobile), not the persisted height
    expect(reservedHeight).toBe('42px');
  });
});
