/**
 * SproutWorkspace — the files and editor units of an arranged workspace.
 *
 * An own-mode workspace with a `layout` assembles a working `fileTree` (the
 * workspace file API), an `editor` rendered from the editor unit, and a
 * `changes` panel whose file clicks open the editor; a kind the host supplies
 * through `viewProps` wins. The tree, the bare editor primitive, the editor
 * pane and the composite views are stubbed with nodes that expose the props
 * the composition handed them; the provider stack and the editor unit are
 * the real ones.
 */

import { act, render, screen, waitFor, within } from '@testing-library/react';
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
vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="files-test-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({
  default: (props: { onFileClick?: (path: string) => void }) => (
    <button type="button" data-testid="files-test-changes" onClick={() => props.onFileClick?.('src/changed.ts')}>
      changes
    </button>
  ),
}));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../services/api', () => {
  const instance = {
    getHotkeys: async () => ({ hotkeys: [], platform: 'test' }),
    applyHotkeyPreset: async () => ({ hotkeys: [] }),
    getWorkspace: async () => ({ workspace_root: '' }),
    getStats: async () => ({}),
    getSettings: async () => ({}),
  };
  return { ApiService: { getInstance: () => instance } };
});
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return <div data-testid="files-test-editor-pane" data-path={buffer?.file.path} />;
    },
  };
});
vi.mock('@sprout/ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@sprout/ui')>();
  return {
    ...actual,
    FileTree: (props: {
      onFileSelect: (file: { path: string; name: string; isDir: boolean }) => void;
      selectedFile?: string;
      onFetchFiles?: (path: string) => Promise<unknown>;
    }) => (
      <div
        data-testid="files-test-tree"
        data-selected={props.selectedFile ?? ''}
        data-has-fetch={String(typeof props.onFetchFiles === 'function')}
      >
        <button
          type="button"
          data-testid="files-test-tree-select"
          onClick={() => props.onFileSelect({ path: 'src/app.ts', name: 'app.ts', isDir: false })}
        >
          select
        </button>
        <button type="button" data-testid="files-test-tree-fetch" onClick={() => void props.onFetchFiles?.('.')}>
          fetch
        </button>
      </div>
    ),
    Editor: (props: { value?: string }) => <div data-testid="files-test-primitive-editor" data-value={props.value} />,
  };
});

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const PROJECT: SproutProject = { id: 'acme/widgets', repoUrl: 'https://github.com/acme/widgets' };
const IDE: ViewsArrangement = { left: ['fileTree'], center: ['editor', 'chat'], right: ['changes'], overlay: [] };

function makeHost(): SproutHost {
  return {
    ...headlessHost(),
    transport: { apiBaseURL: 'https://host.test', wsURL: '', authMode: 'none' },
  };
}

const fetchSpy = vi.fn(async (_url: unknown, _init?: RequestInit) => ({
  ok: true,
  status: 200,
  statusText: 'OK',
  headers: new Headers({ 'content-type': 'application/json' }),
  json: async () => ({ message: 'success', files: [] }),
  text: async () => '',
}));

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  setActiveHost(headlessHost());
  vi.stubGlobal('fetch', fetchSpy);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('SproutWorkspace files and editor', () => {
  it('assembles a working file tree against the host transport', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={IDE} />);
    const tree = await screen.findByTestId('files-test-tree');
    expect(tree).toHaveAttribute('data-has-fetch', 'true');

    await act(async () => {
      screen.getByTestId('files-test-tree-fetch').click();
    });
    const urls = fetchSpy.mock.calls.map(([url]) => String(url));
    expect(urls.some((url) => url.includes('https://host.test') && url.includes('/api/files?path=.'))).toBe(true);
  });

  it('renders the assembled editor for an arranged editor, and a file picked in the tree opens in it', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={IDE} />);
    const workspace = await screen.findByTestId('sprout-workspace');
    expect(within(workspace).getByTestId('workspace-editor')).toBeInTheDocument();
    expect(within(workspace).queryByTestId('files-test-primitive-editor')).not.toBeInTheDocument();

    act(() => {
      screen.getByTestId('files-test-tree-select').click();
    });

    await waitFor(() =>
      expect(screen.getByTestId('files-test-editor-pane')).toHaveAttribute('data-path', 'src/app.ts'),
    );
    expect(screen.getByTestId('files-test-tree')).toHaveAttribute('data-selected', 'src/app.ts');
  });

  it('opens a changed file in the editor from the changes panel and from an open-in-editor request', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={IDE} />);
    await screen.findByTestId('sprout-workspace');

    act(() => {
      screen.getByTestId('files-test-changes').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('files-test-editor-pane')).toHaveAttribute('data-path', 'src/changed.ts'),
    );

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: 'README.md' } }));
    });
    await waitFor(() => expect(screen.getByTestId('files-test-editor-pane')).toHaveAttribute('data-path', 'README.md'));
  });

  it('lets the host replace the tree and the editor through viewProps', async () => {
    const onFileSelect = vi.fn();
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        layout={IDE}
        viewProps={{ fileTree: { onFileSelect, selectedFile: 'host.txt' }, editor: { value: 'host buffer' } }}
      />,
    );
    const workspace = await screen.findByTestId('sprout-workspace');

    const tree = within(workspace).getByTestId('files-test-tree');
    expect(tree).toHaveAttribute('data-selected', 'host.txt');
    expect(tree).toHaveAttribute('data-has-fetch', 'false');
    act(() => {
      screen.getByTestId('files-test-tree-select').click();
    });
    expect(onFileSelect).toHaveBeenCalledWith(expect.objectContaining({ path: 'src/app.ts' }));

    expect(within(workspace).getByTestId('files-test-primitive-editor')).toHaveAttribute('data-value', 'host buffer');
    expect(within(workspace).queryByTestId('workspace-editor')).not.toBeInTheDocument();
  });
});
