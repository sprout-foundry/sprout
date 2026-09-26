import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../config/mode', () => ({ isCloud: true }));
vi.mock('./MenuBar', () => ({ default: () => null }));
vi.mock('./UserMenu', () => ({ UserMenu: () => null }));
vi.mock('./UsageChip', () => ({ UsageChip: () => null }));
vi.mock('./WorkspaceBar', () => ({ default: () => null }));
vi.mock('../services/activeRepo', () => ({ useActiveRepoURL: () => undefined }));

import { __resetFullWorkspaceForTests } from '../services/fullWorkspace';
import HeaderBar from './HeaderBar';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  __resetFullWorkspaceForTests();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

async function renderHeader(status: number, body: string) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(body, { status }))),
  );
  await act(async () => {
    root.render(
      <HeaderBar
        isMobile={false}
        isSidebarOpen
        isConnected
        onToggleSidebar={() => undefined}
        onToggleContextPanel={() => undefined}
      />,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

describe('HeaderBar Start Building', () => {
  it('is hidden when the deployment has no workspace compute', async () => {
    await renderHeader(503, '{"error":"workspaces are not available on this deployment"}');
    expect(container.querySelector('.start-building-btn')).toBeNull();
  });

  it('stays available through a transient 503', async () => {
    await renderHeader(503, '{"error":"upstream timeout"}');
    expect(container.querySelector('.start-building-btn')).not.toBeNull();
  });

  it('shows when workspaces are configured', async () => {
    await renderHeader(200, '[]');
    expect(container.querySelector('.start-building-btn')).not.toBeNull();
  });
});
