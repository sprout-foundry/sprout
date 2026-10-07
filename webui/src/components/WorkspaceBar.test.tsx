// @ts-nocheck
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import WorkspaceBar from './WorkspaceBar';

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({ getWorkspace: vi.fn().mockRejectedValue(new Error('not called in these tests')) }),
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

// The bar names the workspace host. A shell with no workspace switching has no
// host to name unless it is attached to a remote one, so the early return is
// keyed on workspaceSwitching. isConnected: false keeps the fetch path out of
// scope for this early-return flip.
describe('WorkspaceBar host-name early return', () => {
  it('renders nothing on a shell with no workspace switching', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ workspaceSwitching: false })}>
          <WorkspaceBar isConnected={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.workspace-bar')).toBeNull();
  });

  it('renders the local host name on a workspace-switching shell', () => {
    act(() => {
      root.render(
        <HostProvider host={makeTestHost({ workspaceSwitching: true })}>
          <WorkspaceBar isConnected={false} />
        </HostProvider>,
      );
    });
    const bar = container.querySelector('.workspace-bar');
    expect(bar).not.toBeNull();
    expect(container.querySelector('.workspace-bar-host-name')?.textContent).toBe('Local');
  });
});
