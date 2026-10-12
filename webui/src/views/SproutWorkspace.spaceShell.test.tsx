/**
 * SproutWorkspace in own mode without `layout`: the composed space shell —
 * the app's project sidebar (Files / Search / Source control), the Code
 * shell (editor tabs with the conversation, the status bar), the docked
 * terminal — against a daemon host, an in-browser host and a host with
 * neither git nor a terminal.
 *
 * The provider stack, the units, the sidebar, the Code shell, the editor
 * tabs, the file tree, the search panel, the git section and the docked
 * terminal's chrome are the real ones; the editor pane, the chat surface, the
 * agent-changes panel and the terminal workbench are stubbed, and the page
 * fetch stands in for the host's backend.
 */

import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    connect = vi.fn();
    disconnect = vi.fn();
    onEvent = vi.fn();
    removeEvent = vi.fn();
    sendEvent = vi.fn();
    isConnected = vi.fn(() => true);
    onReconnect = vi.fn();
    freeze = vi.fn();
    resume = vi.fn();
    resetAndReconnect = vi.fn();
    getQueuedMessageCount = vi.fn(() => 0);
  },
}));
vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="shell-test-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../hooks/useTerminalWorkbench', () => ({
  useTerminalWorkbench: () => ({ focusedSession: { name: 'Session 1' }, totalSessions: 1 }),
}));
vi.mock('../components/TerminalWorkbench', () => ({
  TerminalWorkbench: (props: { isActive: boolean; isConnected: boolean; trailingActions?: JSX.Element }) => (
    <div data-testid="shell-test-workbench" data-active={String(props.isActive)} data-pty={String(props.isConnected)}>
      {props.trailingActions}
    </div>
  ),
}));
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return <div data-testid="shell-test-editor-pane" data-path={buffer?.file.path} />;
    },
  };
});

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { __resetCommandHandlersForTests, hasCommandHandler } from '../services/commandHandlers';
import { SproutWorkspace } from './SproutWorkspace';

const PROJECT = { id: '/work/widgets', root: '/work/widgets', name: 'widgets' };

function host(
  capabilities: Partial<SproutHost['capabilities']> = {},
  agent?: SproutHost['transport']['agent'],
): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    capabilities: { ...base.capabilities, ...capabilities },
    transport: { apiBaseURL: 'https://host.test', wsURL: '', authMode: 'none', ...(agent ? { agent } : {}) },
  };
}

const daemonHost = () => host({ git: true, localTerminal: true, workspaceSwitching: true });

const REPO_STATUS = {
  message: 'success',
  in_git_repo: true,
  status: {
    branch: 'main',
    ahead: 0,
    behind: 0,
    staged: [],
    modified: [{ path: 'src/changed.ts', status: 'M' }],
    untracked: [],
    deleted: [],
    renamed: [],
  },
};

const FILES = {
  message: 'success',
  files: [
    { name: 'src', path: 'src', is_dir: true },
    { name: 'README.md', path: 'README.md', is_dir: false, size: 10 },
  ],
};

const SEARCH_RESULT = {
  results: [
    {
      file: 'src/found.ts',
      match_count: 1,
      matches: [
        {
          line_number: 12,
          line: 'const needle = 1;',
          column_start: 6,
          column_end: 12,
          context_before: [],
          context_after: [],
        },
      ],
    },
  ],
  total_matches: 1,
  total_files: 1,
  truncated: false,
};

let inRepo = true;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

const fetchSpy = vi.fn(async (input: unknown) => {
  const url = String(input);
  if (url.includes('/api/git/status')) {
    return json(inRepo ? REPO_STATUS : { message: 'success', in_git_repo: false });
  }
  if (url.includes('/api/git/branches')) return json({ message: 'success', current: 'main', branches: ['main'] });
  if (url.includes('/api/git/')) return json({ message: 'success' });
  if (url.includes('/api/files')) return json(FILES);
  if (url.includes('/api/search')) return json(SEARCH_RESULT);
  if (url.includes('/api/workspace')) return json({ workspace_root: '/work/widgets', daemon_root: '/work/widgets' });
  return json({});
});

const requests = () => fetchSpy.mock.calls.map(([url]) => String(url));

