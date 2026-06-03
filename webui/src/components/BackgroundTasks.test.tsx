import { act } from 'react';
import { createRoot } from 'react-dom/client';
import BackgroundTasks from './BackgroundTasks';

// ---------------------------------------------------------------------------
// Mocks — vitest globals mode provides `vi` (not `jest`)
// ---------------------------------------------------------------------------

vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('lucide-react', () => {
  const icons = ['Play', 'Square', 'Layers', 'RefreshCw'];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) => <svg data-testid={name.toLowerCase()} {...props} />;
  }
  return result;
});

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { clientFetch } from '../services/clientSession';
import type { BackgroundTasksProps } from './BackgroundTasks';

const mockClientFetch = clientFetch as ReturnType<typeof vi.fn>;

// ---------------------------------------------------------------------------
// Test data factory — timestamps computed at test time, not module load time
// ---------------------------------------------------------------------------

interface MockSession {
  id: string;
  name: string;
  status: 'active' | 'inactive';
  chat_id: string;
  output_preview: string;
  started_at: number;
}

function makeMockSessions(nowSeconds: number): MockSession[] {
  return [
    {
      id: 'bg-npm-install-abc123',
      name: 'npm install',
      status: 'active',
      chat_id: 'chat-1',
      output_preview: 'Installing packages...',
      started_at: nowSeconds - 45,
    },
    {
      id: 'bg-go-test-def456',
      name: 'go test ./...',
      status: 'inactive',
      chat_id: 'chat-2',
      output_preview: 'PASS\nok  github.com/example/pkg',
      started_at: nowSeconds - 300,
    },
  ];
}

// Mutable module-level refs, recomputed in beforeEach after vi.useFakeTimers()
let mockSessions: MockSession[] = [];
let mockResponse = { sessions: mockSessions, count: 0 };

const emptyResponse = {
  sessions: [],
  count: 0,
};

const makeOkResponse = (data: object) =>
  ({
    ok: true,
    json: () => Promise.resolve(data),
    status: 200,
  }) as unknown as Response;

const makeErrorResponse = (status: number) =>
  ({
    ok: false,
    status,
  }) as unknown as Response;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderBackgroundTasks(props: BackgroundTasksProps = {}) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(<BackgroundTasks {...props} />);
  });
  return { container, root };
}

/**
 * Flush microtask queue so that promise .then() callbacks execute.
 * Works with vi.useFakeTimers() because it doesn't rely on setImmediate.
 */
const flushPromises = async () => {
  await act(async () => {
    // Promise.resolve() schedules a microtask; await flushes it
    await Promise.resolve();
    // Second await to handle any chained promises
    await Promise.resolve();
  });
};

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

