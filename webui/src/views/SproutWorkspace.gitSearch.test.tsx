/**
 * SproutWorkspace — the arranged `git` (source control) and `search` views
 * against a daemon host: git and search requests go to the host transport,
 * a changed file and a search result open in the editor unit, and features
 * that need server-side git follow `serverGit`.
 *
 * The provider stack, the editor unit, the git section and the search panel
 * are the real ones; the editor pane and the chat are stubbed, and the page
 * fetch stands in for the daemon.
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
vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="git-test-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return <div data-testid="git-test-editor-pane" data-path={buffer?.file.path} />;
    },
  };
});

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { onWorkspaceResponse } from '../services/workspaceResponses';
import { SproutWorkspace } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const PROJECT = { id: 'acme/widgets', repoUrl: 'https://github.com/acme/widgets' };
const SOURCE_CONTROL: ViewsArrangement = { left: ['git'], center: ['editor'], right: ['search'], overlay: [] };

function daemonHost(capabilities: Partial<SproutHost['capabilities']> = {}): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    capabilities: { ...base.capabilities, git: true, ...capabilities },
    transport: { apiBaseURL: 'https://host.test', wsURL: '', authMode: 'none' },
  };
}

const REPO_STATUS = {
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
let pushFails = false;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

const fetchSpy = vi.fn(async (input: unknown, _init?: RequestInit) => {
  const url = String(input);
  if (url.includes('/api/git/status')) {
    return json(inRepo ? REPO_STATUS : { message: 'success', in_git_repo: false });
  }
  if (url.includes('/api/git/branches')) return json({ message: 'success', current: 'main', branches: ['main'] });
  if (url.includes('/api/git/diff')) {
    return json({
      message: 'success',
      path: 'src/changed.ts',
      has_staged: false,
      has_unstaged: true,
      staged_diff: '',
      unstaged_diff: '',
      diff: '@@ -1 +1 @@\n-old line\n+new line\n',
    });
  }
  if (url.includes('/api/git/push')) {
    return pushFails ? json({ message: 'push rejected' }, 500) : json({ message: 'success' });
  }
  if (url.includes('/api/git/')) return json({ message: 'success' });
  if (url.includes('/api/search')) return json(SEARCH_RESULT);
  return json({});
});

const requests = () => fetchSpy.mock.calls.map(([url, init]) => ({ url: String(url), init }));

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  inRepo = true;
  pushFails = false;
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  setActiveHost(headlessHost());
  vi.stubGlobal('fetch', fetchSpy);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe('SproutWorkspace source control view (daemon host)', () => {
  it('shows the working tree status from the host and commits through it', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />);
    const view = await screen.findByTestId('workspace-git');
    await within(view).findByTitle('src/changed.ts');
    expect(within(view).getByTitle('src/staged.ts')).toBeInTheDocument();
    expect(requests().some(({ url }) => url.startsWith('https://host.test/api/git/status'))).toBe(true);

    fireEvent.change(within(view).getByLabelText('Commit message'), { target: { value: 'Ship it' } });
    await act(async () => {
      fireEvent.click(within(view).getByRole('button', { name: 'Commit Changes' }));
    });

    const commitRequest = () => requests().find(({ url }) => url === 'https://host.test/api/git/commit');
    await waitFor(() => expect(commitRequest()).toBeDefined());
    expect(JSON.parse(String(commitRequest()?.init?.body))).toMatchObject({
      message: 'Ship it',
      files: ['src/staged.ts'],
    });
  });

  it("shows a changed file's diff and opens the file in the editor unit", async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />);
    const view = await screen.findByTestId('workspace-git');
    const row = await within(view).findByTitle('src/changed.ts');

    await act(async () => {
      fireEvent.click(row);
    });
    const diff = await within(view).findByTestId('workspace-git-diff');
    expect(diff).toHaveTextContent('new line');
    expect(requests().some(({ url }) => url.startsWith('https://host.test/api/git/diff?path=src%2Fchanged.ts'))).toBe(
      true,
    );

    act(() => {
      fireEvent.click(within(diff).getByTestId('workspace-git-diff-open'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('git-test-editor-pane')).toHaveAttribute('data-path', 'src/changed.ts'),
    );
  });

  it('offers worktrees only when the host declares serverGit', async () => {
    const { unmount } = render(
      <SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />,
    );
    let view = await screen.findByTestId('workspace-git');
    await within(view).findByTitle('src/changed.ts');
    expect(within(view).getByRole('tab', { name: /History/ })).toBeInTheDocument();
    expect(within(view).queryByRole('tab', { name: /Worktrees/ })).not.toBeInTheDocument();
    unmount();

    render(
      <SproutWorkspace project={PROJECT} space="code" host={daemonHost({ serverGit: true })} layout={SOURCE_CONTROL} />,
    );
    view = await screen.findByTestId('workspace-git');
    await within(view).findByTitle('src/changed.ts');
    expect(within(view).getByRole('tab', { name: /Worktrees/ })).toBeInTheDocument();
  });

  it('publishes a failed push on the workspace request channel the escalation triggers watch', async () => {
    pushFails = true;
    const seen: Array<{ url: string; ok: boolean }> = [];
    const stop = onWorkspaceResponse((url, response) => seen.push({ url, ok: response.ok }));
    try {
      render(<SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />);
      const view = await screen.findByTestId('workspace-git');
      await within(view).findByTitle('src/changed.ts');
      await act(async () => {
        fireEvent.click(within(view).getByTitle(/^Push/));
      });
      await waitFor(() => expect(seen).toContainEqual({ url: '/api/git/push', ok: false }));
      expect(await within(view).findByText('push rejected')).toBeInTheDocument();
    } finally {
      stop();
    }
  });

  it('shows the empty state for a workspace that is not a repository', async () => {
    inRepo = false;
    render(<SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />);
    const empty = await screen.findByTestId('workspace-git-empty');
    expect(empty).toHaveTextContent('No git repository');
  });

  it('shows the empty state and makes no git requests for a host without git', async () => {
    render(
      <SproutWorkspace project={PROJECT} space="code" host={daemonHost({ git: false })} layout={SOURCE_CONTROL} />,
    );
    const empty = await screen.findByTestId('workspace-git-empty');
    expect(empty).toHaveTextContent("Source control isn't available");
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(requests().some(({ url }) => url.includes('/api/git/'))).toBe(false);
  });

  it('renders a host-driven panel from viewProps.git without loading git state itself', async () => {
    const hostPanel = {
      gitStatus: { ...REPO_STATUS.status, clean: false, truncated: false, branch: 'host-branch' },
      gitBranches: { current: 'host-branch', branches: ['host-branch'] },
      selectedFiles: new Set<string>(),
      activeDiffSelectionKey: null,
      commitMessage: '',
      isLoading: false,
      isActing: false,
      isGeneratingCommitMessage: false,
      isReviewLoading: false,
      actionError: null,
      actionWarning: null,
    };
    const callbacks = new Proxy({}, { get: () => vi.fn() });
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={daemonHost()}
        layout={SOURCE_CONTROL}
        viewProps={{ git: { gitPanel: { ...callbacks, ...hostPanel } } }}
      />,
    );
    const view = await screen.findByTestId('workspace-git');
    expect(within(view).getByTitle('src/changed.ts')).toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(requests().some(({ url }) => url.includes('/api/git/status'))).toBe(false);
  });
});

describe('SproutWorkspace search view (daemon host)', () => {
  it('searches through the host and opens a result in the editor at the matched line', async () => {
    const gotoLines: number[] = [];
    const onGoto = (event: Event) => gotoLines.push((event as CustomEvent<{ line: number }>).detail.line);
    document.addEventListener('editor-goto-line', onGoto);
    try {
      render(<SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={SOURCE_CONTROL} />);
      const view = await screen.findByTestId('workspace-search');
      fireEvent.change(within(view).getByPlaceholderText('Search...'), { target: { value: 'needle' } });

      const hit = await within(view).findByText('12', {}, { timeout: 2000 });
      expect(requests().some(({ url }) => url.startsWith('https://host.test/api/search?query=needle'))).toBe(true);

      act(() => {
        fireEvent.click(hit);
      });
      await waitFor(() =>
        expect(screen.getByTestId('git-test-editor-pane')).toHaveAttribute('data-path', 'src/found.ts'),
      );
      await waitFor(() => expect(gotoLines).toContain(12));
    } finally {
      document.removeEventListener('editor-goto-line', onGoto);
    }
  });

  it('lets the host replace the search view props', async () => {
    const onFileClick = vi.fn();
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={daemonHost()}
        layout={SOURCE_CONTROL}
        viewProps={{ search: { onFileClick } }}
      />,
    );
    const view = await screen.findByTestId('workspace-search');
    fireEvent.change(within(view).getByPlaceholderText('Search...'), { target: { value: 'needle' } });
    const hit = await within(view).findByText('12', {}, { timeout: 2000 });
    act(() => {
      fireEvent.click(hit);
    });
    expect(onFileClick).toHaveBeenCalledWith('src/found.ts', 12);
  });
});
