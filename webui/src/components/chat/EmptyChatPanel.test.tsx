/**
 * The empty chat's state, per agent backend.
 *
 * The "open a repository" panel is for a workspace with no checkout: the
 * in-browser agent's, whose files arrive by cloning `?repo=`. A daemon backend
 * already has the project's checkout, so the panel would offer to clone a
 * repository into a workspace that has one — it renders the ordinary welcome
 * state instead.
 */
import { render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { EmptyChatPanel } from './EmptyChatPanel';

vi.mock('../../services/activeRepo', () => ({ useActiveRepoURL: () => '' }));
vi.mock('../../services/homeView', () => ({ searchForRepo: (url: string) => `?repo=${url}` }));
vi.mock('../GitHubRepoPicker', () => ({ default: () => <div data-testid="repo-picker" /> }));
vi.mock('../layered/NewProjectDialog', () => ({ default: () => <div data-testid="new-project" /> }));

import { setActiveHost } from '../../host/accessor';
import { headlessHost } from '../../host/HostProvider';
import { HostProvider } from '../../host/HostProvider';
import type { SproutHost } from '../../host/types';

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
});

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
});
