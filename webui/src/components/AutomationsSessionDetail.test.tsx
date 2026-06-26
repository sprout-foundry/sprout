import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import React from 'react';
import AutomationsSessionDetail from './AutomationsSessionDetail';

// ── Mock dependencies ───────────────────────────────────────────────────────

vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('./AutomationsSessionDetail.css', () => ({}));

// Import AFTER mocking
import { clientFetch } from '../services/clientSession';

// ── Helpers ─────────────────────────────────────────────────────────────────

const EPOCH_S = 1_000_000;

function mr(ok: boolean, body: unknown, status = ok ? 200 : 500) {
  return { ok, status, json: vi.fn().mockResolvedValue(body) } as unknown as Response;
}

function sessionResp(
  id: string,
  status: 'running' | 'exited' | 'stopped',
  workflow = 'test-workflow',
  budgetUsd: number | null = 5,
  outputPath = '/tmp/out.txt',
  startedAt = EPOCH_S - 60,
) {
  return mr(true, {
    session_id: id,
    workflow,
    pid: 1234,
    status,
    started_at: startedAt,
    kind: 'workflow',
    output_file_path: outputPath,
    budget_usd: budgetUsd,
  });
}

function outputResp(output: string, offset: number, total: number) {
  return mr(true, { output, offset, total });
}

function mockFetchSequence(...responses: ReturnType<typeof mr>[]) {
  let idx = 0;
  vi.mocked(clientFetch).mockImplementation(async () => {
    if (idx < responses.length) {
      return responses[idx++];
    }
    return responses[responses.length - 1];
  });
}

// ── Tests ───────────────────────────────────────────────────────────────────

