import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import React from 'react';
import AutomationsPanel from './AutomationsPanel';

// ── Mock dependencies ───────────────────────────────────────────────────────

vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('./AutomationsPanel.css', () => ({}));

// Import AFTER mocking
import { clientFetch } from '../services/clientSession';

// ── Helpers ─────────────────────────────────────────────────────────────────

const EPOCH_S = 1_000_000; // fixed epoch for deterministic elapsed math

function mr(ok: boolean, body: unknown, status = ok ? 200 : 500) {
  return { ok, status, json: vi.fn().mockResolvedValue(body) } as unknown as Response;
}

function wfResp(list: { name: string; description: string; filename: string; file_path: string }[]) {
  return mr(true, { workflows: list });
}

function seResp(
  list: {
    session_id: string;
    workflow: string;
    pid: number;
    status: 'running' | 'exited' | 'stopped';
    started_at: number;
    kind: string;
    output_file_path: string;
    budget_usd: number;
  }[],
) {
  return mr(true, { sessions: list });
}

function errResp(status = 500) {
  return mr(false, {}, status);
}

function runResp(sid: string, wf: string) {
  return mr(true, { session_id: sid, workflow: wf, status: 'running' });
}

/**
 * Set up clientFetch to return responses in order using an index counter.
 */
function mockFetchSequence(...responses: ReturnType<typeof mr>[]) {
  let idx = 0;
  vi.mocked(clientFetch).mockImplementation(async () => {
    if (idx < responses.length) {
      return responses[idx++];
    }
    // Fallback: return the last response (for polling)
    return responses[responses.length - 1];
  });
}

/**
 * Replicate the component's truncateId so we can match aria-labels.
 */
function truncateId(id: string) {
  if (id.length <= 12) return id;
  return id.slice(0, 12) + '…';
}

/** Stop button by aria-label (component uses truncateId on the session id). */
function stopBtn(id: string) {
  return screen.getByRole('button', { name: `Stop session ${truncateId(id)}` });
}

// ── Tests ───────────────────────────────────────────────────────────────────

