/**
 * CodeShell's right-hand context panel (the agent's change history).
 *
 * Sprout's own app keeps the panel and its header toggle on a host with
 * `capabilities.agentChanges`; a composition that passes `contextPanel={false}`
 * (the composed workspace) gets neither the column nor the toggles.
 */

import { render, screen } from '@testing-library/react';
import { createElement } from 'react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../components/EditorWorkspace', () => ({ default: () => createElement('div') }));
vi.mock('../components/ContextSidebar', () => ({
  default: () => createElement('div', { 'data-testid': 'mock-context-sidebar' }),
}));
vi.mock('../components/StatusBar', () => ({ default: () => createElement('div') }));
vi.mock('../components/Terminal', () => ({ default: () => createElement('div') }));
vi.mock('../components/HeaderBar', () => ({
  default: (props: { hasContextPanel?: boolean }) =>
    props.hasContextPanel ? createElement('button', { 'aria-label': 'Toggle context panel' }) : null,
}));

import { HostProvider } from '../host';
import { makeTestHost } from '../host/testHost';
import CodeShell from './CodeShell';
import type { WorkspaceShellProps } from './shell';

function shellProps(overrides: Partial<WorkspaceShellProps> = {}): WorkspaceShellProps {
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
    previewAvailable: false,
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
    chat: {
      chatProps: {} as WorkspaceShellProps['chat']['chatProps'],
      reviewProps: {} as WorkspaceShellProps['chat']['reviewProps'],
      diffState: {} as WorkspaceShellProps['chat']['diffState'],
    },
    design: {
      loading: false,
      present: false,
      treeState: 'none',
      frontendLike: false,
      recheck: vi.fn(),
      tab: 'flows',
      onTabChange: vi.fn(),
    },
    git: { gitBranches: { current: 'main', branches: [] }, gitStatus: null, workspaceRoot: '/tmp/repo' },
    ship: { live: {}, availability: { canDeploy: false }, history: [] },
    ...overrides,
  };
}

function renderShell(props: WorkspaceShellProps, agentChanges = true) {
  return render(createElement(HostProvider, { host: makeTestHost({ agentChanges }) }, createElement(CodeShell, props)));
}

describe('CodeShell context panel', () => {
  it('renders the panel and its toggle by default on an agent-changes host', () => {
    renderShell(shellProps());
    expect(screen.getByTestId('mock-context-sidebar')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Toggle context panel' })).toBeInTheDocument();
  });

  it('renders neither when the composition leaves the panel out', () => {
    renderShell(shellProps({ contextPanel: false }));
    expect(screen.queryByTestId('mock-context-sidebar')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Toggle context panel' })).not.toBeInTheDocument();
  });

  it('renders neither on a host without agent changes', () => {
    renderShell(shellProps(), false);
    expect(screen.queryByTestId('mock-context-sidebar')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Toggle context panel' })).not.toBeInTheDocument();
  });
});
