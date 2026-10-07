import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const openHome = vi.fn();
vi.mock('../../services/homeView', () => ({ openHome: (p: string) => openHome(p) }));

import { HostProvider } from '../../host/HostProvider';
import { CreateRepoError } from '../../host/platformGitHub';
import type { SproutHost } from '../../host/types';
import NewProjectDialog, { toRepoName } from './NewProjectDialog';

let container: HTMLDivElement;
let root: Root;

/** The GitHub surface the host supplies (account-managed GitHub). */
const github = {
  connected: true,
  createRepo: vi.fn(),
};
let host: SproutHost;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  openHome.mockReset();
  github.connected = true;
  github.createRepo.mockReset();
  host = {
    transport: { apiBaseURL: '', wsURL: '', authMode: 'bearer' },
    navigation: { open: () => undefined },
    notifications: { post: () => undefined },
    capabilities: {
      ssh: false,
      git: true,
      chat: true,
      workspaceSwitching: false,
      folderPicker: false,
      export: false,
      instances: true,
      localTerminal: false,
      settings: true,
      automations: false,
      agentChanges: false,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
    },
    github: {
      isConnected: () => Promise.resolve(github.connected),
      listRepos: () => Promise.resolve([]),
      createRepo: (opts) => github.createRepo(opts),
    },
  };
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

async function render(onCreated = vi.fn(), onClose = vi.fn()) {
  await act(async () => {
    root.render(
      <HostProvider host={host}>
        <NewProjectDialog onCreated={onCreated} onClose={onClose} />
      </HostProvider>,
    );
  });
  return { onCreated, onClose };
}

function typeName(value: string) {
  const input = container.querySelector<HTMLInputElement>('input[aria-label="Repository name"]')!;
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

async function submit() {
  await act(async () => {
    container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
}

describe('toRepoName', () => {
  it('keeps what GitHub keeps', () => {
    expect(toRepoName('My App!')).toBe('My-App');
    expect(toRepoName('api_v2.0')).toBe('api_v2.0');
  });
});

describe('NewProjectDialog', () => {
  it('creates a private repository and hands back its URL', async () => {
    github.createRepo.mockResolvedValue('https://github.com/ada/new-app');
    const { onCreated } = await render();
    typeName('new app');
    await submit();

    expect(github.createRepo).toHaveBeenCalledWith({ name: 'new-app', private: true });
    expect(onCreated).toHaveBeenCalledWith('https://github.com/ada/new-app');
  });

  it('shows why a name was refused', async () => {
    github.createRepo.mockRejectedValue(new CreateRepoError('name already exists on this account', 'name_unavailable'));
    const { onCreated } = await render();
    typeName('taken');
    await submit();

    expect(container.querySelector('[role="alert"]')?.textContent).toBe('name already exists on this account');
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('offers to connect GitHub when the account has none', async () => {
    github.connected = false;
    const { onClose } = await render();

    const connect = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Connect GitHub')!;
    act(() => connect.click());
    expect(openHome).toHaveBeenCalledWith('/settings');
    expect(onClose).toHaveBeenCalled();
  });
});