describe('AutomationsSessionDetail', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders with placeholder content initially before async data arrives', () => {
    // The component's useEffect starts async fetches but sets loading=false
    // before the promises resolve, so it shows placeholder content (not "Loading...")
    vi.mocked(clientFetch).mockReturnValue(new Promise(() => {}));

    render(<AutomationsSessionDetail sessionId="test-session" onClose={() => {}} />);

    // Shows placeholder data from props before session fetch resolves
    expect(screen.getByText('test-session')).toBeInTheDocument();
    expect(screen.getByText('Unknown')).toBeInTheDocument();
    expect(screen.getByText('No output captured')).toBeInTheDocument();
  });

  it('shows session ID and workflow name', async () => {
    mockFetchSequence(sessionResp('sess-001', 'running', 'my-workflow'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="sess-001" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('sess-001')).toBeInTheDocument();
    });
    expect(screen.getByText('my-workflow')).toBeInTheDocument();
  });

  it('shows Running status badge for running sessions', async () => {
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });
  });

  it('shows Exited status badge for exited sessions', async () => {
    mockFetchSequence(sessionResp('s1', 'exited'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Exited')).toBeInTheDocument();
    });
  });

  it('shows Stopped status badge for stopped sessions', async () => {
    mockFetchSequence(sessionResp('s1', 'stopped'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Stopped')).toBeInTheDocument();
    });
  });

  it('shows elapsed time formatted from started_at', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000_000); // Date.now()/1000 === EPOCH_S

    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0, '/tmp/out.txt', EPOCH_S - 45), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('45s')).toBeInTheDocument();
    });
  });

  it('shows budget cap when budget_usd > 0', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 10), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('$10.00 cap')).toBeInTheDocument();
    });
  });

  it('shows "No limit" when budget_usd is 0', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('No limit')).toBeInTheDocument();
    });
  });

  it('shows "No limit" when budget_usd is null', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', null), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('No limit')).toBeInTheDocument();
    });
  });

  it('shows output stream when output_file_path exists and output is present', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0, '/tmp/out.txt'), outputResp('line1\nline2', 10, 10));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    // RTL normalizes whitespace in text matching, so use a function matcher
    await waitFor(() => {
      expect(screen.getByText((content) => content.includes('line1') && content.includes('line2'))).toBeInTheDocument();
    });

    const codeEl = document.querySelector('.automations-output-code');
    expect(codeEl).toBeInTheDocument();
  });

  it('shows "No output captured" when output_file_path is empty string', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0, ''), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('No output captured')).toBeInTheDocument();
    });
  });

  it('shows "(empty)" when output_file_path exists but output is empty', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0, '/tmp/empty.txt'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('(empty)')).toBeInTheDocument();
    });
  });

  it('shows step progress placeholder', async () => {
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Step progress not available')).toBeInTheDocument();
    });
  });

  it('shows budget events placeholder with utilization text', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 25), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Utilization: $0.00 / $25.00')).toBeInTheDocument();
    });
  });

  it('shows budget events placeholder without budget when budget is 0', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('No budget tracking available')).toBeInTheDocument();
    });
  });

  it('close button calls onClose', async () => {
    const onClose = vi.fn();
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={onClose} />);

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByLabelText('Close'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Escape key calls onClose', async () => {
    const onClose = vi.fn();
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={onClose} />);

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('shows error on fetch failure', async () => {
    mockFetchSequence(mr(false, {}, 500), mr(false, {}, 500));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Failed to fetch session: 500')).toBeInTheDocument();
    });
  });

  it('polls while running — calls session+output endpoints every 2s', async () => {
    vi.useFakeTimers();

    // Initial: session + output
    // Poll 1: session + output
    // Poll 2: session + output
    mockFetchSequence(
      sessionResp('s1', 'running'),
      outputResp('', 0, 0),
      sessionResp('s1', 'running'),
      outputResp('', 0, 0),
      sessionResp('s1', 'running'),
      outputResp('', 0, 0),
    );

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    // Wait for initial renders to settle
    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });

    const initialCalls = vi.mocked(clientFetch).mock.calls.length;

    // Advance past one polling interval
    vi.advanceTimersByTime(2500);
    await Promise.resolve();

    // Should have made 2 more calls (session + output)
    expect(vi.mocked(clientFetch).mock.calls.length).toBe(initialCalls + 2);

    // Advance past another polling interval
    vi.advanceTimersByTime(2500);
    await Promise.resolve();

    // Should have 2 more calls
    expect(vi.mocked(clientFetch).mock.calls.length).toBe(initialCalls + 4);
  });

  it('stops polling when session status changes to exited', async () => {
    vi.useFakeTimers();

    // Initial: session(running) + output
    // Poll 1: session(exited) + output
    // Extra responses to absorb any additional interval fires before cleanup
    mockFetchSequence(
      sessionResp('s1', 'running'),
      outputResp('', 0, 0),
      sessionResp('s1', 'exited'),
      outputResp('', 0, 0),
      sessionResp('s1', 'exited'),
      outputResp('', 0, 0),
    );

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });

    // Advance past one polling interval — session changes to exited
    vi.advanceTimersByTime(2500);
    await vi.runOnlyPendingTimersAsync();

    // Verify the status updated in the UI
    expect(screen.getByText('Exited')).toBeInTheDocument();

    // Advance further — no more polling since status is exited
    vi.advanceTimersByTime(5000);
    await vi.runOnlyPendingTimersAsync();

    // The call count should be stable — no new calls after cleanup.
    // We use >= 4 (initial 2 + at least 1 poll) and check no new calls were made
    // in the final 5s window by verifying the last response index didn't increase.
    const callsAfterFirstPoll = vi.mocked(clientFetch).mock.calls.length;
    vi.advanceTimersByTime(5000);
    await vi.runOnlyPendingTimersAsync();
    expect(vi.mocked(clientFetch).mock.calls.length).toBe(callsAfterFirstPoll);
  });

  it('output auto-scrolls on new output', async () => {
    vi.useFakeTimers();

    mockFetchSequence(sessionResp('s1', 'running'), outputResp('initial output', 14, 14));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('initial output')).toBeInTheDocument();
    });

    // Verify the output container exists (scrollToBottom operates on it)
    const preEl = document.querySelector('.automations-output-pre');
    expect(preEl).toBeInTheDocument();
  });

  it('appends output on subsequent polling fetches', async () => {
    vi.useFakeTimers();

    // Initial: session(running) + output("part1")
    // Poll 1: session(running) + output("part2")
    mockFetchSequence(
      sessionResp('s1', 'running'),
      outputResp('part1', 5, 5),
      sessionResp('s1', 'running'),
      outputResp('part2', 10, 10),
    );

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    // Wait for initial output
    await waitFor(() => {
      expect(screen.getByText('part1')).toBeInTheDocument();
    });

    // Advance past one polling interval
    vi.advanceTimersByTime(2500);
    await Promise.resolve();

    // After polling, both parts should be visible (appended)
    await waitFor(() => {
      expect(screen.getByText('part1part2')).toBeInTheDocument();
    });
  });
});
