// @ts-nocheck
/**
 * SP-140-5 step 3 — mode shell smoke tests.
 *
 * The shells own the <main> column and the chrome that belongs to a mode:
 * CodeShell composes the editor chrome (menubar, context panel, status bar)
 * and the preview panel. What is pinned here: Code renders its full chrome,
 * the preview panel shows the mockup when the workspace has one and the dev
 * server's output otherwise, and the local/non-local terminal switch still
 * lands in the Code column.
 *
 * Leaf components are mocked to divs; ErrorBoundary stays real (it is
 * self-contained).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { HostProvider } from '../host';
import { makeTestHost } from '../host/testHost';

/**
 * Wrap a tree in a host provider so useHost()/useHostCapabilities() consumers
 * resolve (the feat/host-contract refactor made a HostProvider mandatory).
 *
 * The shell smoke tests represent a LOCAL shell (makeShellProps defaults
 * supportsLocalTerminal: true), so the default host turns the local-only
 * capabilities on (agentChanges drives CodeShell's context-pane chrome). Pass
 * an override to exercise a different capability mix.
 */
function host(children: unknown, capabilities: Record<string, boolean> = { agentChanges: true }): unknown {
  return createElement(HostProvider, { host: makeTestHost(capabilities) }, children);
}

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing the shells
// ---------------------------------------------------------------------------

vi.mock('../components/HeaderBar', () => ({
  default: () => createElement('div', { className: 'mock-header-bar' }),
}));

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

vi.mock('../components/MockupPanel', () => ({
  default: () => createElement('div', { className: 'mock-mockup-panel' }),
}));
vi.mock('../components/PreviewPanel', () => ({
  default: ({ open }: { open: boolean }) => (open ? createElement('div', { className: 'mock-preview-panel' }) : null),
}));

// The preview panel (rendered by CodeShell) drives the dev-server lifecycle API
// through the adapter fetch. Freeze it so the shell smoke tests stay hermetic:
// no provider is mounted and no request is ever issued. importOriginal keeps
// the real exports (useSproutAdapter, the provider) for anything else that reads
// them.
vi.mock('../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../contexts/SproutAdapterContext')>();
  return {
    ...actual,
    useSproutFetch: () => () => new Promise<Response>(() => {}),
  };
});

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import CodeShell from './CodeShell';

// ---------------------------------------------------------------------------
// Test setup
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
    onSessionRestore: vi.fn(),
    chat: {
      chatProps: {},
      reviewProps: {},
      diffState: {},
    },
    mockup: {
      loading: false,
      present: false,
    },
    git: {
      gitBranches: { current: 'main', branches: [] },
      gitStatus: null,
      workspaceRoot: '/tmp/repo',
    },
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('CodeShell', () => {
  it('renders its full chrome around the editor surface', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    expect(container.querySelector('main.main-content')).not.toBeNull();
    expect(container.querySelector('.mock-header-bar')).not.toBeNull();
    expect(container.querySelector('.mock-editor-workspace')).not.toBeNull();
    expect(container.querySelector('.mock-context-sidebar')).not.toBeNull();
    expect(container.querySelector('.mock-status-bar')).not.toBeNull();
  });

  it('renders the placeholder terminal without a local terminal', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: false }))));
    });
    expect(container.querySelector('.mock-terminal')).not.toBeNull();
  });

  it('opens the placeholder terminal only when the user left it open', () => {
    act(() => {
      root.render(
        host(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: false, isTerminalExpanded: false }))),
      );
    });
    expect(container.querySelector('.mock-terminal')?.getAttribute('data-expanded')).toBe('false');
    act(() => {
      root.render(
        host(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: false, isTerminalExpanded: true }))),
      );
    });
    expect(container.querySelector('.mock-terminal')?.getAttribute('data-expanded')).toBe('true');
  });

  it('omits the placeholder terminal when a local terminal is supported', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: true }))));
    });
    expect(container.querySelector('.mock-terminal')).toBeNull();
  });

  it('renders the mobile pane controls on mobile', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ isMobile: true }))));
    });
    expect(container.querySelector('.pane-controls-mobile')).not.toBeNull();
    expect(container.querySelector('.top-mobile-context-btn')).not.toBeNull();
  });

  it('omits the mobile pane controls on desktop', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ isMobile: false }))));
    });
    expect(container.querySelector('.pane-controls-mobile')).toBeNull();
  });

  it('opens the preview panel on the mockup when the workspace has one', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ mockup: { loading: false, present: true } }))));
    });
    expect(container.querySelector('.mock-mockup-panel')).not.toBeNull();
    expect(container.querySelector('.mock-preview-panel')).toBeNull();
  });

  it('leaves the preview panel closed when there is no mockup', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });
    expect(container.querySelector('.mock-mockup-panel')).toBeNull();
    expect(container.querySelector('.mock-preview-panel')).toBeNull();
  });
});
