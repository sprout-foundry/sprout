// @ts-nocheck
/**
 * SP-140-5 step 3 — mode shell smoke tests.
 *
 * The shells own the <main> column and the chrome that belongs to a mode:
 * CodeShell composes the editor chrome (menubar, context panel, status bar),
 * DesignShell composes only the design surface. What is pinned here: Code
 * renders its full chrome, Design renders no Code chrome, and the
 * local/non-local terminal switch still lands in the Code column.
 *
 * Same lightweight-mock pattern as Sidebar.designNav.test.tsx: leaf
 * components are mocked to divs, ErrorBoundary stays real (it is
 * self-contained).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

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
  default: () => createElement('div', { className: 'mock-terminal' }),
}));

vi.mock('../components/design/DesignSurface', () => ({
  default: () => createElement('div', { className: 'mock-design-surface' }),
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import CodeShell from './CodeShell';
import DesignShell from './DesignShell';

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
    design: {
      loading: false,
      present: true,
      tab: 'flows',
      onTabChange: vi.fn(),
      onBack: vi.fn(),
      onOpenFile: vi.fn(),
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
      root.render(createElement(CodeShell, makeShellProps()));
    });

    expect(container.querySelector('main.main-content')).not.toBeNull();
    expect(container.querySelector('.mock-header-bar')).not.toBeNull();
    expect(container.querySelector('.mock-editor-workspace')).not.toBeNull();
    expect(container.querySelector('.mock-context-sidebar')).not.toBeNull();
    expect(container.querySelector('.mock-status-bar')).not.toBeNull();
  });

  it('renders the placeholder terminal without a local terminal', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: false })));
    });
    expect(container.querySelector('.mock-terminal')).not.toBeNull();
  });

  it('omits the placeholder terminal when a local terminal is supported', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps({ supportsLocalTerminal: true })));
    });
    expect(container.querySelector('.mock-terminal')).toBeNull();
  });

  it('renders the mobile pane controls on mobile', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps({ isMobile: true })));
    });
    expect(container.querySelector('.pane-controls-mobile')).not.toBeNull();
    expect(container.querySelector('.top-mobile-context-btn')).not.toBeNull();
  });

  it('omits the mobile pane controls on desktop', () => {
    act(() => {
      root.render(createElement(CodeShell, makeShellProps({ isMobile: false })));
    });
    expect(container.querySelector('.pane-controls-mobile')).toBeNull();
  });
});

describe('DesignShell', () => {
  it('renders the design surface with no Code chrome', () => {
    act(() => {
      root.render(createElement(DesignShell, makeShellProps()));
    });

    expect(container.querySelector('main.main-content')).not.toBeNull();
    expect(container.querySelector('.mock-design-surface')).not.toBeNull();
    expect(container.querySelector('.mock-header-bar')).toBeNull();
    expect(container.querySelector('.mock-editor-workspace')).toBeNull();
    expect(container.querySelector('.mock-context-sidebar')).toBeNull();
    expect(container.querySelector('.mock-status-bar')).toBeNull();
    expect(container.querySelector('.pane-controls-mobile')).toBeNull();
  });

  it('keeps the mobile pane controls to sidebar + agent toggle (§6f)', () => {
    act(() => {
      root.render(createElement(DesignShell, makeShellProps({ isMobile: true })));
    });

    expect(container.querySelector('.pane-controls-mobile')).not.toBeNull();
    expect(container.querySelector('.top-mobile-menu-btn')).not.toBeNull();
    // §6f: Design has agent presence — the mobile chat button opens the
    // agent panel. Terminal and context chrome stay Code-only.
    expect(container.querySelector('.top-mobile-chat-btn')).not.toBeNull();
    expect(container.querySelector('.top-mobile-terminal-btn')).toBeNull();
    expect(container.querySelector('.top-mobile-context-btn')).toBeNull();
  });
});
