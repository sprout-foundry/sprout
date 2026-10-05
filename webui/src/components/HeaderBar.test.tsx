import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../config/mode', () => ({ isCloud: true }));
vi.mock('./MenuBar', () => ({ default: () => null }));
vi.mock('./UserMenu', () => ({ UserMenu: () => null }));
vi.mock('./UsageChip', () => ({ UsageChip: () => null }));
vi.mock('./WorkspaceBar', () => ({ default: () => null }));
let activeRepo: string | undefined;
vi.mock('../services/activeRepo', () => ({ useActiveRepoURL: () => activeRepo }));

import { __resetFullWorkspaceForTests } from '../services/fullWorkspace';
import HeaderBar from './HeaderBar';

// These cover the classic layout, still available as ?layout=classic.
vi.mock('../config/layout', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../config/layout')>()),
  shellLayout: 'classic',
  isLayeredLayout: false,
}));

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  activeRepo = undefined;
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

describe('HeaderBar back-link', () => {
  it('returns to the dashboard when no repo is open', async () => {
    await renderHeader(503, '');
    const link = container.querySelector<HTMLAnchorElement>('.header-back-to-dashboard');
    expect(link?.getAttribute('href')).toBe('/?from=editor');
    expect(link?.textContent).toContain('Dashboard');
  });

  it("returns to the open repo's hub page", async () => {
    activeRepo = 'https://github.com/acme/widgets';
    await renderHeader(503, '');
    const link = container.querySelector<HTMLAnchorElement>('.header-back-to-dashboard');
    expect(link?.getAttribute('href')).toBe('/?from=editor#/repos/acme/widgets');
    expect(link?.textContent).toContain('acme/widgets');
  });
});

// SP-155 §155a (item 155.6): the Code-mode preview panel toggle.
describe('HeaderBar preview toggle', () => {
  async function renderWithPreview(onTogglePreviewPanel: () => void, previewPanelOpen = false) {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('[]', { status: 200 }))),
    );
    await act(async () => {
      root.render(
        <HeaderBar
          isMobile={false}
          isSidebarOpen
          isConnected
          onToggleSidebar={() => undefined}
          onToggleContextPanel={() => undefined}
          onTogglePreviewPanel={onTogglePreviewPanel}
          previewPanelOpen={previewPanelOpen}
        />,
      );
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }

  it('renders the toggle when the host wires it in and fires the callback', async () => {
    const onToggle = vi.fn();
    await renderWithPreview(onToggle, false);
    const btn = container.querySelector('[data-testid="preview-panel-toggle"]');
    expect(btn).not.toBeNull();
    expect(btn).toHaveAttribute('aria-pressed', 'false');
    btn?.click();
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('reflects the open state through aria-pressed', async () => {
    await renderWithPreview(() => undefined, true);
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toHaveAttribute('aria-pressed', 'true');
  });

  it('renders no preview toggle when the host does not wire it in', async () => {
    await renderHeader(200, '[]');
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toBeNull();
  });

  it('hides the preview toggle on mobile', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('[]', { status: 200 }))),
    );
    await act(async () => {
      root.render(
        <HeaderBar
          isMobile
          isSidebarOpen
          isConnected
          onToggleSidebar={() => undefined}
          onToggleContextPanel={() => undefined}
          onTogglePreviewPanel={() => undefined}
        />,
      );
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toBeNull();
  });
});
