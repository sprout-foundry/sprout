/**
 * ScreensGrid co-editing integration (SP-140-7 §7b).
 *
 * Drives the real event bridge: an agent-file-changed event under the held
 * buffer fires the banner; Keep mine forces the §7a write (asserted on the
 * injected transport — never a window.fetch spy); Take theirs reloads the
 * pane; other assets' changes never surface.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import type { DesignInventory } from '../../services/api/types';
import ScreensGrid from './ScreensGrid';

// The conflict tests exercise the banner/decision flow, not preview
// rendering. LivePreview embeds a CodeMirror editor whose measure loop
// throws an unhandled getClientRects error under jsdom (which fails the
// whole vitest run with exit 1), so it is stubbed here.
vi.mock('../LivePreview', () => ({
  default: function MockLivePreview({ fileName }: { fileName?: string }) {
    return <div data-testid="mock-live-preview">{fileName}</div>;
  },
}));

// Hoisted so the vi.mock factory can close over it (factories hoist above
// module scope); one transport per test via mockReset in beforeEach.
const { transportMock } = vi.hoisted(() => ({ transportMock: vi.fn<typeof fetch>() }));

vi.mock('../../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, useSproutFetch: () => transportMock };
});

const DISK_TEXT = '<p>on disk now</p>';

function textResponse(body: string): Response {
  return new Response(body, { status: 200 });
}

function inventory(): DesignInventory {
  return {
    exists: true,
    manifest: { path: 'README.md', exists: false, frames: [], chars: 0 },
    assets: [{ path: 'screens/login.html', name: 'login.html', kind: 'screen', size: 10, modified: 0 }],
    wireframes: [],
    screens: [{ path: 'screens/login.html', name: 'login.html', kind: 'screen', size: 10, modified: 0 }],
    flows: [],
    layouts: [],
    tokenFiles: [],
    feedback: [],
    tokenGroups: [],
    tokenCount: 0,
    flowSummaries: [],
    summary: '',
  };
}

describe('ScreensGrid conflict flow', () => {
  beforeEach(() => {
    transportMock.mockReset();
    // GET /api/file -> the disk text; feedback reads -> empty doc; other GETs -> empty.
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return new Response(JSON.stringify({ success: true }), { status: 200 });
      }
      const url = String(input);
      if (url.includes('feedback/')) {
        return textResponse(JSON.stringify({ target: '', status: '', resolution: '', annotations: [] }));
      }
      return textResponse(DISK_TEXT);
    });
  });

  function renderGrid(held: string) {
    return render(
      <SproutAdapterProvider>
        <ScreensGrid
          inventory={inventory()}
          contentByPath={{ 'screens/login.html': held }}
          selectedPath="screens/login.html"
        />
      </SproutAdapterProvider>,
    );
  }

  async function fireAgentChange() {
    await act(async () => {
      window.dispatchEvent(new CustomEvent('agent-file-changed', { detail: { path: 'design/screens/login.html' } }));
    });
  }

  it('an agent write under the held buffer surfaces the banner', async () => {
    renderGrid('<p>user edit</p>');
    expect(await screen.findByTestId('design-screen-detail')).toBeInTheDocument();
    await fireAgentChange();
    expect(await screen.findByTestId('design-conflict')).toBeInTheDocument();
  });

  it('identical disk text does not surface a banner (silent refresh)', async () => {
    renderGrid(DISK_TEXT);
    expect(await screen.findByTestId('design-screen-detail')).toBeInTheDocument();
    await fireAgentChange();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
    expect(screen.queryByTestId('design-conflict')).not.toBeInTheDocument();
  });

  it('keep mine forces the write through and offers restore', async () => {
    renderGrid('<p>user edit</p>');
    expect(await screen.findByTestId('design-screen-detail')).toBeInTheDocument();
    await fireAgentChange();
    await screen.findByTestId('design-conflict');

    fireEvent.click(screen.getByTestId('design-conflict-keep'));
    await waitFor(() => expect(screen.getByTestId('design-conflict')).toHaveAttribute('data-resolved', 'true'));

    const posts = transportMock.mock.calls.filter(([, init]) => init?.method === 'POST');
    expect(posts.length).toBeGreaterThanOrEqual(1);
    expect(String(posts[0][1]?.body)).toContain('<p>user edit</p>');

    fireEvent.click(screen.getByTestId('design-conflict-restore'));
    await waitFor(() => expect(screen.queryByTestId('design-conflict')).not.toBeInTheDocument());
  });

  it('take theirs reloads the pane with the disk text and clears the banner', async () => {
    renderGrid('<p>user edit</p>');
    expect(await screen.findByTestId('design-screen-detail')).toBeInTheDocument();
    await fireAgentChange();
    await screen.findByTestId('design-conflict');
    fireEvent.click(screen.getByTestId('design-conflict-take'));
    await waitFor(() => expect(screen.queryByTestId('design-conflict')).not.toBeInTheDocument());
  });

  it('a change to another asset never surfaces the banner', async () => {
    renderGrid('<p>user edit</p>');
    expect(await screen.findByTestId('design-screen-detail')).toBeInTheDocument();
    await act(async () => {
      window.dispatchEvent(new CustomEvent('agent-file-changed', { detail: { path: 'design/screens/other.html' } }));
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
    expect(screen.queryByTestId('design-conflict')).not.toBeInTheDocument();
  });
});