vi.setConfig({ testTimeout: 20000 });

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  inRepo = true;
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  __resetCommandHandlersForTests();
  setActiveHost(headlessHost());
  vi.stubGlobal('fetch', fetchSpy);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function mount(workspaceHost: SproutHost, space: 'code' | 'design' = 'code') {
  setActiveHost(workspaceHost);
  return render(<SproutWorkspace project={PROJECT} space={space} host={workspaceHost} />);
}

function tabPaths(): string[] {
  return Array.from(document.querySelectorAll('[data-buffer-id]')).map((tab) => tab.getAttribute('title') ?? '');
}

/** Whether a tab is open for a workspace path (the editor holds it absolute once the root is known). */
function hasTab(path: string): boolean {
  return tabPaths().some((tab) => tab === path || tab.endsWith(`/${path}`));
}

describe('SproutWorkspace space shell (own mode, no layout)', () => {
  it('renders the sidebar sections, editor tabs, the conversation, the docked terminal and the status bar', async () => {
    mount(daemonHost());
    const shell = await screen.findByTestId('workspace-space-shell');
    expect(shell).toHaveAttribute('data-lane', 'code');

    const sidebar = screen.getByTestId('workspace-sidebar');
    const sections = within(sidebar).getByRole('tablist', { name: 'Files' });
    expect(
      within(sections)
        .getAllByRole('tab')
        .map((tab) => tab.getAttribute('aria-label')),
    ).toEqual(['Files', 'Search', 'Source control']);
    // Files opens first, listed from the host.
    expect(await within(sidebar).findByText('README.md')).toBeInTheDocument();
    expect(requests().some((url) => url.startsWith('https://host.test/api/files'))).toBe(true);

    // The editor tabs, with the conversation in its own tab.
    expect(tabPaths()).toContain('__workspace/chat');
    expect(screen.getByTestId('shell-test-chat')).toBeInTheDocument();

    // The docked PTY terminal, collapsed to its strip.
    const terminal = screen.getByTestId('workspace-docked-terminal');
    expect(terminal).toHaveAttribute('data-backend', 'pty');
    expect(terminal).toHaveAttribute('data-expanded', 'false');
    expect(within(terminal).getByTestId('terminal-toggle')).toBeInTheDocument();

    // The status bar, with the branch the host's git reports.
    const statusBar = screen.getByTestId('status-bar');
    await waitFor(() => expect(statusBar).toHaveTextContent('main'));
    expect(within(statusBar).getByTestId('status-bar-workspace')).toHaveTextContent('widgets');
  });

  it('opens a file selected in the tree as an editor tab', async () => {
    mount(daemonHost());
    const sidebar = await screen.findByTestId('workspace-sidebar');
    const file = await within(sidebar).findByText('README.md');
    act(() => {
      fireEvent.click(file);
    });
    await waitFor(() => expect(hasTab('README.md')).toBe(true));
    expect(screen.getByTestId('shell-test-editor-pane').getAttribute('data-path')).toMatch(/README\.md$/);
  });

  it('opens a search result in the editor at its line', async () => {
    const gotoLines: number[] = [];
    const onGoto = (event: Event) => gotoLines.push((event as CustomEvent<{ line: number }>).detail.line);
    document.addEventListener('editor-goto-line', onGoto);
    try {
      mount(daemonHost());
      const sidebar = await screen.findByTestId('workspace-sidebar');
      act(() => {
        fireEvent.click(within(sidebar).getByRole('tab', { name: 'Search' }));
      });
      const view = within(sidebar).getByTestId('workspace-search');
      fireEvent.change(within(view).getByPlaceholderText('Search...'), { target: { value: 'needle' } });
      const hit = await within(view).findByText('12', {}, { timeout: 5000 });
      expect(requests().some((url) => url.startsWith('https://host.test/api/search?query=needle'))).toBe(true);
      act(() => {
        fireEvent.click(hit);
      });
      await waitFor(() => expect(hasTab('src/found.ts')).toBe(true));
      await waitFor(() => expect(gotoLines).toContain(12));
    } finally {
      document.removeEventListener('editor-goto-line', onGoto);
    }
  });

  it("shows the host's working tree in Source control, and a clear state without a repository", async () => {
    const { unmount } = mount(daemonHost());
    let sidebar = await screen.findByTestId('workspace-sidebar');
    act(() => {
      fireEvent.click(within(sidebar).getByRole('tab', { name: 'Source control' }));
    });
    expect(await within(sidebar).findByTitle('src/changed.ts')).toBeInTheDocument();
    unmount();

    inRepo = false;
    mount(daemonHost());
    sidebar = await screen.findByTestId('workspace-sidebar');
    act(() => {
      fireEvent.click(within(sidebar).getByRole('tab', { name: 'Source control' }));
    });
    expect(await within(sidebar).findByTestId('workspace-git-empty')).toHaveTextContent('No git repository');
  });

  it('expands and collapses the docked terminal from its strip, its collapse button and toggle_terminal', async () => {
    mount(daemonHost());
    const terminal = await screen.findByTestId('workspace-docked-terminal');
    expect(within(terminal).getByTestId('shell-test-workbench')).toHaveAttribute('data-pty', 'true');

    act(() => {
      fireEvent.click(within(terminal).getByTestId('terminal-toggle'));
    });
    expect(terminal).toHaveAttribute('data-expanded', 'true');
    expect(within(terminal).getByTestId('shell-test-workbench')).toHaveAttribute('data-active', 'true');

    act(() => {
      fireEvent.click(within(terminal).getByRole('button', { name: 'Collapse terminal' }));
    });
    expect(terminal).toHaveAttribute('data-expanded', 'false');

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_terminal' } }));
    });
    expect(terminal).toHaveAttribute('data-expanded', 'true');
  });

  it('docks the in-browser terminal out of sight until the sidebar Terminal entry opens it', async () => {
    mount(host({ git: false }, { kind: 'wasm', modelEndpoint: '' }));
    const terminal = await screen.findByTestId('workspace-docked-terminal');
    expect(terminal).toHaveAttribute('data-backend', 'wasm');
    expect(terminal).not.toBeVisible();

    const sidebar = screen.getByTestId('workspace-sidebar');
    act(() => {
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Back to project' }));
    });
    act(() => {
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Terminal' }));
    });
    expect(terminal).toHaveAttribute('data-expanded', 'true');
    expect(terminal).toBeVisible();
  });

  it('hides what has no backend: no Source control, terminal, preview toggle or their commands', async () => {
    mount(host({ git: false, localTerminal: false, preview: false }));
    const sidebar = await screen.findByTestId('workspace-sidebar');
    expect(
      within(sidebar)
        .getAllByRole('tab')
        .map((tab) => tab.getAttribute('aria-label')),
    ).toEqual(['Files', 'Search']);
    act(() => {
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Back to project' }));
    });
    expect(within(sidebar).queryByRole('button', { name: 'Source control' })).not.toBeInTheDocument();
    expect(within(sidebar).queryByRole('button', { name: 'Terminal' })).not.toBeInTheDocument();

    expect(screen.queryByTestId('workspace-docked-terminal')).not.toBeInTheDocument();
    expect(screen.queryByTestId('preview-panel-toggle')).not.toBeInTheDocument();
    expect(hasCommandHandler('toggle_terminal')).toBe(false);
    expect(hasCommandHandler('toggle_preview')).toBe(false);
    // The sidebar and editor commands the shell does handle are registered.
    expect(hasCommandHandler('open_search')).toBe(true);
    expect(hasCommandHandler('close_all_editors')).toBe(true);

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(requests().some((url) => url.includes('/api/git/'))).toBe(false);
  });

  it('collapses the sidebar to a rail and back', async () => {
    mount(daemonHost());
    const sidebar = await screen.findByTestId('workspace-sidebar');
    act(() => {
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Collapse sidebar' }));
    });
    expect(screen.queryByTestId('workspace-sidebar')).not.toBeInTheDocument();
    act(() => {
      fireEvent.click(
        within(screen.getByTestId('workspace-sidebar-rail')).getByRole('button', { name: 'Expand sidebar' }),
      );
    });
    expect(screen.getByTestId('workspace-sidebar')).toBeInTheDocument();
  });

  it("lists the design space's sections for the design space", async () => {
    mount(daemonHost(), 'design');
    const shell = await screen.findByTestId('workspace-space-shell');
    expect(shell).toHaveAttribute('data-lane', 'design');
    const sidebar = screen.getByTestId('workspace-sidebar');
    expect(
      within(sidebar)
        .getAllByRole('tab')
        .map((tab) => tab.getAttribute('aria-label')),
    ).toEqual(['Screens', 'Flows', 'Tokens', 'Feedback']);
  });
});
