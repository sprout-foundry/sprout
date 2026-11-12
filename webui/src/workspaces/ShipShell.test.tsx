/**
 * Ship mode — shell and registry integration.
 *
 * What is pinned: the Ship mode registers through the public mode API and
 * appears in the switcher; its shell renders the ship surface and none of the
 * other modes' chrome; Code is unchanged by its registration (it keeps its
 * identity and position, and still works).
 *
 * Registration is a module side effect, so this file imports `ship-mode` (which
 * registers at load) and asserts the registry as the app sees it. A test that
 * registers its own Ship definition re-registers in place and disposes after,
 * so the module's own registration outlives the test — matching how the app
 * loads it.
 */

// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import CodeShell from './CodeShell';
import { WORKSPACE_MODES, availableModes, registerWorkspaceMode, type WorkspaceModeRegistration } from './registry';
import { SHIP_MODE_ID, registerShipMode } from './ship-mode';
import ShipShell from './ShipShell';

const withMockup = { hasMockup: true };

/** A full shell props object; overrides win. The shells read only their slice. */
function makeShellProps(overrides = {}) {
  return {
    isMobile: false,
    isTablet: false,
    isSidebarOpen: false,
    isConnected: true,
    currentView: 'chat',
    onViewChange: vi.fn(),
    onToggleSidebar: vi.fn(),
    onToggleContextPanel: vi.fn(),
    supportsLocalTerminal: true,
    isTerminalExpanded: false,
    onTerminalExpandedChange: vi.fn(),
    showContextSidebar: true,
    contextPanelRef: { current: null },
    toolExecutions: [],
    logs: [],
    subagentActivities: [],
    messages: [],
    isProcessing: false,
    lastError: null,
    queryProgress: null,
    currentBuffer: null,
    handleOutlineNavigateToSymbol: vi.fn(),
    chat: { chatProps: {}, reviewProps: {}, diffState: {} },
    mockup: { loading: false, present: true },
    git: { gitBranches: { current: 'main', branches: [] }, gitStatus: null, workspaceRoot: '/tmp/repo' },
    ship: {
      live: { url: 'https://example.pages.dev', version: 'v1.0.0', deployedAt: '2026-01-02T03:04:05Z', state: 'ready' },
      availability: { canDeploy: true },
      history: [],
    },
    ...overrides,
  };
}

describe('Ship mode registration', () => {
  it('is registered through the public API and offered for every workspace', () => {
    const ship = WORKSPACE_MODES.find((mode) => mode.id === SHIP_MODE_ID);
    expect(ship).toBeDefined();
    expect(ship?.label).toBe('Ship');
    expect(ship?.Shell).toBe(ShipShell);
    expect(ship?.available(withMockup)).toBe(true);
    expect(ship?.available({ hasMockup: false })).toBe(true);
    expect(availableModes(withMockup).map((mode) => mode.id)).toContain('ship');
  });

  it('lists after the Code built-in', () => {
    const ids = availableModes(withMockup).map((mode) => mode.id);
    expect(ids[0]).toBe('code');
    expect(ids).toContain('ship');
  });

  it('leaves Code unchanged', () => {
    const code = WORKSPACE_MODES.find((mode) => mode.id === 'code');
    expect(code?.Shell).toBe(CodeShell);
    expect(code?.label).toBe('Code');
  });

  it('re-registers in place through the public API without duplicating', () => {
    const before = WORKSPACE_MODES.length;
    const dispose = registerShipMode();
    expect(WORKSPACE_MODES.length).toBe(before);
    expect(WORKSPACE_MODES.filter((mode) => mode.id === SHIP_MODE_ID)).toHaveLength(1);
    // The module's own registration must survive a test that re-registers and
    // disposes; re-register it so later tests still see Ship.
    dispose();
    registerShipMode();
    expect(WORKSPACE_MODES.filter((mode) => mode.id === SHIP_MODE_ID)).toHaveLength(1);
  });

  it('accepts an override payload through the public API', () => {
    const override: WorkspaceModeRegistration = {
      id: SHIP_MODE_ID,
      label: 'Ship (override)',
      icon: () => null,
      hint: 'overridden',
      available: () => true,
      Shell: ShipShell,
    };
    // The public API is idempotent for a non-built-in id: register + dispose.
    const dispose = registerWorkspaceMode(override);
    expect(WORKSPACE_MODES.find((mode) => mode.id === SHIP_MODE_ID)?.label).toBe('Ship (override)');
    dispose();
    // Restore the module's definition for the remaining tests.
    registerShipMode();
    expect(WORKSPACE_MODES.find((mode) => mode.id === SHIP_MODE_ID)?.label).toBe('Ship');
  });
});

describe('ShipShell', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  it('renders the ship surface and no other mode chrome', () => {
    act(() => {
      root.render(createElement(ShipShell, makeShellProps()));
    });

    expect(container.querySelector('main.main-content')).not.toBeNull();
    expect(container.querySelector('[data-testid="ship-surface"]')).not.toBeNull();
    // No Code chrome.
    expect(container.querySelector('.mock-header-bar')).toBeNull();
    expect(container.querySelector('.mock-status-bar')).toBeNull();
  });

  it('renders the mobile sidebar toggle on mobile', () => {
    act(() => {
      root.render(createElement(ShipShell, makeShellProps({ isMobile: true })));
    });
    expect(container.querySelector('.pane-controls-mobile')).not.toBeNull();
    expect(container.querySelector('.top-mobile-menu-btn')).not.toBeNull();
  });
});
