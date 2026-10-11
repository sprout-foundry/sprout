/**
 * CodeShell's in-browser terminal is mounted only when the composition asks
 * for it. A host with no terminal at all (`inBrowserTerminal: false`) gets no
 * terminal element and no WASM shell started; the default keeps the in-browser
 * terminal, which starts the shell once opened.
 *
 * The real docked terminal and its panes run; the editor chrome is stubbed,
 * xterm is inert (jsdom cannot lay it out) and the WASM shell is a spy.
 */

import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const wasm = vi.hoisted(() => ({ init: vi.fn() }));

vi.mock('../services/wasmShell', () => ({
  initWasmShell: wasm.init.mockImplementation(async () => ({
    executeCommand: () => ({ stdout: '', stderr: '', exitCode: 0 }),
    autoComplete: () => ({ completions: [] }),
    getCwd: () => '/home/user',
    getWorkspaceRoot: () => '/home/user',
  })),
}));
vi.mock('@xterm/xterm', () => ({
  Terminal: vi.fn(function (this: Record<string, unknown>) {
    Object.assign(this, {
      cols: 80,
      rows: 24,
      options: {},
      buffer: { active: { baseY: 0, getLine: () => null } },
      loadAddon: vi.fn(),
      open: vi.fn(),
      focus: vi.fn(),
      clear: vi.fn(),
      dispose: vi.fn(),
      write: vi.fn(),
      writeln: vi.fn(),
      hasSelection: () => false,
      getSelection: () => '',
      attachCustomKeyEventHandler: vi.fn(),
      registerLinkProvider: vi.fn(() => ({ dispose: vi.fn() })),
      onSelectionChange: vi.fn(() => ({ dispose: vi.fn() })),
      onTitleChange: vi.fn(() => ({ dispose: vi.fn() })),
      onData: vi.fn(() => ({ dispose: vi.fn() })),
    });
  }),
}));
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn(function (this: Record<string, unknown>) {
    this.fit = vi.fn();
  }),
}));
vi.mock('@xterm/addon-search', () => ({
  SearchAddon: vi.fn(function (this: Record<string, unknown>) {
    this.clearDecorations = vi.fn();
    this.onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    this.dispose = vi.fn();
  }),
}));
vi.mock('../contexts/ThemeContext', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../contexts/ThemeContext')>()),
  useTheme: () => ({ theme: 'dark', themePack: { id: 'default' } }),
}));
vi.mock('../hooks/useAvailableShells', () => ({
  useAvailableShells: () => ({
    availableShells: [],
    shellsLoaded: true,
    selectedShell: null,
    setSelectedShell: vi.fn(),
  }),
}));
vi.mock('../hooks/useAttachableSessions', () => ({
  useAttachableSessions: () => ({
    attachableSessions: [],
    setAttachableSessions: vi.fn(),
    fetchAttachableSessions: vi.fn(async () => undefined),
  }),
}));
vi.mock('../components/HeaderBar', () => ({ default: () => <div /> }));
vi.mock('../components/EditorWorkspace', () => ({ default: () => <div /> }));
vi.mock('../components/ContextSidebar', () => ({ default: () => <div /> }));
vi.mock('../components/StatusBar', () => ({ default: () => <div /> }));
vi.mock('../components/PreviewPanel', () => ({ default: () => null, PreviewPanel: () => null }));

import { HostProvider } from '../host';
import { makeTestHost } from '../host/testHost';
import CodeShell from './CodeShell';
import type { WorkspaceShellProps } from './shell';

function shellProps(overrides: Partial<WorkspaceShellProps>): WorkspaceShellProps {
  return {
    isMobile: false,
    isTablet: false,
    isSidebarOpen: false,
    isConnected: true,
    currentView: 'chat',
    onViewChange: vi.fn(),
    onToggleSidebar: vi.fn(),
    onToggleContextPanel: vi.fn(),
    supportsLocalTerminal: false,
    isTerminalExpanded: true,
    onTerminalExpandedChange: vi.fn(),
    showContextSidebar: false,
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
    chat: { chatProps: {}, reviewProps: {}, diffState: {} } as unknown as WorkspaceShellProps['chat'],
    design: {} as WorkspaceShellProps['design'],
    git: { gitBranches: { current: '', branches: [] }, gitStatus: null, workspaceRoot: '' },
    ...overrides,
  };
}

function renderShell(overrides: Partial<WorkspaceShellProps>) {
  return render(
    <HostProvider host={makeTestHost({ localTerminal: false })}>
      <CodeShell {...shellProps(overrides)} />
    </HostProvider>,
  );
}

afterEach(() => {
  wasm.init.mockClear();
});

describe('CodeShell in-browser terminal', () => {
  it('mounts the in-browser terminal by default and starts its shell once open', async () => {
    renderShell({});
    expect(screen.getByTestId('terminal-container')).toBeInTheDocument();
    await waitFor(() => expect(wasm.init).toHaveBeenCalled());
  });

  it('mounts no terminal and starts no shell when the composition has no terminal', async () => {
    renderShell({ inBrowserTerminal: false });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.queryByTestId('terminal-container')).not.toBeInTheDocument();
    expect(wasm.init).not.toHaveBeenCalled();
  });
});
