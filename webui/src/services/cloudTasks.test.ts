/**
 * Tests for cloudTasks.ts — the Mode A platform task client used by the
 * escalation toast. Fetch is stubbed with real Response objects so header
 * parsing (X-Remaining-Task-Credits) is exercised against the actual Headers
 * implementation.
 */

import {
  CLOUD_TASK_TERMINAL_STATUSES,
  REMAINING_TASK_CREDITS_HEADER,
  getCloudTask,
  isTerminalCloudTaskStatus,
  pollCloudTask,
  submitCloudTask,
} from './cloudTasks';

/** Build a real Response with a JSON body and arbitrary status/headers. */
function jsonResponse(body: unknown, init?: { status?: number; headers?: Record<string, string> }) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
}

describe('cloudTasks', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /* ---- submitCloudTask ---- */

  it('submits POST /api/tasks with credentials and parses the credits header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        { task_id: 'task-1', status: 'pending' },
        {
          status: 201,
          headers: { [REMAINING_TASK_CREDITS_HEADER]: '7' },
        },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const result = await submitCloudTask({ repo_url: 'https://x/y', prompt: 'go' });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/tasks');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body as string)).toEqual({ repo_url: 'https://x/y', prompt: 'go' });
    expect(result.task.task_id).toBe('task-1');
    expect(result.task.status).toBe('pending');
    expect(result.remainingTaskCredits).toBe(7);
  });

  it('reports null remaining credits when the header is absent', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ task_id: 'task-2', status: 'pending' }, { status: 201 })),
    );
    const result = await submitCloudTask({ repo_url: 'r', prompt: 'p' });
    expect(result.remainingTaskCredits).toBeNull();
  });

  it('throws the platform error message on 402 (credits exhausted)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ error: 'No task credits remaining' }, { status: 402 })),
    );
    await expect(submitCloudTask({ repo_url: 'r', prompt: 'p' })).rejects.toThrow('No task credits remaining');
  });

  it('falls back to a status-bearing message when the error body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<html>bad gateway</html>', { status: 502 })));
    await expect(submitCloudTask({ repo_url: 'r', prompt: 'p' })).rejects.toThrow(/HTTP 502/);
  });

  it('falls back when the error body has no error field', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ message: 'unrelated' }, { status: 401 })));
    await expect(submitCloudTask({ repo_url: 'r', prompt: 'p' })).rejects.toThrow(/HTTP 401/);
  });

  it('throws TypeError when the 201 body has no task_id', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ status: 'pending' }, { status: 201 })));
    await expect(submitCloudTask({ repo_url: 'r', prompt: 'p' })).rejects.toThrow(TypeError);
  });

  /* ---- getCloudTask ---- */

  it('fetches /api/tasks/{encoded id} with credentials', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ task_id: 'a/b c', status: 'running', logs_url: 'https://l' }));
    vi.stubGlobal('fetch', fetchMock);

    const task = await getCloudTask('a/b c');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/tasks/a%2Fb%20c');
    expect(init.method).toBe('GET');
    expect(init.credentials).toBe('include');
    expect(task.task_id).toBe('a/b c');
    expect(task.status).toBe('running');
    expect(task.logs_url).toBe('https://l');
  });

  it('rejects on a non-2xx status fetch', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'task not found' }, { status: 404 })));
    await expect(getCloudTask('gone')).rejects.toThrow('task not found');
  });

  it('rejects defensively on a missing/absent task_id', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ status: 'running' })));
    await expect(getCloudTask('x')).rejects.toThrow(TypeError);
  });

  /* ---- terminal status helpers ---- */

  it('classifies terminal vs non-terminal statuses', () => {
    expect([...CLOUD_TASK_TERMINAL_STATUSES]).toEqual(['completed', 'failed', 'cancelled', 'timeout']);
    for (const s of CLOUD_TASK_TERMINAL_STATUSES) {
      expect(isTerminalCloudTaskStatus(s)).toBe(true);
    }
    expect(isTerminalCloudTaskStatus('pending')).toBe(false);
    expect(isTerminalCloudTaskStatus('running')).toBe(false);
  });

  /* ---- pollCloudTask ---- */

  it('polls until a terminal status and reports every tick', async () => {
    const statuses = ['pending', 'running', 'completed'];
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ task_id: 't', status: statuses[0] }))
      .mockResolvedValueOnce(jsonResponse({ task_id: 't', status: statuses[1] }))
      .mockResolvedValueOnce(jsonResponse({ task_id: 't', status: statuses[2] }));
    vi.stubGlobal('fetch', fetchMock);

    const ticks: string[] = [];
    const final = await pollCloudTask('t', {
      intervalMs: 1,
      onTick: (task) => ticks.push(task.status),
    });

    expect(final.status).toBe('completed');
    // onTick fires for every poll, including the terminal one.
    expect(ticks).toEqual(['pending', 'running', 'completed']);
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('works with onTick omitted', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ task_id: 't', status: 'failed' })));
    const final = await pollCloudTask('t', { intervalMs: 1 });
    expect(final.status).toBe('failed');
  });

  it('works with opts omitted entirely (default 2s interval)', async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi
        .fn()
        .mockImplementation(() => Promise.resolve(jsonResponse({ task_id: 't', status: 'completed' })));
      vi.stubGlobal('fetch', fetchMock);

      const promise = pollCloudTask('t');

      // Nothing fetched before the default interval elapses...
      expect(fetchMock).not.toHaveBeenCalled();

      // ...then the first poll resolves the terminal status immediately.
      await vi.advanceTimersByTimeAsync(2000);
      const final = await promise;

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(final.status).toBe('completed');
    } finally {
      vi.useRealTimers();
    }
  });

  it('keeps polling through non-terminal statuses a throwing onTick cannot break', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(jsonResponse({ task_id: 't', status: 'pending' }))
        .mockResolvedValueOnce(jsonResponse({ task_id: 't', status: 'cancelled' })),
    );
    const onTick = vi.fn(() => {
      throw new Error('listener exploded');
    });
    const final = await pollCloudTask('t', { intervalMs: 1, onTick });
    expect(final.status).toBe('cancelled');
    expect(onTick).toHaveBeenCalledTimes(2);
  });

  it('rejects with a timeout error when no terminal status arrives', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => Promise.resolve(jsonResponse({ task_id: 't', status: 'running' }))),
    );
    await expect(pollCloudTask('t', { intervalMs: 1, timeoutMs: 40 })).rejects.toThrow(/Cloud task timed out/);
  });

  it('propagates status request failures', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')));
    await expect(pollCloudTask('t', { intervalMs: 1 })).rejects.toThrow('network down');
  });

  it('clears its pending timer once settled (no dangling handles)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ task_id: 't', status: 'completed' })));
    await pollCloudTask('t', { intervalMs: 5, timeoutMs: 1000 });
    // If the loop kept scheduling, this suite would hang on open handles.
  });
});
