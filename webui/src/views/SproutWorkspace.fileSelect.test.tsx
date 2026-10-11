/**
 * SproutWorkspace — selecting a file in the assembled tree, mounted the way
 * the package mounts it.
 *
 * Own mode with only the stack `SproutWorkspace` builds itself: no provider
 * around it, the real `FileTree`, the real editor view and pane, and a fetch
 * that answers the workspace file API. Only the chat view (not under test)
 * and the events transport (no socket in jsdom) are stubbed.
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
vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="file-select-chat" /> }));

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const PROJECT: SproutProject = { id: 'acme/widgets', repoUrl: 'https://github.com/acme/widgets' };
/** The arrangement from the integration report: no `editor` placed. */
const NO_EDITOR: ViewsArrangement = { left: ['fileTree'], center: ['chat'], right: ['changes'] };
const WITH_EDITOR: ViewsArrangement = { left: ['fileTree'], center: ['editor', 'chat'], right: ['changes'] };

const README = '# widgets readme\n';

let listing: Array<{ name: string; path: string; isDir: boolean; size?: number }>;

function respond(status: number, body: string, type: string): Response {
  return new Response(body, { status, headers: { 'content-type': type } });
}

const fetchSpy = vi.fn(async (input: RequestInfo | URL) => {
  const url = new URL(String(input), 'http://host.test');
  if (url.pathname === '/api/files') {
    return respond(200, JSON.stringify({ message: 'success', files: listing }), 'application/json');
  }
  if (url.pathname === '/api/file' && url.searchParams.get('path') === 'README.md') {
    return respond(200, README, 'text/plain');
  }
  return respond(404, '{}', 'application/json');
});

function requested(path: string): string[] {
  return fetchSpy.mock.calls
    .map(([input]) => new URL(String(input), 'http://host.test'))
    .filter((url) => url.pathname === path)
    .map((url) => `${url.pathname}${url.search}`);
}

function makeHost(): SproutHost {
  return { ...headlessHost(), transport: { apiBaseURL: '', wsURL: '', authMode: 'none' } };
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  setActiveHost(headlessHost());
  listing = [{ name: 'README.md', path: 'README.md', isDir: false, size: README.length }];
  vi.stubGlobal('fetch', fetchSpy);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

async function clickTreeRow(name: string): Promise<void> {
  const row = await screen.findByText(name);
  await act(async () => {
    fireEvent.click(row);
  });
}

describe('SproutWorkspace tree selection, as the package mounts it', () => {
  it('opens a selected file in the arranged editor: the file is read and gets a tab', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={WITH_EDITOR} />);
    await clickTreeRow('README.md');

    await waitFor(() => expect(requested('/api/file')).toContain('/api/file?path=README.md'));
    const editor = screen.getByTestId('workspace-editor');
    await waitFor(() => expect(within(editor).getByRole('tab', { name: /README\.md/ })).toBeInTheDocument());
    await waitFor(() => expect(editor.querySelector('.cm-content')?.textContent).toContain('# widgets readme'));
  });

  it('with no editor arranged, shows the editor in the center slot while a file is open', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={NO_EDITOR} />);
    await screen.findByText('README.md');
    expect(screen.queryByTestId('workspace-editor')).not.toBeInTheDocument();

    await clickTreeRow('README.md');

    await waitFor(() => expect(requested('/api/file')).toContain('/api/file?path=README.md'));
    const center = screen.getByTestId('views-slot-center');
    const editor = within(center).getByTestId('workspace-editor');
    // The editor joins the host's center views; it does not replace them.
    expect(within(center).getByTestId('file-select-chat')).toBeInTheDocument();
    await waitFor(() => expect(editor.querySelector('.cm-content')?.textContent).toContain('# widgets readme'));

    // Closing the last file gives the center slot back to the host's views.
    await act(async () => {
      fireEvent.click(within(editor).getByRole('button', { name: 'Close README.md' }));
    });
    await waitFor(() => expect(screen.queryByTestId('workspace-editor')).not.toBeInTheDocument());
    expect(within(center).getByTestId('file-select-chat')).toBeInTheDocument();
  });

  it('lists again when an agent adapter is installed and when the startup repo import completes', async () => {
    listing = [];
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={NO_EDITOR} />);
    await waitFor(() => expect(requested('/api/files')).toHaveLength(1));
    expect(screen.queryByText('README.md')).not.toBeInTheDocument();

    // The adapter arrives after the first listing; the repository is not in yet.
    act(() => {
      window.dispatchEvent(new Event(ADAPTER_INSTALLED_EVENT));
    });
    await waitFor(() => expect(requested('/api/files')).toHaveLength(2));

    listing = [{ name: 'README.md', path: 'README.md', isDir: false, size: README.length }];
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:repo-imported', { detail: { repo: PROJECT.repoUrl } }));
    });
    expect(await screen.findByText('README.md')).toBeInTheDocument();
  });
});
