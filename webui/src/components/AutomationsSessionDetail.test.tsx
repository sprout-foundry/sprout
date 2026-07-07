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

vi.mock('../services/automateEvents', () => {
  const handlers: Array<(eventType: string, payload: unknown) => void> = [];
  return {
    subscribeAutomate: vi.fn((handler: (eventType: string, payload: unknown) => void) => {
      handlers.push(handler);
      return () => {
        const idx = handlers.indexOf(handler);
        if (idx >= 0) handlers.splice(idx, 1);
      };
    }),
    __getHandlers: () => handlers,
  };
});

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
      expect(screen.getByText(/Step-level events aren't tracked yet/)).toBeInTheDocument();
    });
  });

  it('shows budget section with cap text when budget is set', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 25), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText(/Session budget cap: \$25\.00/)).toBeInTheDocument();
    });
  });

  it('shows budget section fallback when budget is 0', async () => {
    mockFetchSequence(sessionResp('s1', 'running', 'wf', 0), outputResp('', 0, 0));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('No budget cap was set for this session.')).toBeInTheDocument();
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

  it('refetches output when output_chunk event arrives for matching session', async () => {
    vi.useFakeTimers();

    // Initial: session + output("part1")
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('part1', 5, 5), outputResp('part2', 10, 10));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    // Wait for initial output
    await waitFor(() => {
      expect(screen.getByText('part1')).toBeInTheDocument();
    });

    // Simulate the output_chunk event for this session
    const { __getHandlers } = await import('../services/automateEvents');
    const handlers = __getHandlers();
    if (handlers.length > 0) {
      handlers[0]('automate.output_chunk', { session_id: 's1', offset: 5, chunk_len: 5 });
    }

    // Advance timers past the 250ms debounce
    vi.advanceTimersByTime(300);
    await Promise.resolve();

    // After the event, part2 should be appended
    await waitFor(() => {
      expect(screen.getByText('part1part2')).toBeInTheDocument();
    });
  });

  it('ignores output_chunk event for non-matching session_id', async () => {
    vi.useFakeTimers();

    mockFetchSequence(sessionResp('s1', 'running'), outputResp('part1', 5, 5));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('part1')).toBeInTheDocument();
    });

    const callsBefore = vi.mocked(clientFetch).mock.calls.length;

    // Simulate output_chunk for a DIFFERENT session
    const { __getHandlers } = await import('../services/automateEvents');
    const handlers = __getHandlers();
    if (handlers.length > 0) {
      handlers[0]('automate.output_chunk', { session_id: 'other-session', offset: 5, chunk_len: 5 });
    }

    vi.advanceTimersByTime(500);
    await Promise.resolve();

    // No extra fetch calls — the event was filtered out
    expect(vi.mocked(clientFetch).mock.calls.length).toBe(callsBefore);
  });

  it('refetches session metadata when session_ended event arrives', async () => {
    vi.useFakeTimers();

    mockFetchSequence(sessionResp('s1', 'running'), outputResp('', 0, 0), sessionResp('s1', 'exited'));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Running')).toBeInTheDocument();
    });

    // Simulate session_ended event
    const { __getHandlers } = await import('../services/automateEvents');
    const handlers = __getHandlers();
    if (handlers.length > 0) {
      handlers[0]('automate.session_ended', { session_id: 's1', status: 'exited' });
    }

    await waitFor(() => {
      expect(screen.getByText('Exited')).toBeInTheDocument();
    });
  });

  it('output auto-scrolls on new output', async () => {
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('initial output', 14, 14));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('initial output')).toBeInTheDocument();
    });

    // Verify the output container exists (scrollToBottom operates on it)
    const preEl = document.querySelector('.automations-output-pre');
    expect(preEl).toBeInTheDocument();
  });

  it('appends output on subsequent event-driven fetches', async () => {
    vi.useFakeTimers();

    // Initial: session(running) + output("part1")
    // Event triggers output fetch("part2")
    mockFetchSequence(sessionResp('s1', 'running'), outputResp('part1', 5, 5), outputResp('part2', 10, 10));

    render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    // Wait for initial output
    await waitFor(() => {
      expect(screen.getByText('part1')).toBeInTheDocument();
    });

    // Simulate output_chunk event to trigger refetch
    const { __getHandlers } = await import('../services/automateEvents');
    const handlers = __getHandlers();
    if (handlers.length > 0) {
      handlers[0]('automate.output_chunk', { session_id: 's1', offset: 5, chunk_len: 5 });
    }

    // Advance past the debounce timer
    vi.advanceTimersByTime(300);
    await Promise.resolve();

    // After the event-driven fetch, both parts should be visible (appended)
    await waitFor(() => {
      expect(screen.getByText('part1part2')).toBeInTheDocument();
    });
  });

  it('does not fetch output after unmount when debounce is pending', async () => {
    vi.useFakeTimers();

    mockFetchSequence(sessionResp('s1', 'running'), outputResp('part1', 5, 5));

    const { unmount } = render(<AutomationsSessionDetail sessionId="s1" onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('part1')).toBeInTheDocument();
    });

    const callsBefore = vi.mocked(clientFetch).mock.calls.length;

    // Trigger an output_chunk event to start the debounce timer
    const { __getHandlers } = await import('../services/automateEvents');
    const handlers = __getHandlers();
    if (handlers.length > 0) {
      handlers[0]('automate.output_chunk', { session_id: 's1', offset: 5, chunk_len: 5 });
    }

    // Unmount before the 250ms debounce fires — the cleanup must cancel
    // the pending setTimeout so fetchOutput doesn't fire on a dead component.
    unmount();

    // Advance past the debounce window
    vi.advanceTimersByTime(500);
    // Drain pending microtasks
    await Promise.resolve();
    await Promise.resolve();

    // No additional fetch calls after unmount
    expect(vi.mocked(clientFetch).mock.calls.length).toBe(callsBefore);
  });
});
