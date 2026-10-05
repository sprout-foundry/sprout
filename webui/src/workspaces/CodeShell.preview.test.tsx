// @ts-nocheck
/**
 * CodeShell preview-panel wiring (SP-155 §155a, item 155.6).
 *
 * Pins that the preview panel is mounted in Code mode and driven by the
 * HeaderBar toggle:
 *   1. The toggle is present (CodeShell wires it into the HeaderBar).
 *   2. The panel is closed by default (no pane rendered, no polling).
 *   3. Clicking the toggle opens the panel — the preview pane renders in the
 *      Code surface, and the toggle reflects the open state.
 *
 * Same lightweight-mock pattern as shell.test.tsx: leaf components are mocked
 * to divs. The HeaderBar mock renders the real preview toggle (so the test can
 * drive CodeShell's open state), and the adapter fetch is mocked so the
 * PreviewPanel's hook makes no real requests.
 */

import { fireEvent } from '@testing-library/react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

// NOTE: the vitest setup mocks `Element.prototype.click` to a no-op, so the
// tests dispatch events via `fireEvent.click` (which uses `dispatchEvent`).

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing CodeShell
// ---------------------------------------------------------------------------

vi.mock('../components/EditorWorkspace', () => ({
  default: () => createElement('div', { className: 'mock-editor-workspace' }),
}));
vi.mock('../components/ContextSidebar', () => ({
  default: () => createElement('div', { className: 'mock-context-sidebar' }),
}));
vi.mock('../components/StatusBar', () => ({
  default: () => createElement('div', { className: 'mock-status-bar' }),
}));
vi.mock('../components/Terminal', () => ({
  default: (props: { isExpanded?: boolean }) =>
    createElement('div', { className: 'mock-terminal', 'data-expanded': String(!!props.isExpanded) }),
}));
// Render the real preview toggle so the test can drive CodeShell's open state.
vi.mock('../components/HeaderBar', () => ({
  default: (props: { onTogglePreviewPanel?: () => void; previewPanelOpen?: boolean }) =>
    props.onTogglePreviewPanel
      ? createElement('button', {
          'data-testid': 'preview-panel-toggle',
          'aria-pressed': String(!!props.previewPanelOpen),
          onClick: () => props.onTogglePreviewPanel?.(),
        })
      : createElement('div', { className: 'mock-header-bar' }),
}));
// The adapter fetch the preview hook uses. This wiring test isolates the
// panel's open/close state from the dev-server lifecycle: the fetch is frozen
// (never resolves) so the hook stays in its synchronous initial `stopped` state
// and never fires an async `setState` (which would need an `act` flush — and an
// `act` flush stalls on the live 5s heartbeat interval). The running/failed
// lifecycle is asserted in PreviewPanel.test.tsx, where the panel is rendered
// in isolation with a controllable fetch.
vi.mock('../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => () =>
    new Promise<Response>(() => {
      /* frozen: never resolves, so no poll update fires */
    }),
}));

import CodeShell from './CodeShell';

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

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
    design: { loading: false, present: true, tab: 'flows', onTabChange: vi.fn(), onOpenFile: vi.fn() },
    git: { gitBranches: { current: 'main', branches: [] }, gitStatus: null, workspaceRoot: '/tmp/repo' },
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
//
// The preview pane renders its root + initial (stopped) state synchronously the
// moment the panel opens — before the hook's poll resolves — so these tests
// assert synchronously and never `await` the poll's promise chain (an async
// `act` flush stalls on the live 5s heartbeat interval, real or fake).
// ---------------------------------------------------------------------------

describe('CodeShell preview panel', () => {
  it('mounts the preview toggle, with the panel closed by default', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps()));
    });

    // CodeShell wires the toggle into the HeaderBar.
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).not.toBeNull();
    // Closed by default: no pane is rendered (and the toggle is not pressed).
    expect(container.querySelector('[data-testid="preview-pane"]')).toBeNull();
    expect(container.querySelector('[data-testid="preview-panel"]')).toBeNull();
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toHaveAttribute('aria-pressed', 'false');
  });

  it('opens the preview panel from the toggle (the pane renders in Code mode)', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps()));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    fireEvent.click(toggle);

    // The panel is open and renders the preview pane in the Code surface.
    expect(container.querySelector('[data-testid="preview-panel"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="preview-pane"]')).not.toBeNull();
    // The toggle reflects the open state.
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
  });

  it('closes the panel again when the toggle is clicked a second time', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps()));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    fireEvent.click(toggle);
    expect(container.querySelector('[data-testid="preview-pane"]')).not.toBeNull();

    fireEvent.click(toggle);
    expect(container.querySelector('[data-testid="preview-pane"]')).toBeNull();
  });

  it('renders the pane in its initial lifecycle state (stopped)', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps()));
    });
    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    fireEvent.click(toggle);

    expect(container.querySelector('[data-testid="preview-pane-stopped"]')).not.toBeNull();
  });
});
