// @ts-nocheck
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import ProjectFolderBar from './ProjectFolderBar';

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({ getProjectFolder: vi.fn().mockRejectedValue(new Error('not called in these tests')) }),
  },
}));
vi.mock('../services/clientSession', () => ({ getSSHProxyContext: () => null }));

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

// The bar names the project folder host. A shell with no project folder switching has no
// host to name unless it is attached to a remote one, so the early return is
// keyed on projectFolderSwitching. isConnected: false keeps the fetch path out of
// scope for this early-return flip.
describe('ProjectFolderBar host-name early return', () => {
  it('renders nothing in a hosted build, even with workspace switching', () => {
    const host = makeTestHost({ projectFolderSwitching: true });
    host.transport = { ...host.transport, authMode: 'bearer' };
    act(() => {
      root.render(
        <HostProvider host={host}>
          <ProjectFolderBar isConnected={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.project-folder-bar')).toBeNull();
  });

  it('renders nothing on a shell with no workspace switching', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ projectFolderSwitching: false })}>
          <ProjectFolderBar isConnected={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.project-folder-bar')).toBeNull();
  });

  it('renders the local host name on a project-folder-switching shell', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ projectFolderSwitching: true })}>
          <ProjectFolderBar isConnected={false} />
        </HostProvider>,
      );
    });
    const bar = container.querySelector('.project-folder-bar');
    expect(bar).not.toBeNull();
    expect(container.querySelector('.project-folder-bar-host-name')?.textContent).toBe('Local');
  });
});
