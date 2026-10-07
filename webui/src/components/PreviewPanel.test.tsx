/**
 * PreviewPanel — the Code-mode preview panel.
 *
 * Pins the wiring between the panel and the presentational PreviewPane:
 *   1. Closed renders nothing; open renders the pane.
 *   2. The pane is driven by the dev-server lifecycle API (the hook).
 *   3. The Restart action picks the right lifecycle call: a stopped/failed
 *      app is started (POST /start); a running app is restarted (POST /restart).
 *   4. A hosted (platform-registered) preview disables the local restart.
 *   5. The close affordance collapses the panel (onClose).
 *
 * The hook polls and applies action results asynchronously; a `flush` (one
 * macrotask tick inside act) settles those updates so assertions observe the
 * post-update render without act warnings.
 */

import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import PreviewPanel from './PreviewPanel';

// ---------------------------------------------------------------------------
// Mock the adapter fetch (the transport the hook uses)
// ---------------------------------------------------------------------------

let statusBody: {
  status: 'starting' | 'running' | 'stopped' | 'failed';
  url?: string;
  error?: string;
  detected?: boolean;
  hosted?: boolean;
} = { status: 'stopped' };
let postBodies: Record<string, { status: string; url?: string }> = {};
let posted: string[] = [];

const fetchMock = (input: unknown, init?: RequestInit): Promise<Response> => {
  const url = String(input);
  const method = String(init?.method ?? 'GET');
  if (url === '/api/preview/status') {
    return Promise.resolve(new Response(JSON.stringify(statusBody), { status: 200 }));
  }
  posted.push(`${method} ${url}`);
  const body = postBodies[url] ?? { status: 'starting' };
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
};

vi.mock('../contexts/SproutAdapterContext', () => ({
  __esModule: true,
  useSproutFetch: () => fetchMock,
}));
vi.mock('./PreviewPanel.css', () => ({}));

/** Settle the hook's async state updates (one macrotask tick inside act). */
const flush = async (): Promise<void> => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

beforeEach(() => {
  statusBody = { status: 'stopped' };
  postBodies = {};
  posted = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Open / closed
// ---------------------------------------------------------------------------

describe('PreviewPanel open/closed', () => {
  it('renders nothing when closed', () => {
    const { container } = render(<PreviewPanel open={false} onClose={() => undefined} />);
    expect(screen.queryByTestId('preview-pane')).toBeNull();
    expect(container.querySelector('.preview-panel')).toBeNull();
  });

  it('renders the pane inside the panel when open', async () => {
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();
    expect(screen.getByTestId('preview-panel')).toBeInTheDocument();
    expect(screen.getByTestId('preview-pane')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Driven by the lifecycle API
// ---------------------------------------------------------------------------

describe('PreviewPanel lifecycle', () => {
  it('shows the stopped affordance before the server starts', async () => {
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();
    expect(screen.getByTestId('preview-pane-stopped')).toBeInTheDocument();
    expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Stopped');
  });

  it('embeds the running app once the dev server is up', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();
    const iframe = screen.getByTestId('preview-pane-iframe');
    expect(iframe).toHaveAttribute('src', 'http://localhost:3000');
    expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Running');
  });

  it('shows the failure reason in the failed state', async () => {
    statusBody = { status: 'failed', error: 'dev server exited with code 1' };
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();
    const failed = screen.getByTestId('preview-pane-failed');
    expect(failed).toHaveTextContent('dev server exited with code 1');
  });
});

// ---------------------------------------------------------------------------
// Restart wiring
// ---------------------------------------------------------------------------

describe('PreviewPanel restart wiring', () => {
  it('starts a stopped app (POST /start) from the Restart action', async () => {
    statusBody = { status: 'stopped' };
    postBodies['/api/preview/start'] = { status: 'starting' };
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();

    fireEvent.click(screen.getByTestId('preview-pane-restart'));
    await flush();
    expect(posted).toContain('POST /api/preview/start');
  });

  it('restarts a running app (POST /restart) from the Restart action', async () => {
    statusBody = { status: 'running', url: 'http://localhost:3000' };
    postBodies['/api/preview/restart'] = { status: 'starting' };
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();

    fireEvent.click(screen.getByTestId('preview-pane-restart'));
    await flush();
    expect(posted).toContain('POST /api/preview/restart');
  });

  it('disables the Restart action for a hosted (platform) preview', async () => {
    statusBody = { status: 'running', url: 'https://preview.example.com/abc', hosted: true };
    render(<PreviewPanel open onClose={() => undefined} />);
    await flush();
    expect(screen.getByTestId('preview-pane-restart')).toBeDisabled();
  });
});

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

describe('PreviewPanel close', () => {
  it('collapses the panel from the close affordance', async () => {
    const onClose = vi.fn();
    render(<PreviewPanel open onClose={onClose} />);
    await flush();
    fireEvent.click(screen.getByTestId('preview-pane-close'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
