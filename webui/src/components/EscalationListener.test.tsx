/**
 * Tests for EscalationListener.tsx — the browser-limitation toast. Covers the
 * ETH-2 "Run in cloud container" action end-to-end (open → push → run → pull
 * → apply → finish, with the always-finish guarantee), its gating and error
 * mapping, plus regressions for the existing Mode A / Mode B affordances.
 *
 * The txn client and the browser-git bridge are mocked so the component's
 * orchestration is what's under test; the builders themselves are covered by
 * cloudTxn.test.ts.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import { ESCALATION_TRIGGER_EVENT, type EscalationTriggerEvent } from '../hooks/useEscalationTriggers';
import {
  CloudTxnError,
  createTxn,
  resolveTxnWorkspace,
  txnFinish,
  txnPull,
  txnPush,
  txnRun,
  type TxnManifest,
  type TxnRunResult,
} from '../services/cloudTxn';
import { EscalationListener } from './EscalationListener';

vi.mock('../services/cloudTxn', async () => {
  // Real builders (buildPushManifest/applyPullManifest/CloudTxnError) with
  // only the network lifecycle mocked — the component's orchestration is
  // what's under test here.
  const actual = await vi.importActual<Record<string, unknown>>('../services/cloudTxn');
  return {
    ...actual,
    resolveTxnWorkspace: vi.fn(),
    createTxn: vi.fn(),
    txnPush: vi.fn(),
    txnRun: vi.fn(),
    txnPull: vi.fn(),
    txnFinish: vi.fn(),
  };
});

const { getBrowserGitVfsBridge, gitLog, gitStatus } = vi.hoisted(() => ({
  getBrowserGitVfsBridge: vi.fn(),
  gitLog: vi.fn(),
  gitStatus: vi.fn(),
}));

vi.mock('../services/browserGit', () => ({
  getBrowserGitVfsBridge,
  gitLog,
  gitStatus,
}));

const writtenVfs: Array<{ path: string; content: string }> = [];
const deletedVfs: string[] = [];

/** Fire a blocking escalation trigger, the only kind the toast shows. */
function dispatchTrigger(overrides: Partial<EscalationTriggerEvent> = {}): void {
  window.dispatchEvent(
    new CustomEvent<EscalationTriggerEvent>(ESCALATION_TRIGGER_EVENT, {
      detail: {
        id: 'test-trigger',
        reason: 'command_unavailable_in_browser',
        severity: 'blocking',
        message: '“go build ./...” needs a real runtime.',
        repoURL: 'https://github.com/acme/app',
        command: 'go build ./...',
        ...overrides,
      },
    }),
  );
}

/** Fire a trigger inside act() so its state update lands in act. */
function fireTrigger(overrides: Partial<EscalationTriggerEvent> = {}): void {
  act(() => dispatchTrigger(overrides));
}

const RUN_RESULT: TxnRunResult = {
  stdout: 'built ok\n',
  stderr: '',
  exit_code: 0,
  duration_ms: 1500,
  timed_out: false,
  truncated: false,
};

/** The pull manifest the mocked txnPull resolves with. */
const PULL_MANIFEST: TxnManifest = {
  base: { git_sha: 'abc123', client: 'container' },
  files: [{ path: 'dist/app', content_base64: 'YmluYXJ5', size: 6, mode: '0644' }],
  deletes: ['tmp.log'],
  truncated: false,
  skipped: [],
};

