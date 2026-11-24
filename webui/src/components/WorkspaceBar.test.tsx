// @ts-nocheck
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import SpacesBar from './WorkspaceBar';

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
describe('WorkspaceBar host-name early return', () => {
  it('renders nothing in a hosted build, even with workspace switching', () => {
    const host = makeTestHost({ projectFolderSwitching: true });
    host.transport = { ...host.transport, authMode: 'bearer' };
    act(() => {
      root.render(
        <HostProvider host={host}>
          <SpacesBar isConnected={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.workspace-bar')).toBeNull();
  });

  it('renders nothing on a shell with no workspace switching', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ projectFolderSwitching: false })}>
          <SpacesBar isConnected={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.workspace-bar')).toBeNull();
  });

  it('renders the local host name on a workspace-switching shell', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ projectFolderSwitching: true })}>
          <SpacesBar isConnected={false} />
        </HostProvider>,
      );
    });
    const bar = container.querySelector('.workspace-bar');
    expect(bar).not.toBeNull();
    expect(container.querySelector('.workspace-bar-host-name')?.textContent).toBe('Local');
  });
});