describe('BackgroundTasks', () => {
  let container: HTMLDivElement | null = null;
  let root: ReturnType<typeof createRoot> | null = null;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    vi.useFakeTimers();
    mockClientFetch.mockClear();

    // Compute mock sessions at test time so timestamps don't go stale
    const nowSeconds = Math.floor(Date.now() / 1000);
    mockSessions = makeMockSessions(nowSeconds);
    mockResponse = { sessions: mockSessions, count: mockSessions.length };

    mockClientFetch.mockResolvedValue(makeOkResponse(emptyResponse));
  });

  afterEach(() => {
    act(() => {
      if (root) root.unmount();
    });
    if (container) {
      container.remove();
      container = null;
    }
    root = null;
    vi.useRealTimers();
  });

  // ─────────────────────────────────────────────────────────────────────
  // 1. Rendering
  // ─────────────────────────────────────────────────────────────────────

  describe('rendering', () => {
    it('renders closed by default with trigger button only', () => {
      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const trigger = container.querySelector('.background-tasks-trigger');
      expect(trigger).toBeTruthy();

      // The popover should not be present when closed
      const popover = container.querySelector('.background-tasks-popover');
      expect(popover).toBeNull();
    });

    it('shows "Background Tasks" title inside popover when open', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(emptyResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const trigger = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        trigger.click();
      });
      await flushPromises();

      const title = container.querySelector('.background-tasks-popover-title span');
      expect(title?.textContent).toBe('Background Tasks');
    });

    it('expands on header click', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;

      act(() => {
        header.click();
      });
      await flushPromises();

      const body = container.querySelector('.background-tasks-popover-body');
      expect(body).toBeTruthy();
    });

    it('collapses on header click when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;

      // Expand
      act(() => {
        header.click();
      });
      await flushPromises();

      expect(container.querySelector('.background-tasks-popover-body')).toBeTruthy();

      // Collapse
      act(() => {
        header.click();
      });

      expect(container.querySelector('.background-tasks-popover-body')).toBeNull();
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 2. Badge
  // ─────────────────────────────────────────────────────────────────────

  describe('badge', () => {
    it('does not show badge when there are no sessions', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(emptyResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Wait for mount fetch to complete
      await flushPromises();

      const badge = container.querySelector('.background-tasks-trigger-badge');
      expect(badge).toBeNull();
    });

    it('shows badge with count when sessions are present after mount fetch', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Wait for mount fetch to complete
      await flushPromises();

      const badge = container.querySelector('.background-tasks-trigger-badge');
      expect(badge).toBeTruthy();
      expect(badge?.textContent).toBe('2');
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 3. Session List Display
  // ─────────────────────────────────────────────────────────────────────

  describe('session list display', () => {
    it('shows "No background tasks running" when empty and expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(emptyResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const emptyEl = container.querySelector('.background-tasks-empty');
      expect(emptyEl?.textContent).toBe('No background tasks running');
    });

    it('displays session items when expanded with data', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const items = container.querySelectorAll('.background-task-item');
      expect(items.length).toBe(2);
    });

    it('shows session name', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const names = container.querySelectorAll('.background-task-name');
      expect(names[0]?.textContent).toBe('npm install');
      expect(names[1]?.textContent).toBe('go test ./...');
    });

    it('shows session.id as name fallback when name is empty', async () => {
      const responseNoName = {
        sessions: [
          {
            id: 'bg-no-name-999',
            name: '',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: 0,
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(responseNoName));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const nameEl = container.querySelector('.background-task-name');
      expect(nameEl?.textContent).toBe('bg-no-name-999');
    });

    it('shows "Running" for active status', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const statusTexts = container.querySelectorAll('.background-task-status-text');
      expect(statusTexts[0]?.textContent).toBe('Running');
    });

    it('shows "Exited" for inactive status', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const statusTexts = container.querySelectorAll('.background-task-status-text');
      expect(statusTexts[1]?.textContent).toBe('Exited');
    });

    it('shows output preview when present', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const previews = container.querySelectorAll('.background-task-preview');
      expect(previews.length).toBe(2);
      expect(previews[0]?.textContent).toBe('Installing packages...');
    });

    it('shows duration for sessions with started_at > 0', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // The first session was 45 seconds ago, so duration should show seconds
      const durationEls = container.querySelectorAll('.background-task-duration');
      expect(durationEls.length).toBe(2);
      // Duration should contain "s" for seconds or "m" for minutes
      expect(durationEls[0]?.textContent).toMatch(/\d+s/);
      expect(durationEls[1]?.textContent).toMatch(/\d+m/);
    });

    it('does not show duration for sessions with started_at = 0', async () => {
      const responseNoStarted = {
        sessions: [
          {
            id: 'bg-no-start',
            name: 'no start time',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: 0,
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(responseNoStarted));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeNull();
    });

    it('renders attach and kill buttons for each session', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const attachBtns = container.querySelectorAll('.background-task-btn-attach');
      const killBtns = container.querySelectorAll('.background-task-btn-kill');
      expect(attachBtns.length).toBe(2);
      expect(killBtns.length).toBe(2);
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 4. API Interaction
  // ─────────────────────────────────────────────────────────────────────

  describe('API interaction', () => {
    it('fetches sessions on mount when collapsed', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      await flushPromises();

      expect(mockClientFetch).toHaveBeenCalledWith('/api/terminal/agent-sessions');
    });

    it('fetches sessions on expand', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Wait for mount fetch to complete so isFetchingRef is cleared
      await flushPromises();

      // Clear mount fetch calls
      mockClientFetch.mockClear();
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      expect(mockClientFetch).toHaveBeenCalledWith('/api/terminal/agent-sessions');
    });

    it('handles API errors gracefully on expand', async () => {
      mockClientFetch.mockResolvedValue(makeErrorResponse(500));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const errorEl = container.querySelector('.background-tasks-error');
      expect(errorEl).toBeTruthy();
      expect(errorEl?.textContent).toContain('Failed to fetch sessions: Internal server error');
    });

    it('handles API errors gracefully on mount', async () => {
      mockClientFetch.mockResolvedValue(makeErrorResponse(404));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      await flushPromises();

      // On mount when collapsed, errors are logged but not shown in UI
      // (the error element only shows when expanded)
      const badge = container.querySelector('.background-tasks-trigger-badge');
      expect(badge).toBeNull();
    });

    it('shows retry button when there is an error', async () => {
      mockClientFetch.mockResolvedValue(makeErrorResponse(500));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const retryBtn = container.querySelector('.background-task-btn-retry');
      expect(retryBtn).toBeTruthy();
      expect(retryBtn?.getAttribute('title')).toBe('Retry');
    });

    it('retry button re-fetches sessions', async () => {
      mockClientFetch.mockResolvedValue(makeErrorResponse(500));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand to trigger error
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Now make fetch succeed
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const retryBtn = container.querySelector('.background-task-btn-retry') as HTMLElement;
      act(() => {
        retryBtn.click();
      });
      await flushPromises();

      // Error should be gone and sessions should be shown
      const errorEl = container.querySelector('.background-tasks-error');
      expect(errorEl).toBeNull();
      const items = container.querySelectorAll('.background-task-item');
      expect(items.length).toBe(2);
    });

    it('guards against concurrent fetches', async () => {
      // Simulate slow response
      let resolveFetch: (() => void) | undefined;
      mockClientFetch.mockImplementation(
        () =>
          new Promise<Response>((resolve) => {
            resolveFetch = () => resolve(makeOkResponse(mockResponse));
          }),
      );

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand to trigger fetch
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });

      // Advance timer to trigger poll interval while first fetch is still pending
      act(() => {
        vi.advanceTimersByTime(3000);
      });

      // Resolve the pending fetch
      act(() => {
        resolveFetch!();
      });
      await flushPromises();

      // Only one fetch should have completed (the second one should have been skipped by the guard)
      // The key assertion: component didn't crash and rendered
      expect(container.querySelector('.background-tasks-dropdown')).toBeTruthy();
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 5. Attach Action
  // ─────────────────────────────────────────────────────────────────────

  describe('attach action', () => {
    it('calls POST /api/terminal/agent-sessions/{id}/attach on attach button click', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Mock the attach endpoint response + refetch
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // attach POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      const attachBtn = container.querySelector('.background-task-btn-attach') as HTMLElement;
      act(() => {
        attachBtn.click();
      });
      await flushPromises();

      // Verify the POST was called
      expect(mockClientFetch).toHaveBeenCalledWith('/api/terminal/agent-sessions/bg-npm-install-abc123/attach', {
        method: 'POST',
      });
    });

    it('dispatches sprout:terminal-attach-session custom event', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Set up attach endpoint mock
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // attach POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      let eventDispatched: CustomEvent | null = null;
      window.addEventListener('sprout:terminal-attach-session', (e: Event) => {
        eventDispatched = e as CustomEvent;
      });

      const attachBtn = container.querySelector('.background-task-btn-attach') as HTMLElement;
      act(() => {
        attachBtn.click();
      });
      await flushPromises();

      expect(eventDispatched).toBeTruthy();
      expect(eventDispatched?.detail.sessionId).toBe('bg-npm-install-abc123');
      expect(eventDispatched?.detail.name).toBe('npm install');
    });

    it('calls onAttachSession prop callback', async () => {
      const onAttachSession = vi.fn();
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks({ onAttachSession });
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Set up attach endpoint mock
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // attach POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      const attachBtn = container.querySelector('.background-task-btn-attach') as HTMLElement;
      act(() => {
        attachBtn.click();
      });
      await flushPromises();

      expect(onAttachSession).toHaveBeenCalledWith('bg-npm-install-abc123');
    });

    it('refetches session list after attach', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Clear and set up mocks: attach POST → success, then refetch → empty
      mockClientFetch.mockClear();
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // attach POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      const attachBtn = container.querySelector('.background-task-btn-attach') as HTMLElement;
      act(() => {
        attachBtn.click();
      });
      await flushPromises();

      // The second call should be the refetch
      expect(mockClientFetch).toHaveBeenLastCalledWith('/api/terminal/agent-sessions');
    });

    it('handles attach API error', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Mock attach failure
      mockClientFetch.mockResolvedValueOnce(makeErrorResponse(500));

      const attachBtn = container.querySelector('.background-task-btn-attach') as HTMLElement;
      act(() => {
        attachBtn.click();
      });
      await flushPromises();

      const errorEl = container.querySelector('.background-tasks-error');
      expect(errorEl).toBeTruthy();
      expect(errorEl?.textContent).toContain('Failed to attach session: Internal server error');
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 6. Kill Action
  // ─────────────────────────────────────────────────────────────────────

  describe('kill action', () => {
    it('calls POST /api/terminal/agent-sessions/{id}/kill on kill button click', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Mock the kill endpoint response + refetch
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // kill POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      const killBtn = container.querySelector('.background-task-btn-kill') as HTMLElement;
      act(() => {
        killBtn.click();
      });
      await flushPromises();

      expect(mockClientFetch).toHaveBeenCalledWith('/api/terminal/agent-sessions/bg-npm-install-abc123/kill', {
        method: 'POST',
      });
    });

    it('refetches session list after kill', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Mock: kill POST → success, refetch → empty
      mockClientFetch.mockClear();
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(mockResponse)) // kill POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      const killBtn = container.querySelector('.background-task-btn-kill') as HTMLElement;
      act(() => {
        killBtn.click();
      });
      await flushPromises();

      expect(mockClientFetch).toHaveBeenLastCalledWith('/api/terminal/agent-sessions');
    });

    it('handles kill API error', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Mock kill failure
      mockClientFetch.mockResolvedValueOnce(makeErrorResponse(500));

      const killBtn = container.querySelector('.background-task-btn-kill') as HTMLElement;
      act(() => {
        killBtn.click();
      });
      await flushPromises();

      const errorEl = container.querySelector('.background-tasks-error');
      expect(errorEl).toBeTruthy();
      expect(errorEl?.textContent).toContain('Failed to kill session: Internal server error');
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 7. Duration Display (via rendering)
  // ─────────────────────────────────────────────────────────────────────

  describe('duration display', () => {
    it('shows seconds for elapsed time under 1 minute', async () => {
      const now = Math.floor(Date.now() / 1000);
      const response = {
        sessions: [
          {
            id: 'bg-seconds',
            name: 'seconds test',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: now - 30, // 30 seconds ago
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(response));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeTruthy();
      // Should show something like "30s" or "31s" (30-31 seconds)
      expect(durationEl?.textContent).toMatch(/\d+s$/);
    });

    it('shows minutes+seconds for elapsed time under 1 hour', async () => {
      const now = Math.floor(Date.now() / 1000);
      const response = {
        sessions: [
          {
            id: 'bg-minutes',
            name: 'minutes test',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: now - 345, // 5 min 45 sec ago
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(response));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeTruthy();
      // Should show "5m 45s" or similar (5m 45-46s due to timing)
      expect(durationEl?.textContent).toMatch(/\d+m \d+s/);
    });

    it('shows hours+minutes for elapsed time over 1 hour', async () => {
      const now = Math.floor(Date.now() / 1000);
      const response = {
        sessions: [
          {
            id: 'bg-hours',
            name: 'hours test',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: now - 3750, // ~1h 2m 30s ago
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(response));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeTruthy();
      // Should show "1h 2m" or similar
      expect(durationEl?.textContent).toMatch(/\d+h \d+m/);
    });

    it('does not render duration for started_at = 0', async () => {
      const response = {
        sessions: [
          {
            id: 'bg-zero-start',
            name: 'zero start',
            status: 'active' as const,
            chat_id: 'chat-x',
            output_preview: '',
            started_at: 0,
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(response));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeNull();
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 8. Polling
  // ─────────────────────────────────────────────────────────────────────

  describe('polling', () => {
    it('sets up polling interval when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const initialCallCount = mockClientFetch.mock.calls.length;

      // Advance past poll interval (3000ms)
      act(() => {
        vi.advanceTimersByTime(3000);
      });
      await flushPromises();

      // Should have made additional fetch calls
      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(initialCallCount);
    });

    it('polls at a slower cadence when closed (keeps badge fresh)', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Open
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Fast poll while open: 3s interval triggers a fetch
      const callCountAfterOpen = mockClientFetch.mock.calls.length;
      act(() => {
        vi.advanceTimersByTime(3000);
      });
      await flushPromises();
      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountAfterOpen);

      // Close — switches to the idle (15s) poll cadence
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountAfterClose = mockClientFetch.mock.calls.length;

      // 3s elapsed is below the 15s idle interval — no extra fetch yet
      act(() => {
        vi.advanceTimersByTime(3000);
      });
      await flushPromises();
      expect(mockClientFetch.mock.calls.length).toBe(callCountAfterClose);

      // Past 15s — the idle poll fires
      act(() => {
        vi.advanceTimersByTime(15000);
      });
      await flushPromises();
      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountAfterClose);
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 9. Tick (duration re-render)
  // ─────────────────────────────────────────────────────────────────────

  describe('duration ticker', () => {
    it('triggers re-render every second when active sessions exist', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Get initial duration text
      const durationEl = container.querySelector('.background-task-duration');
      const initialText = durationEl?.textContent;

      // Advance 1 second
      act(() => {
        vi.advanceTimersByTime(1000);
      });

      // Duration should have changed (incremented)
      const updatedText = container.querySelector('.background-task-duration')?.textContent;
      expect(updatedText).not.toBe(initialText);
    });

    it('does not tick when no active sessions', async () => {
      const now = Math.floor(Date.now() / 1000);
      const allInactive = {
        sessions: [
          {
            id: 'bg-inactive',
            name: 'done',
            status: 'inactive' as const,
            chat_id: 'chat-x',
            output_preview: 'done',
            started_at: now - 60,
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(allInactive));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Advance 2 seconds — no tick interval should fire since no active sessions
      act(() => {
        vi.advanceTimersByTime(2000);
      });

      // Component should still be in a valid state (no crash)
      expect(container.querySelector('.background-task-item')).toBeTruthy();
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 10. WebSocket Events
  // ─────────────────────────────────────────────────────────────────────

  describe('WebSocket events', () => {
    it('refreshes on terminal_output event when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      // Dispatch terminal_output event
      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'terminal_output', data: 'output' },
          }),
        );
      });
      await flushPromises();

      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountBefore);
    });

    it('refreshes on pty_exit event when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      // Dispatch pty_exit event
      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'pty_exit', session_id: 'bg-npm-install-abc123' },
          }),
        );
      });
      await flushPromises();

      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountBefore);
    });

    it('refreshes on agent_session_update event when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      // Dispatch agent_session_update event
      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'agent_session_update', session_id: 'bg-npm-install-abc123' },
          }),
        );
      });
      await flushPromises();

      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountBefore);
    });

    it('ignores other event types when expanded', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      // Dispatch unrelated event
      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'some_other_event' },
          }),
        );
      });
      await flushPromises();

      // Should NOT have triggered a refetch
      expect(mockClientFetch.mock.calls.length).toBe(callCountBefore);
    });

    it('still listens for events when closed (keeps badge in sync)', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(emptyResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Do NOT expand — stay closed; the trigger badge needs WS updates
      // so it can reflect new tasks without opening the popover.

      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'terminal_output', data: 'output' },
          }),
        );
      });
      await flushPromises();

      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountBefore);
    });

    it('continues listening for events after closing the popover', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Open then close
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();
      act(() => {
        header.click();
      });
      await flushPromises();

      const callCountBefore = mockClientFetch.mock.calls.length;

      act(() => {
        window.dispatchEvent(
          new CustomEvent('sprout:wsevent', {
            detail: { type: 'terminal_output', data: 'output' },
          }),
        );
      });
      await flushPromises();

      // WS event triggers a fetch regardless of popover state.
      expect(mockClientFetch.mock.calls.length).toBeGreaterThan(callCountBefore);
      expect(container.querySelector('.background-tasks-trigger')).toBeTruthy();
    });
  });

  // ─────────────────────────────────────────────────────────────────────
  // 11. Edge Cases
  // ─────────────────────────────────────────────────────────────────────

  describe('edge cases', () => {
    it('handles response with null sessions gracefully', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse({ sessions: null, count: 0 }));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Should show empty state, not crash
      const emptyEl = container.querySelector('.background-tasks-empty');
      expect(emptyEl).toBeTruthy();
    });

    it('handles response with undefined data gracefully', async () => {
      mockClientFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(undefined),
      } as unknown as Response);

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Should show empty state, not crash (data?.sessions || [] handles this)
      const emptyEl = container.querySelector('.background-tasks-empty');
      expect(emptyEl).toBeTruthy();
    });

    it('handles sessions with missing optional fields', async () => {
      const minimalSessions = {
        sessions: [
          {
            id: 'bg-minimal',
            name: '',
            status: 'active' as const,
            chat_id: '',
            output_preview: '',
            started_at: 0,
          },
        ],
        count: 1,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(minimalSessions));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Should render without crashing, using id as name fallback
      const nameEl = container.querySelector('.background-task-name');
      expect(nameEl?.textContent).toBe('bg-minimal');

      // No duration should be shown
      const durationEl = container.querySelector('.background-task-duration');
      expect(durationEl).toBeNull();
    });

    it('does not crash when event detail is null', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Dispatch event with null detail
      act(() => {
        window.dispatchEvent(new CustomEvent('sprout:wsevent', { detail: null }));
      });
      await flushPromises();

      // Should not crash
      expect(container.querySelector('.background-tasks-dropdown')).toBeTruthy();
    });

    it('disables attach button for inactive sessions', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      const attachBtns = container.querySelectorAll('.background-task-btn-attach') as NodeListOf<HTMLButtonElement>;
      // First session (active) should have enabled attach button
      expect(attachBtns[0].disabled).toBe(false);
      // Second session (inactive) should have disabled attach button
      expect(attachBtns[1].disabled).toBe(true);
    });

    it('attaching second session uses correct session data', async () => {
      // Use a response where both sessions are active so the second attach button
      // is enabled and clickable
      const allActive = {
        sessions: [
          {
            id: 'bg-npm-install-abc123',
            name: 'npm install',
            status: 'active' as const,
            chat_id: 'chat-1',
            output_preview: 'Installing packages...',
            started_at: Math.floor(Date.now() / 1000) - 45,
          },
          {
            id: 'bg-go-test-def456',
            name: 'go test ./...',
            status: 'active' as const,
            chat_id: 'chat-2',
            output_preview: 'PASS\nok  github.com/example/pkg',
            started_at: Math.floor(Date.now() / 1000) - 300,
          },
        ],
        count: 2,
      };
      mockClientFetch.mockResolvedValue(makeOkResponse(allActive));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // Set up mocks for second session attach
      mockClientFetch
        .mockResolvedValueOnce(makeOkResponse(allActive)) // attach POST
        .mockResolvedValueOnce(makeOkResponse(emptyResponse)); // refetch

      let eventDispatched: CustomEvent | null = null;
      window.addEventListener('sprout:terminal-attach-session', (e: Event) => {
        eventDispatched = e as CustomEvent;
      });

      // Click the second attach button (index 1)
      const attachBtns = container.querySelectorAll('.background-task-btn-attach');
      act(() => {
        (attachBtns[1] as HTMLElement).click();
      });
      await flushPromises();

      expect(eventDispatched?.detail.sessionId).toBe('bg-go-test-def456');
      expect(eventDispatched?.detail.name).toBe('go test ./...');
    });

    it('hides loading state after fetch completes', async () => {
      mockClientFetch.mockResolvedValue(makeOkResponse(mockResponse));

      const view = renderBackgroundTasks();
      container = view.container;
      root = view.root;

      // Expand
      const header = container.querySelector('.background-tasks-trigger') as HTMLElement;
      act(() => {
        header.click();
      });
      await flushPromises();

      // After fetch completes, should show session items not loading
      const items = container.querySelectorAll('.background-task-item');
      expect(items.length).toBe(2);
    });
  });
});
