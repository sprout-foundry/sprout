/**
 * SproutWorkspace — the arranged `git` and `search` views on an in-browser
 * (wasm) host. Git requests are answered by the browser's own git through
 * the in-browser adapter, search by the browser file store; neither reaches
 * the page's fetch.
 *
 * The adapter is a stub that routes as the real one does — `/api/git/*` to
 * the browser git handler, `/api/search` to the wasm-local handlers over a
 * fake shell — and browser git's operation entry point is mocked.
 */

import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const browserGit = vi.hoisted(() => ({ ops: [] as Array<{ op: string; body?: Record<string, unknown> }> }));

vi.mock('../services/browserGit', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/browserGit')>()),
  executeGitOp: async (op: string, body?: Record<string, unknown>) => {
    browserGit.ops.push({ op, body });
    switch (op) {
      case 'status':
        return {
          message: 'success',
          in_git_repo: true,
          status: {
            branch: 'main',
            ahead: 0,
            behind: 0,
            staged: [{ path: 'src/staged.ts', status: 'M' }],
            modified: [{ path: 'src/changed.ts', status: 'M' }],
            untracked: [],
            deleted: [],
            renamed: [],
          },
        };
      case 'branches':
        return { message: 'success', current: 'main', branches: ['main'] };
      case 'diff':
        return {
          message: 'success',
          path: 'src/changed.ts',
          has_staged: false,
          has_unstaged: true,
          staged_diff: '',
          unstaged_diff: '',
          diff: '@@ -1 +1 @@\n-old line\n+browser line\n',
        };
      default:
        return { message: 'success' };
    }
  },
}));

vi.mock('../services/cloudAdapter', async () => {
  const { handleBrowserGitRequest } = await import('../services/browserGitHandler');
  const { handleWasmLocal } = await import('../services/cloudWasmHandlers');
  const shell = {
    getCwd: () => '/workspace',
    executeCommand: (command: string) =>
      command.includes('needle')
        ? { exitCode: 0, stdout: '/workspace/src/found.ts:12:const needle = 1;\n', stderr: '' }
        : { exitCode: 1, stdout: '', stderr: '' },
  };
  return {
    CloudAdapter: class {
      name = 'cloud-test';
      requiresBackendHealthCheck = false;
      fileOpsViaAPI = false;
      showOnboarding = false;
      supportsSSH = false;
      supportsGit = true;
      supportsChat = true;
      supportsWorkspaceSwitching = false;
      supportsExport = false;
      supportsInstances = false;
      supportsLocalTerminal = false;
      supportsSettings = true;
      getWebSocketURL = () => '';
      getStartupRepo = () => undefined;
      fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const path = url.split('?')[0];
        const body = typeof init?.body === 'string' ? init.body : undefined;
        if (path.startsWith('/api/git/')) return handleBrowserGitRequest(path, init?.method ?? 'GET', url, body);
        if (path === '/api/search') {
          return handleWasmLocal(shell as never, path, 'GET', url, body);
        }
        return new Response(JSON.stringify({ providers: [], chat_sessions: [], active_chat_id: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      };
    },
  };
});
vi.mock('../services/cloudWasmHandlers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/cloudWasmHandlers')>()),
  setAgentEventDispatcher: vi.fn(),
}));
vi.mock('../components/ChatView', () => ({ default: () => <div /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return <div data-testid="wasm-git-editor-pane" data-path={buffer?.file.path} />;
    },
  };
});

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { WebSocketService } from '../services/websocket';
import { SproutWorkspace } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const SOURCE_CONTROL: ViewsArrangement = { left: ['git'], center: ['editor'], right: ['search'], overlay: [] };

/** A socket that never opens: the host serves no events stream. */
class SilentWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  readyState = 0;
  onopen: unknown = null;
  onclose: unknown = null;
  onerror: unknown = null;
  onmessage: unknown = null;
  send = vi.fn();
  close = vi.fn();
}

function wasmHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    capabilities: { ...base.capabilities, git: true, settings: true },
    transport: { apiBaseURL: '', wsURL: '', authMode: 'none', agent: { kind: 'wasm', modelEndpoint: '' } },
  };
}

const pageFetch = vi.fn(
  async (_input: unknown, _init?: RequestInit) =>
    new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } }),
);

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  browserGit.ops = [];
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  setActiveHost(headlessHost());
  vi.stubGlobal('WebSocket', SilentWebSocket);
  vi.stubGlobal('fetch', pageFetch);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  WebSocketService.getInstance().setLocalAgentState('none');
});

const pageCalls = (fragment: string) => pageFetch.mock.calls.filter(([input]) => String(input).includes(fragment));

describe('SproutWorkspace source control and search views (wasm host)', () => {
  it('shows status from browser git, commits through it, and opens a changed file in the editor', async () => {
    render(
      <SproutWorkspace
        project={{ id: 'acme/widgets', root: '' }}
        space="code"
        host={wasmHost()}
        layout={SOURCE_CONTROL}
      />,
    );
    const view = await screen.findByTestId('workspace-git');
    await within(view).findByTitle('src/changed.ts', {}, { timeout: 3000 });
    expect(browserGit.ops.some(({ op }) => op === 'status')).toBe(true);

    // Browser git cannot unstage: the action is offered disabled.
    const stagedRow = within(view).getByTitle('src/staged.ts');
    expect(within(stagedRow).getByTitle('Not available in browser mode')).toBeDisabled();
    expect(within(view).queryByRole('tab', { name: /Worktrees/ })).not.toBeInTheDocument();

    fireEvent.change(within(view).getByLabelText('Commit message'), { target: { value: 'From the browser' } });
    await act(async () => {
      fireEvent.click(within(view).getByRole('button', { name: 'Commit Changes' }));
    });
    await waitFor(() =>
      expect(browserGit.ops).toContainEqual(
        expect.objectContaining({ op: 'commit', body: expect.objectContaining({ message: 'From the browser' }) }),
      ),
    );

    // The status reload after the commit re-renders the rows.
    const row = await within(view).findByTitle('src/changed.ts');
    await act(async () => {
      fireEvent.click(row);
    });
    const diff = await within(view).findByTestId('workspace-git-diff');
    expect(diff).toHaveTextContent('browser line');
    act(() => {
      fireEvent.click(within(diff).getByTestId('workspace-git-diff-open'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('wasm-git-editor-pane')).toHaveAttribute('data-path', 'src/changed.ts'),
    );
    expect(pageCalls('/api/git/commit')).toEqual([]);
  });

  it('searches the browser file store and opens a result in the editor at the matched line', async () => {
    const gotoLines: number[] = [];
    const onGoto = (event: Event) => gotoLines.push((event as CustomEvent<{ line: number }>).detail.line);
    document.addEventListener('editor-goto-line', onGoto);
    try {
      render(
        <SproutWorkspace
          project={{ id: 'acme/widgets', root: '' }}
          space="code"
          host={wasmHost()}
          layout={SOURCE_CONTROL}
        />,
      );
      // The search runs once the in-browser adapter is in place.
      await waitFor(() => expect(browserGit.ops.length).toBeGreaterThan(0), { timeout: 3000 });
      const view = await screen.findByTestId('workspace-search');
      fireEvent.change(within(view).getByPlaceholderText('Search...'), { target: { value: 'needle' } });

      const hit = await within(view).findByText('12', {}, { timeout: 2000 });
      expect(within(view).getByText('found.ts', { exact: false })).toBeInTheDocument();
      act(() => {
        fireEvent.click(hit);
      });
      // The editor holds the file under the browser workspace root.
      await waitFor(() =>
        expect(screen.getByTestId('wasm-git-editor-pane').getAttribute('data-path')).toMatch(/(^|\/)src\/found\.ts$/),
      );
      await waitFor(() => expect(gotoLines).toContain(12));
      expect(pageCalls('/api/search')).toEqual([]);
    } finally {
      document.removeEventListener('editor-goto-line', onGoto);
    }
  });
});