describe('AutomationsPanel', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  // ── Tab Rendering ────────────────────────────────────────────

  it('renders all three tab buttons', () => {
    mockFetchSequence(wfResp([]));
    render(<AutomationsPanel />);

    expect(screen.getByRole('tab', { name: 'Available' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Running' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Recent' })).toBeInTheDocument();
  });

  it('highlights the Available tab by default', () => {
    mockFetchSequence(wfResp([]));
    render(<AutomationsPanel />);

    expect(screen.getByRole('tab', { name: 'Available' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches to the Running tab when clicked', async () => {
    mockFetchSequence(wfResp([]), seResp([]));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    // Wait for the async fetch to settle
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Running' })).toHaveAttribute('aria-selected', 'true');
    });
  });

  it('switches to the Recent tab when clicked', async () => {
    mockFetchSequence(wfResp([]), seResp([]));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Recent' })).toHaveAttribute('aria-selected', 'true');
    });
  });

  // ── Available Tab ────────────────────────────────────────────

  it('shows empty state when no workflows', async () => {
    mockFetchSequence(wfResp([]));
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByText('No automation workflows available')).toBeInTheDocument();
    });
  });

  it('shows loading state while fetching workflows', () => {
    vi.mocked(clientFetch).mockReturnValue(new Promise(() => {}));
    render(<AutomationsPanel />);
    expect(screen.getByText('Loading workflows...')).toBeInTheDocument();
  });

  it('shows workflow cards with name, description, and Run button', async () => {
    mockFetchSequence(
      wfResp([
        { name: 'my-test', description: 'Runs test suite', filename: 'my-test.json', file_path: '/a/my-test.json' },
        { name: 'build-pkg', description: '', filename: 'build-pkg.json', file_path: '/a/build-pkg.json' },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByText('my-test')).toBeInTheDocument();
    });
    expect(screen.getByText('Runs test suite')).toBeInTheDocument();
    expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    expect(screen.getByText('build-pkg')).toBeInTheDocument();
    expect(screen.getByLabelText('Run build-pkg')).toBeInTheDocument();
  });

  it('handles workflow fetch errors (500)', async () => {
    mockFetchSequence(errResp(500));
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByText('Failed to fetch workflows: Internal server error')).toBeInTheDocument();
    });
  });

  it('handles workflow fetch errors (404)', async () => {
    mockFetchSequence(errResp(404));
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByText('Failed to fetch workflows: Not found')).toBeInTheDocument();
    });
  });

  it('handles workflow fetch errors (503)', async () => {
    mockFetchSequence(errResp(503));
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByText('Failed to fetch workflows: Service unavailable')).toBeInTheDocument();
    });
  });

  // ── Running Tab ──────────────────────────────────────────────

  it('shows empty state when no running sessions', async () => {
    mockFetchSequence(wfResp([]), seResp([]));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('No sessions running')).toBeInTheDocument();
    });
  });

  it('shows running session rows with truncated ID and workflow name', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'abc123-def456-ghi789',
          workflow: 'my-test',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 65,
          kind: 'workflow',
          output_file_path: '/tmp/o.txt',
          budget_usd: 5,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('abc123-def45…')).toBeInTheDocument();
    });
    expect(screen.getByText('my-test')).toBeInTheDocument();
  });

  it('shows "No limit" when budget_usd is 0', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('No limit')).toBeInTheDocument();
    });
  });

  it('renders budget bar with spent/cap text', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 10,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('$0.00 / $10.00')).toBeInTheDocument();
    });
  });

  it('shows loading state while fetching sessions', () => {
    mockFetchSequence(wfResp([]));
    vi.mocked(clientFetch).mockReturnValue(new Promise(() => {}));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    expect(screen.getByText('Loading sessions...')).toBeInTheDocument();
  });

  it('shows error when session fetch fails', async () => {
    mockFetchSequence(wfResp([]), errResp(500));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('Failed to fetch sessions: Internal server error')).toBeInTheDocument();
    });
  });

  // ── Recent Tab ───────────────────────────────────────────────

  it('shows empty state when no recent sessions', async () => {
    mockFetchSequence(wfResp([]), seResp([]));
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByText('No recent sessions')).toBeInTheDocument();
    });
  });

  it('shows exited and stopped sessions in Recent tab', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'se',
          workflow: 'my-test',
          pid: 1,
          status: 'exited',
          started_at: EPOCH_S - 300,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 5,
        },
        {
          session_id: 'ss',
          workflow: 'build-pkg',
          pid: 2,
          status: 'stopped',
          started_at: EPOCH_S - 600,
          kind: 'workflow',
          output_file_path: '/t/o2.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByText('Exited')).toBeInTheDocument();
    });
    expect(screen.getByText('Stopped')).toBeInTheDocument();
  });

  it('truncates long session IDs in recent tab', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'session-1234567890-abcdef',
          workflow: 'my-test',
          pid: 1,
          status: 'exited',
          started_at: EPOCH_S - 300,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByText('session-1234…')).toBeInTheDocument();
    });
  });

  // ── Tab Count Badges ─────────────────────────────────────────

  it('displays count badge on Running tab', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w1',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/1.txt',
          budget_usd: 0,
        },
        {
          session_id: 's2',
          workflow: 'w2',
          pid: 2,
          status: 'running',
          started_at: EPOCH_S - 5,
          kind: 'workflow',
          output_file_path: '/t/2.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('2')).toBeInTheDocument();
    });
  });

  // ── Run Modal ────────────────────────────────────────────────

  it('opens the run modal with workflow name and description', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Runs test suite',
          filename: 'my-test.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));

    expect(screen.getByText('Run Workflow')).toBeInTheDocument();
    // Workflow name appears both on card AND in modal
    expect(screen.getAllByText('my-test').length).toBeGreaterThanOrEqual(2);
    // Description also appears twice
    expect(screen.getAllByText('Runs test suite').length).toBeGreaterThanOrEqual(2);
  });

  it('shows budget and heartbeat inputs in the run modal', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Test',
          filename: 'my-test.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    expect(screen.getByLabelText(/Budget/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Heartbeat/)).toBeInTheDocument();
  });

  it('closes the run modal when clicking the close button', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Test',
          filename: 'my-test.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByLabelText('Close'));
    expect(screen.queryByText('Run Workflow')).not.toBeInTheDocument();
  });

  it('closes the run modal when clicking the overlay', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Test',
          filename: 'my-test.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByRole('dialog').querySelector('.automations-modal-overlay')!);
    expect(screen.queryByText('Run Workflow')).not.toBeInTheDocument();
  });

  it('closes the run modal when clicking Cancel', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Test',
          filename: 'my-test.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByText('Cancel'));
    expect(screen.queryByText('Run Workflow')).not.toBeInTheDocument();
  });

  // ── Run Workflow Submission ──────────────────────────────────

  it('submits workflow run with budget and heartbeat', async () => {
    mockFetchSequence(
      wfResp([{ name: 'my-test', description: 'Test', filename: 'mt.json', file_path: '/a/mt.json' }]),
      runResp('sid', 'my-test'),
      seResp([
        {
          session_id: 'sid',
          workflow: 'my-test',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 5,
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.change(screen.getByLabelText(/Budget/), { target: { value: '5.00' } });
    fireEvent.change(screen.getByLabelText(/Heartbeat/), { target: { value: '60' } });
    fireEvent.click(screen.getByRole('button', { name: 'Run' }));

    await waitFor(() => {
      const call = vi.mocked(clientFetch).mock.calls.find((c) => c[0] === '/api/automate/run');
      expect(call).toBeDefined();
    });
    const call = vi.mocked(clientFetch).mock.calls.find((c) => c[0] === '/api/automate/run');
    expect(call![1]?.method).toBe('POST');
    const body = JSON.parse((call![1]!.body as string) || '{}');
    expect(body.workflow).toBe('my-test');
    expect(body.budget_usd).toBe(5);
    expect(body.heartbeat).toBe(60);
  });

  it('omits budget/heartbeat when fields are empty', async () => {
    mockFetchSequence(
      wfResp([{ name: 'my-test', description: '', filename: 'mt.json', file_path: '/a/mt.json' }]),
      runResp('sid', 'my-test'),
      seResp([]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByRole('button', { name: 'Run' }));

    await waitFor(() => {
      const call = vi.mocked(clientFetch).mock.calls.find((c) => c[0] === '/api/automate/run');
      expect(call).toBeDefined();
    });
    const call = vi.mocked(clientFetch).mock.calls.find((c) => c[0] === '/api/automate/run');
    const body = JSON.parse((call![1]!.body as string) || '{}');
    expect(body.workflow).toBe('my-test');
    expect(body.budget_usd).toBeUndefined();
    expect(body.heartbeat).toBeUndefined();
  });

  it('switches to Running tab after successful run', async () => {
    mockFetchSequence(
      wfResp([{ name: 'my-test', description: '', filename: 'mt.json', file_path: '/a/mt.json' }]),
      runResp('sid', 'my-test'),
      seResp([
        {
          session_id: 'sid',
          workflow: 'my-test',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByRole('button', { name: 'Run' }));

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Running' })).toHaveAttribute('aria-selected', 'true');
    });
  });

  it('does nothing when confirm returns false', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    );
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: '',
          filename: 'mt.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByRole('button', { name: 'Run' }));

    expect(screen.getByText('Run Workflow')).toBeInTheDocument();
    expect(vi.mocked(clientFetch).mock.calls.filter((c) => c[0] === '/api/automate/run')).toHaveLength(0);
  });

  it('shows error on Running tab when run fails and session fetch also fails', async () => {
    mockFetchSequence(
      wfResp([{ name: 'my-test', description: '', filename: 'mt.json', file_path: '/a/mt.json' }]),
      errResp(500), // POST run fails
      errResp(500), // fetchSessions fails when switching to Running
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    fireEvent.click(screen.getByRole('button', { name: 'Run' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));

    await waitFor(() => {
      expect(screen.getByText('Failed to fetch sessions: Internal server error')).toBeInTheDocument();
    });
  });

  // ── Stop Session ─────────────────────────────────────────────

  it('calls stop API endpoint when Stop button is clicked', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'stop-session-01',
          workflow: 'my-test',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 5,
        },
      ]),
      mr(true, {}), // stop response
      seResp([]), // re-fetch after stop
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('my-test')).toBeInTheDocument();
    });
    fireEvent.click(stopBtn('stop-session-01'));

    await waitFor(() => {
      const call = vi.mocked(clientFetch).mock.calls.find((c) => typeof c[0] === 'string' && c[0].includes('/stop'));
      expect(call).toBeDefined();
    });
    const call = vi.mocked(clientFetch).mock.calls.find((c) => typeof c[0] === 'string' && c[0].includes('/stop'));
    expect(call![1]?.method).toBe('POST');
  });

  it('disables the Stop button while stopping', async () => {
    vi.useFakeTimers();

    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'stop-me',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );

    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));

    // With fake timers, Promise microtasks don't auto-fire.
    // Use runOnlyPendingTimersAsync to flush pending promises (effect callbacks).
    await vi.runOnlyPendingTimersAsync();

    expect(screen.getByText('w')).toBeInTheDocument();

    // After the initial sequence is exhausted, make the stop call never resolve
    vi.mocked(clientFetch).mockReturnValue(new Promise(() => {}));

    fireEvent.click(stopBtn('stop-me'));
    expect(screen.getByText('Stopping...')).toBeInTheDocument();
  });

  it('does nothing when stop confirm returns false', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    );
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'stop-me',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('w')).toBeInTheDocument();
    });
    fireEvent.click(stopBtn('stop-me'));

    expect(
      vi.mocked(clientFetch).mock.calls.filter((c) => typeof c[0] === 'string' && c[0].includes('/stop')),
    ).toHaveLength(0);
  });

  it('shows error when stop fails', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'stop-me',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
      errResp(503), // stop fails
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('w')).toBeInTheDocument();
    });
    fireEvent.click(stopBtn('stop-me'));

    await waitFor(() => {
      expect(screen.getByText('Failed to stop session: Service unavailable')).toBeInTheDocument();
    });
  });

  // ── onNavigateToSession Callback ─────────────────────────────

  it('calls onNavigateToSession when clicking a recent session row', async () => {
    const onNavigate = vi.fn();
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'nav-session-1',
          workflow: 'my-test',
          pid: 1,
          status: 'exited',
          started_at: EPOCH_S - 300,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel onNavigateToSession={onNavigate} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByText('my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('my-test'));
    expect(onNavigate).toHaveBeenCalledWith('nav-session-1');
  });

  it('does not mark rows as clickable when onNavigateToSession is not provided', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'sess-1',
          workflow: 'my-test',
          pid: 1,
          status: 'exited',
          started_at: EPOCH_S - 300,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    const { container } = render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Recent' }));
    await waitFor(() => {
      expect(screen.getByText('my-test')).toBeInTheDocument();
    });
    const rows = container.querySelectorAll('.automations-session-row');
    for (const row of rows) {
      expect(row.classList.contains('clickable')).toBe(false);
    }
  });

  it('opens detail panel when clicking a running session row', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'detail-sess',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('w')).toBeInTheDocument();
    });
    // Click the session row
    fireEvent.click(screen.getByText('detail-sess'));
    // Verify detail overlay is rendered
    expect(document.querySelector('.automations-detail-overlay')).toBeInTheDocument();
  });

  // ── Elapsed Time Formatting (requires fake timers) ───────────

  it('formats elapsed seconds only (45s)', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000_000); // Date.now()/1000 === EPOCH_S

    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 45,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('45s')).toBeInTheDocument();
    });
  });

  it('formats elapsed minutes and seconds (3m 5s)', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000_000);

    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 185,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('3m 5s')).toBeInTheDocument();
    });
  });

  it('formats elapsed hours and minutes (2h 2m)', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000_000);

    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 's1',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 7320,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('2h 2m')).toBeInTheDocument();
    });
  });

  // ── Polling Behavior (requires fake timers) ──────────────────

  it('polls sessions periodically when on the Running tab', async () => {
    vi.useFakeTimers();

    mockFetchSequence(
      seResp([
        {
          session_id: 'poll-test',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );

    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));

    await waitFor(() => {
      expect(screen.getByText('w')).toBeInTheDocument();
    });

    vi.advanceTimersByTime(3000); // past one polling interval

    await waitFor(() => {
      expect(vi.mocked(clientFetch).mock.calls.length).toBeGreaterThan(1);
    });
  });

  it('stops polling when switching away from Running tab', async () => {
    vi.useFakeTimers();

    mockFetchSequence(
      seResp([
        {
          session_id: 'poll-test',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );

    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));

    await waitFor(() => {
      expect(screen.getByText('w')).toBeInTheDocument();
    });
    const afterRunning = vi.mocked(clientFetch).mock.calls.length;

    // Switch away — stops polling
    fireEvent.click(screen.getByRole('tab', { name: 'Available' }));
    vi.advanceTimersByTime(12000);
    await Promise.resolve();

    // At most 1 extra call (Available tab fetches workflows once)
    expect(vi.mocked(clientFetch).mock.calls.length - afterRunning).toBeLessThanOrEqual(1);
  });

  // ── Accessibility ────────────────────────────────────────────

  it('renders the run modal with proper ARIA attributes', async () => {
    mockFetchSequence(
      wfResp([
        {
          name: 'my-test',
          description: 'Test',
          filename: 'mt.json',
          file_path: '/a/mt.json',
        },
      ]),
    );
    render(<AutomationsPanel />);
    await waitFor(() => {
      expect(screen.getByLabelText('Run my-test')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Run my-test'));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAttribute('aria-labelledby', 'run-modal-title');
  });

  it('renders the tab bar with role="tablist"', () => {
    mockFetchSequence(wfResp([]));
    render(<AutomationsPanel />);
    const tablist = screen.getByRole('tablist');
    expect(tablist).toHaveAttribute('aria-label', 'Automation tabs');
  });

  // ── Session ID Truncation ────────────────────────────────────

  it('displays short session IDs without truncation', async () => {
    mockFetchSequence(
      wfResp([]),
      seResp([
        {
          session_id: 'short',
          workflow: 'w',
          pid: 1,
          status: 'running',
          started_at: EPOCH_S - 10,
          kind: 'workflow',
          output_file_path: '/t/o.txt',
          budget_usd: 0,
        },
      ]),
    );
    render(<AutomationsPanel />);
    fireEvent.click(screen.getByRole('tab', { name: 'Running' }));
    await waitFor(() => {
      expect(screen.getByText('short')).toBeInTheDocument();
    });
  });
});
