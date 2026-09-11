// @ts-nocheck
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';

const clientFetchMock = vi.hoisted(() => vi.fn());
const getBootstrapSyncMock = vi.hoisted(() => vi.fn());

vi.mock('../services/clientSession', () => ({
  clientFetch: clientFetchMock,
}));

vi.mock('../bootstrapAdapter', () => ({
  getBootstrapSync: getBootstrapSyncMock,
}));

import SyncStatusBanner from './SyncStatusBanner';

function okBody(body: unknown) {
  return { ok: true, status: 200, json: async () => body };
}

const CLEAN_REPORT = {
  in_git_repo: true,
  branch: 'main',
  dirty_files: [],
  ahead: 0,
  behind: 0,
  last_commit: { sha: 'abc1234def', subject: 'init', author: 'a', timestamp: '2026-09-11T00:00:00Z' },
  pull: { attempted: false, result: 'not_attempted', error: '' },
};

const DIRTY_REPORT = {
  ...CLEAN_REPORT,
  dirty_files: ['src/a.ts', 'src/b.ts'],
  ahead: 2,
  behind: 1,
};

let container: HTMLDivElement;
let root: Root;

function renderBanner() {
  act(() => {
    root.render(<SyncStatusBanner />);
  });
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  clientFetchMock.mockReset();
  getBootstrapSyncMock.mockReset();
  getBootstrapSyncMock.mockReturnValue(null);
  sessionStorage.clear();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

describe('SyncStatusBanner', () => {
  it('renders nothing when the live report is clean', async () => {
    clientFetchMock.mockResolvedValue(okBody(CLEAN_REPORT));
    renderBanner();
    await act(async () => {});
    expect(container.textContent).toBe('');
  });

  it('shows dirty/ahead/behind counts from the live GET /api/sync report', async () => {
    clientFetchMock.mockResolvedValue(okBody(DIRTY_REPORT));
    renderBanner();
    await act(async () => {});

    const banner = container.querySelector('.sync-status-banner');
    expect(banner).not.toBeNull();
    expect(container.textContent).toContain('main');
    expect(container.textContent).toContain('2 uncommitted files');
    expect(container.textContent).toContain('2 ahead');
    expect(container.textContent).toContain('1 behind');
    expect(container.textContent).toContain('abc1234');
    // The live fetch must be a GET (status only, never a pull)
    expect(clientFetchMock).toHaveBeenCalledWith('/api/sync');
  });

  it('falls back to the bootstrap snapshot before the live fetch resolves', () => {
    getBootstrapSyncMock.mockReturnValue(DIRTY_REPORT);
    clientFetchMock.mockReturnValue(new Promise(() => {})); // never resolves
    renderBanner();
    const banner = container.querySelector('.sync-status-banner');
    expect(banner).not.toBeNull();
    expect(container.textContent).toContain('1 behind');
  });

  it('renders nothing when there is no report and no error (not a git repo)', async () => {
    clientFetchMock.mockResolvedValue(okBody({ ...CLEAN_REPORT, in_git_repo: false, branch: '' }));
    renderBanner();
    await act(async () => {});
    expect(container.querySelector('.sync-status-banner')).toBeNull();
  });

  it('Pull button issues POST /api/sync and renders the resulting report', async () => {
    clientFetchMock
      .mockResolvedValueOnce(okBody(DIRTY_REPORT)) // initial live fetch
      .mockResolvedValueOnce(
        okBody({ ...CLEAN_REPORT, pull: { attempted: true, result: 'fast_forwarded', error: '' } }),
      ); // the pull
    renderBanner();
    await act(async () => {});

    const pullBtn = container.querySelector('.sync-status-banner-btn.primary') as HTMLButtonElement;
    expect(pullBtn).not.toBeNull();
    await act(async () => {
      pullBtn.click();
      await Promise.resolve();
    });

    expect(clientFetchMock).toHaveBeenLastCalledWith('/api/sync', { method: 'POST' });
    // Pull succeeded and tree is clean → banner self-hides
    expect(container.querySelector('.sync-status-banner')).toBeNull();
  });

  it('stays visible with an error note when the status fetch fails', async () => {
    clientFetchMock.mockRejectedValue(new Error('network down'));
    renderBanner();
    await act(async () => {});

    const banner = container.querySelector('.sync-status-banner');
    expect(banner).not.toBeNull();
    expect(container.textContent).toContain('Sync check failed');
  });

  it('dismisses for the session via the close button', async () => {
    clientFetchMock.mockResolvedValue(okBody(DIRTY_REPORT));
    renderBanner();
    await act(async () => {});

    const closeBtn = container.querySelector('.sync-status-banner-close') as HTMLButtonElement;
    await act(async () => {
      closeBtn.click();
    });

    expect(container.querySelector('.sync-status-banner')).toBeNull();
    expect(sessionStorage.getItem('sprout:sync-banner-dismissed')).toBe('1');
  });
});
