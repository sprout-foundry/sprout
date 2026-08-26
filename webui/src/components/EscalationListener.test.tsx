/**
 * Tests for EscalationListener.tsx — the browser-limitation toast. Focuses on
 * the Mode A "Run as cloud task" affordance: visibility gating on repoURL,
 * submit payload (escalation reason included in the prompt), inline progress
 * state, error state, and the unchanged Mode B CTA.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import { ESCALATION_TRIGGER_EVENT, type EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import { EscalationListener } from './EscalationListener';

/** Fire a blocking escalation trigger, the only kind the toast shows. */
function dispatchTrigger(overrides: Partial<EscalationTriggerEvent> = {}): void {
  window.dispatchEvent(
    new CustomEvent<EscalationTriggerEvent>(ESCALATION_TRIGGER_EVENT, {
      detail: {
        id: 'test-trigger',
        reason: 'git_push_failed',
        severity: 'blocking',
        message: 'Git push failed in the browser.',
        repoURL: 'https://github.com/acme/app',
        ...overrides,
      },
    }),
  );
}

/**
 * Fire a trigger inside act() — dispatchEvent is not a Testing Library util,
 * so React's state update from the listener would otherwise land outside act.
 */
function fireTrigger(overrides: Partial<EscalationTriggerEvent> = {}): void {
  act(() => dispatchTrigger(overrides));
}

/**
 * Advance fake timers inside act() — the polls they unleash resolve promises
 * whose state updates must land in act.
 */
async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

/** JSON Response helper (fresh body per call — Response bodies are one-shot). */
function jsonResponse(body: unknown, init?: { status?: number; headers?: Record<string, string> }) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
}

/** Mock fetch for the happy path: submit 201 then polled statuses in order. */
function mockTaskFetch(statuses: string[], submitStatus = 'pending') {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url === '/api/tasks') {
      return jsonResponse(
        { task_id: 'task-9', status: submitStatus },
        { status: 201, headers: { 'X-Remaining-Task-Credits': '3' } },
      );
    }
    const next = statuses.shift() ?? submitStatus;
    return jsonResponse({ task_id: 'task-9', status: next });
  });
}

// ── window.location mock ─────────────────────────────────────────────
// jsdom throws "Not implemented: navigation" when the Mode B CTA assigns
// window.location.href. Replace it with a spyable setter (same pattern as
// cloudProxyRoutes.sessionExpired.test.ts).
const originalLocation = window.location;
const hrefSetter = vi.fn();
let hrefValue = 'https://app.test.sprout.dev/';

beforeEach(() => {
  vi.stubGlobal('fetch', mockTaskFetch([]));
  hrefSetter.mockClear();
  hrefValue = 'https://app.test.sprout.dev/';
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: {
      ...originalLocation,
      get href() {
        return hrefValue;
      },
      set href(value: string) {
        hrefValue = value;
        hrefSetter(value);
      },
    },
  });
});