/** JSON Response helper (fresh body per call — Response bodies are one-shot). */
function jsonResponse(body: unknown, init?: { status?: number; headers?: Record<string, string> }) {
  return new Response(JSON.stringify(body), {
    status: init?.status ?? 200,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
}

/** Standard happy-path txn mocks (individual tests override with mockResolvedValueOnce). */
function mockHappyTxn() {
  vi.mocked(resolveTxnWorkspace).mockResolvedValue({ workspaceId: 'ws-1', created: false });
  vi.mocked(createTxn).mockResolvedValue({ txn_id: 'txn-9', status: 'push' });
  vi.mocked(txnPush).mockResolvedValue({ applied: 2, deleted: 0, skipped: [], status: 'ok' });
  vi.mocked(txnRun).mockResolvedValue(RUN_RESULT);
  vi.mocked(txnPull).mockResolvedValue(PULL_MANIFEST);
  vi.mocked(txnFinish).mockResolvedValue({ status: 'done', txn_duration_seconds: 3, stop_initiated: true });
  // Real builders against a fake bridge that reports two dirty files.
  getBrowserGitVfsBridge.mockReturnValue({
    readVfsFiles: async () => [
      { path: 'main.go', content: 'package main' },
      { path: 'other.go', content: 'x' },
    ],
    writeVfsFiles: async (files: Array<{ path: string; content: string }>) => {
      writtenVfs.push(...files);
    },
    deleteVfsFiles: async (paths: string[]) => {
      deletedVfs.push(...paths);
    },
  });
  gitStatus.mockResolvedValue({
    staged: [],
    unstaged: [{ path: 'main.go', status: 'modified', staged: false }],
    untracked: [],
  });
  gitLog.mockResolvedValue([{ hash: 'h', message: 'm', author: 'a', date: 'd' }]);
}

/** Flush pending promise chains. */
async function flush(times = 8): Promise<void> {
  for (let i = 0; i < times; i += 1) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

// ── window.location mock ─────────────────────────────────────────────
const originalLocation = window.location;
const hrefSetter = vi.fn();
let hrefValue = 'https://app.test.sprout.dev/';

beforeEach(() => {
  writtenVfs.length = 0;
  deletedVfs.length = 0;
  mockHappyTxn();
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => jsonResponse({})),
  );
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
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe('EscalationListener — ETH-2 txn action', () => {
  it('shows the txn CTA only when the trigger carries a command and repoURL', () => {
    render(createElement(EscalationListener));

    fireTrigger({ command: undefined });
    expect(screen.queryByTestId('escalation-toast-txn')).toBeNull();

    fireTrigger({ repoURL: undefined });
    expect(screen.queryByTestId('escalation-toast-txn')).toBeNull();

    fireTrigger();
    expect(screen.getByTestId('escalation-toast-txn')).toBeInTheDocument();
    expect(screen.getByTestId('escalation-toast-txn')).toHaveTextContent('Run in cloud container');
  });

  it('runs open → push → run → pull → apply → finish and renders the result', async () => {
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));

    await flush();

    expect(resolveTxnWorkspace).toHaveBeenCalledWith('https://github.com/acme/app');
    expect(createTxn).toHaveBeenCalledWith('ws-1');
    expect(txnPush).toHaveBeenCalledWith(
      'ws-1',
      'txn-9',
      expect.objectContaining({ base: { git_sha: '', client: 'wasm' } }),
    );
    // Only the dirty file is pushed (other.go is clean).
    const manifest = vi.mocked(txnPush).mock.calls[0][2];
    expect(manifest.files.map((f) => f.path)).toEqual(['main.go']);
    expect(txnRun).toHaveBeenCalledWith('ws-1', 'txn-9', 'go build ./...', 600);

    // The pulled file landed in the VFS via the bridge and the delete was applied.
    expect(writtenVfs).toEqual([{ path: 'dist/app', content: 'binary' }]);
    expect(deletedVfs).toEqual(['tmp.log']);

    // finish is the machine-stop guarantee.
    expect(txnFinish).toHaveBeenCalledTimes(1);
    expect(txnFinish).toHaveBeenCalledWith('ws-1', 'txn-9');

    const result = await screen.findByTestId('escalation-toast-txn-result');
    expect(result).toHaveTextContent('exit 0');
    expect(result).toHaveTextContent('1.5s');
    expect(result).toHaveTextContent('built ok');
    expect(screen.getByTestId('escalation-toast-txn-pulled')).toHaveTextContent('1 file pulled back');
  });

  it('falls back to pushing every VFS file when git has no commits', async () => {
    gitStatus.mockResolvedValue({ staged: [], unstaged: [], untracked: [] });
    gitLog.mockResolvedValue([]);
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    const manifest = vi.mocked(txnPush).mock.calls[0][2];
    expect(manifest.files.map((f) => f.path)).toEqual(['main.go', 'other.go']);
  });

  it('pushes nothing when the repo is clean', async () => {
    gitStatus.mockResolvedValue({ staged: [], unstaged: [], untracked: [] });
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(vi.mocked(txnPush).mock.calls[0][2].files).toEqual([]);
  });

  it('still finishes when the run phase fails, and names the failing side', async () => {
    vi.mocked(txnRun).mockRejectedValue(new Error('container died'));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-error')).toHaveTextContent(
      /Running command failed: container died/,
    );
    // No result view, but the txn WAS finished — no leaked machine.
    expect(screen.queryByTestId('escalation-toast-txn-result')).toBeNull();
    expect(txnFinish).toHaveBeenCalledWith('ws-1', 'txn-9');
  });

  it('still finishes when the pull/apply phase fails', async () => {
    vi.mocked(txnPull).mockRejectedValue(new CloudTxnError('pull failed', 500));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-error')).toHaveTextContent(/Pulling results back failed/);
    expect(txnFinish).toHaveBeenCalledWith('ws-1', 'txn-9');
    expect(writtenVfs).toEqual([]);
  });

  it('maps a 409 to the friendly busy message', async () => {
    vi.mocked(createTxn).mockRejectedValue(new CloudTxnError('a transaction is already open', 409));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-error')).toHaveTextContent(
      'another transaction is running, try again shortly',
    );
  });

  it('maps a 402 to a credits message', async () => {
    vi.mocked(resolveTxnWorkspace).mockRejectedValue(new CloudTxnError('Overage spending cap reached.', 402));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-error')).toHaveTextContent(
      'Not enough credits: Overage spending cap reached.',
    );
  });

  it('maps a 503 to a friendly unavailable message', async () => {
    vi.mocked(createTxn).mockRejectedValue(new CloudTxnError('workspace not running', 503));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-error')).toHaveTextContent(
      'Cloud workspace is unavailable right now — try again shortly.',
    );
  });

  it('surfaces a failed finish as a warning without losing the result', async () => {
    vi.mocked(txnFinish).mockRejectedValue(new Error('stop timeout'));
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    expect(await screen.findByTestId('escalation-toast-txn-result')).toHaveTextContent('exit 0');
    expect(screen.getByTestId('escalation-toast-txn-warning')).toHaveTextContent(/stop failed/);
    // The finally-block retry also ran (and also failed) — never silent success.
    expect(txnFinish).toHaveBeenCalled();
  });

  it('renders a timed-out run with its flag and skip counts', async () => {
    vi.mocked(txnRun).mockResolvedValue({ ...RUN_RESULT, exit_code: 124, timed_out: true, duration_ms: 600000 });
    vi.mocked(txnPull).mockResolvedValue({
      ...PULL_MANIFEST,
      files: [],
      deletes: [],
      skipped: [{ path: 'big.bin', reason: 'exceeds_per_file_cap' }],
      truncated: true,
    });
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();

    const result = await screen.findByTestId('escalation-toast-txn-result');
    expect(result).toHaveTextContent('exit 124');
    expect(result).toHaveTextContent('timed out');
    expect(screen.getByTestId('escalation-toast-txn-pulled')).toHaveTextContent('0 files pulled back');
    expect(screen.getByTestId('escalation-toast-txn-skipped')).toHaveTextContent('1 file skipped');
  });

  it('does not double-submit while a txn is in flight', async () => {
    let resolveCreate!: (value: { txn_id: string; status: string }) => void;
    vi.mocked(createTxn).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve;
        }),
    );
    render(createElement(EscalationListener));
    fireTrigger();

    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    // The CTA is replaced by the txn progress view, so a second click is
    // impossible (the handler guards on `txn` too).
    expect(screen.queryByTestId('escalation-toast-txn')).toBeNull();
    expect(screen.getByTestId('escalation-toast-txn-status')).toBeInTheDocument();

    // Let the open call reach createTxn before unblocking it.
    await flush(2);
    resolveCreate({ txn_id: 'txn-9', status: 'push' });
    await flush();
    expect(createTxn).toHaveBeenCalledTimes(1);
  });

  it('resets the txn view when a new blocking trigger arrives', async () => {
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();
    expect(await screen.findByTestId('escalation-toast-txn-result')).toBeInTheDocument();

    fireTrigger({ id: 'second', command: 'cargo build' });
    expect(screen.queryByTestId('escalation-toast-txn-result')).toBeNull();
    expect(screen.getByTestId('escalation-toast-txn')).toBeEnabled();
  });
});

