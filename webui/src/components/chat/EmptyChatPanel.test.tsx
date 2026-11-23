/**
 * The empty chat's state, per agent backend.
 *
 * The "open a repository" panel is for a project folder with no checkout: the
 * in-browser agent's, whose files arrive by cloning `?repo=`. A daemon backend
 * already has the project's checkout, so the panel would offer to clone a
 * repository into a project folder that has one — it renders the ordinary welcome
 * state instead.
 */
import { act, fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EmptyChatPanel } from './EmptyChatPanel';

vi.mock('../../services/activeRepo', () => ({ useActiveRepoURL: () => '' }));
vi.mock('../../services/homeView', () => ({ searchForRepo: (url: string) => `?repo=${url}` }));
vi.mock('../GitHubRepoPicker', () => ({ default: () => <div data-testid="repo-picker" /> }));
vi.mock('../layered/NewProjectDialog', () => ({ default: () => <div data-testid="new-project" /> }));

import { setActiveHost } from '../../host/accessor';
import { headlessHost } from '../../host/HostProvider';
import { HostProvider } from '../../host/HostProvider';
import type { SproutHost } from '../../host/types';
import {
  __resetStartupRepoImportForTests,
  claimHostProjectRepo,
  importStartupRepo,
} from '../../services/startupRepoImport';

function hostWith(transport: Partial<SproutHost['transport']>): SproutHost {
  const base = headlessHost();
  return { ...base, transport: { ...base.transport, ...transport } };
}

const LOCAL = hostWith({ authMode: 'none' });
const PLATFORM_WASM = hostWith({ authMode: 'bearer', agent: { kind: 'wasm' } });
const PLATFORM_DAEMON = hostWith({
  authMode: 'bearer',
  apiBaseURL: 'https://platform.test',
  agent: { kind: 'daemon', apiBaseURL: '/workspace/ws-1/proxy' },
});

function renderWithHost(host: SproutHost, children: ReactNode) {
  setActiveHost(host);
  return render(<HostProvider host={host}>{children}</HostProvider>);
}

afterEach(() => {
  setActiveHost(headlessHost());
  __resetStartupRepoImportForTests();
});

const PROJECT_REPO = 'https://git.example.test/acme/app';

describe('EmptyChatPanel', () => {
  it('offers to open a repository when the in-browser agent has none', () => {
    renderWithHost(PLATFORM_WASM, <EmptyChatPanel />);
    expect(screen.getByTestId('chat-open-repo')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-welcome')).not.toBeInTheDocument();
  });

  it('shows the ordinary welcome state with a daemon backend, which has a checkout', () => {
    renderWithHost(PLATFORM_DAEMON, <EmptyChatPanel />);
    expect(screen.queryByTestId('chat-open-repo')).not.toBeInTheDocument();
    expect(screen.getByTestId('chat-welcome')).toBeInTheDocument();
  });

  it('shows the ordinary welcome state on a local host', () => {
    renderWithHost(LOCAL, <EmptyChatPanel />);
    expect(screen.queryByTestId('chat-open-repo')).not.toBeInTheDocument();
    expect(screen.getByTestId('chat-welcome')).toBeInTheDocument();
  });

  it('says the project repository is opening, not offering another one', () => {
    claimHostProjectRepo(PROJECT_REPO);
    renderWithHost(PLATFORM_WASM, <EmptyChatPanel />);
    expect(screen.getByTestId('chat-project-repo').dataset.state).toBe('opening');
    expect(screen.queryByTestId('chat-open-repo')).not.toBeInTheDocument();
  });

  it('says why the project repository failed to open, with a way to fix the git connection and to retry', async () => {
    const open = vi.fn();
    const host: SproutHost = {
      ...PLATFORM_WASM,
      navigation: { ...PLATFORM_WASM.navigation, open, intentPath: () => '/account/git' },
    };
    const restoreRepo = vi
      .fn()
      .mockResolvedValueOnce({ success: false, error: 'git proxy: HTTP 401 Unauthorized' })
      .mockResolvedValueOnce({ success: true });
    claimHostProjectRepo(PROJECT_REPO);
    renderWithHost(host, <EmptyChatPanel />);
    await act(async () => {
      await importStartupRepo({ restoreRepo }, PROJECT_REPO, 'host');
    });

    expect(screen.getByTestId('chat-project-repo').dataset.state).toBe('failed');
    expect(screen.getByText(/isn't connected for your account/)).toBeInTheDocument();
    expect(screen.queryByTestId('chat-open-repo')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('project-repo-connect'));
    expect(open).toHaveBeenCalledWith({ type: 'gitConnections' });

    await act(async () => {
      fireEvent.click(screen.getByTestId('project-repo-retry'));
      await Promise.resolve();
    });
    expect(restoreRepo).toHaveBeenCalledTimes(2);
  });

  it('shows any other open failure as it is', async () => {
    claimHostProjectRepo(PROJECT_REPO);
    renderWithHost(PLATFORM_WASM, <EmptyChatPanel />);
    await act(async () => {
      await importStartupRepo(
        { restoreRepo: async () => ({ success: false, error: 'repository not found' }) },
        PROJECT_REPO,
        'host',
      );
    });
    expect(screen.getByText('repository not found')).toBeInTheDocument();
    expect(screen.queryByTestId('project-repo-connect')).not.toBeInTheDocument();
  });
});