afterEach(() => {
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: originalLocation,
  });
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('EscalationListener', () => {
  /* ---- visibility ---- */

  it('renders nothing before any trigger', () => {
    render(createElement(EscalationListener));
    expect(screen.queryByText('Browser limitation reached')).toBeNull();
  });

  it('ignores info-severity triggers', () => {
    render(createElement(EscalationListener));
    fireTrigger({ severity: 'info' });
    expect(screen.queryByText('Browser limitation reached')).toBeNull();
  });

  it('renders the toast for a blocking trigger', () => {
    render(createElement(EscalationListener));
    fireTrigger();
    expect(screen.getByText('Browser limitation reached')).toBeInTheDocument();
    expect(screen.getByText('Git push failed in the browser.')).toBeInTheDocument();
  });

  /* ---- Mode A button gating ---- */

  it('hides the cloud-task CTA when the trigger has no repoURL', () => {
    render(createElement(EscalationListener));
    fireTrigger({ repoURL: undefined });
    expect(screen.queryByTestId('escalation-toast-cloud-task')).toBeNull();
    // Mode B CTA is unaffected.
    expect(screen.getByText('Start Full Workspace')).toBeInTheDocument();
  });

  it('shows the cloud-task CTA when the trigger has a repoURL', () => {
    render(createElement(EscalationListener));
    fireTrigger();
    expect(screen.getByTestId('escalation-toast-cloud-task')).toBeInTheDocument();
  });

  /* ---- submit + progress ---- */

  it('submits POST /api/tasks with the escalation reason in the prompt, then shows inline progress', async () => {
    vi.useFakeTimers();
    const fetchMock = mockTaskFetch(['completed']);
    vi.stubGlobal('fetch', fetchMock);
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));

    // Submit payload carries repo + derived prompt (reason included for context).
    // Flush microtasks rather than waitFor — its poll interval is faked.
    await advance(0);
    expect(fetchMock).toHaveBeenCalledWith('/api/tasks', expect.anything());
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/tasks');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(JSON.parse(init.body as string)).toEqual({
      repo_url: 'https://github.com/acme/app',
      prompt: 'Continue building this repository. Escalation reason: git_push_failed.',
    });

    // Progress renders immediately from the submit response (before first poll).
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task pending');
    expect(screen.getByTestId('escalation-toast-cloud-task-link')).toHaveAttribute('href', '/tasks/task-9');

    // First poll tick lands the terminal status.
    await advance(3000);
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task completed');
    // Once a task is in flight the CTA is replaced by the progress view, so a
    // second submit is impossible (the handler guards on it too).
    expect(screen.queryByTestId('escalation-toast-cloud-task')).toBeNull();
  });

  it('updates the status line from non-terminal poll ticks', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('fetch', mockTaskFetch(['running', 'completed']));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));

    await advance(0);
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task pending');

    await advance(3000);
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task running');

    await advance(3000);
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task completed');
  });

  it('renders the submit error in the toast body and keeps it dismissible', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ error: 'No task credits remaining' }, { status: 402 })),
    );
    render(createElement(EscalationListener));
    fireTrigger();

    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));

    expect(await screen.findByTestId('escalation-toast-cloud-task-error')).toHaveTextContent(
      'No task credits remaining',
    );
    // Submit failed before a task id existed, so no task link.
    expect(screen.queryByTestId('escalation-toast-cloud-task-link')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    expect(screen.queryByText('Browser limitation reached')).toBeNull();
  });

  it('keeps the task link when the poll times out', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('fetch', mockTaskFetch(['running']));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));

    await advance(0);
    expect(screen.getByTestId('escalation-toast-cloud-task-link')).toHaveAttribute('href', '/tasks/task-9');

    // Default poll timeout is 10 minutes.
    await advance(600000);
    expect(screen.getByTestId('escalation-toast-cloud-task-error')).toHaveTextContent(/Cloud task timed out/);
    expect(screen.getByTestId('escalation-toast-cloud-task-link')).toHaveAttribute('href', '/tasks/task-9');
  });

  /* ---- Mode B unchanged ---- */

  it('keeps the Mode B CTA wired to workspace creation', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ workspace_url: 'https://fly.sprout.dev/w/1' }, { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    render(createElement(EscalationListener));
    fireTrigger();

    fireEvent.click(screen.getByText('Start Full Workspace'));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/workspace/fly', expect.anything()));
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/fly');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      repo_url: 'https://github.com/acme/app',
      mode: 'build',
    });
    // jsdom cannot navigate; assert the assignment happened instead.
    await waitFor(() => expect(hrefSetter).toHaveBeenCalledWith('https://fly.sprout.dev/w/1'));
  });

  /* ---- double-submit + new trigger ---- */

  it('does not double-submit', async () => {
    // Keep the submit in flight so a second click could race it.
    let resolveSubmit!: (r: Response) => void;
    const submitGate = new Promise<Response>((resolve) => {
      resolveSubmit = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/tasks') return submitGate;
      return jsonResponse({ task_id: 'task-9', status: 'pending' });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(createElement(EscalationListener));
    fireTrigger();

    const button = screen.getByTestId('escalation-toast-cloud-task');
    fireEvent.click(button);
    fireEvent.click(button);

    // Button is disabled while the submit is in flight.
    expect(button).toBeDisabled();

    resolveSubmit(jsonResponse({ task_id: 'task-9', status: 'pending' }, { status: 201 }));
    await act(async () => {
      await Promise.resolve();
    });
    const submits = fetchMock.mock.calls.filter(([url]: [string, RequestInit]) => url === '/api/tasks');
    expect(submits).toHaveLength(1);
  });

  it('resets the cloud-task view when a new blocking trigger arrives', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('fetch', mockTaskFetch(['pending']));
    render(createElement(EscalationListener));
    fireTrigger();

    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));
    await advance(0);
    expect(screen.getByTestId('escalation-toast-cloud-task-status')).toBeInTheDocument();

    fireTrigger({ id: 'second', reason: 'vfs_quota_exceeded' });
    expect(screen.queryByTestId('escalation-toast-cloud-task-status')).toBeNull();
    expect(screen.getByTestId('escalation-toast-cloud-task')).toBeEnabled();
  });
});