describe('EscalationListener — Mode A/B regressions', () => {
  it('renders nothing before any trigger', () => {
    render(createElement(EscalationListener));
    expect(screen.queryByText('Browser limitation reached')).toBeNull();
  });

  it('keeps the Mode B CTA wired to workspace creation', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ workspace_url: 'https://fly.sprout.dev/w/1' }, { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    render(createElement(EscalationListener));
    fireTrigger({ command: undefined });

    fireEvent.click(screen.getByText('Start Full Workspace'));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/workspace/fly', expect.anything()));
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/workspace/fly');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      repo_url: 'https://github.com/acme/app',
      mode: 'build',
    });
    await waitFor(() => expect(hrefSetter).toHaveBeenCalledWith('https://fly.sprout.dev/w/1'));
  });

  it('submits Mode A with the escalation reason in the prompt, then shows inline progress', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/tasks') {
        return jsonResponse({ task_id: 'task-9', status: 'completed' }, { status: 201 });
      }
      return jsonResponse({ task_id: 'task-9', status: 'completed' });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(createElement(EscalationListener));
    // No command → Mode A is the primary inline action.
    fireTrigger({ command: undefined, reason: 'git_push_failed' });
    fireEvent.click(screen.getByTestId('escalation-toast-cloud-task'));
    await flush();

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/tasks');
    expect(JSON.parse(init.body as string)).toEqual({
      repo_url: 'https://github.com/acme/app',
      prompt: 'Continue building this repository. Escalation reason: git_push_failed.',
    });
    expect(await screen.findByTestId('escalation-toast-cloud-task-status')).toHaveTextContent('Cloud task completed');
  });

  it('hides the cloud-task CTA once a txn is in flight for the same toast', async () => {
    render(createElement(EscalationListener));
    fireTrigger();
    fireEvent.click(screen.getByTestId('escalation-toast-txn'));
    await flush();
    expect(screen.queryByTestId('escalation-toast-cloud-task')).toBeNull();
  });
});
