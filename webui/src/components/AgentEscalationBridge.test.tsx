import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HostUnavailableError } from '../services/cloudTxnEscalate';
import { escalationHostKey, getRememberedHost } from '../services/escalationHost';
import type { Runner } from '../services/runners';
import { AgentEscalationBridge } from './AgentEscalationBridge';

vi.mock('../config/mode', () => ({ isCloud: true, mode: 'cloud' }));

const runTxnCommand = vi.fn();
vi.mock('../services/cloudTxnEscalate', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/cloudTxnEscalate')>()),
  runTxnCommand: (...args: unknown[]) => runTxnCommand(...args),
}));

type Bridge = { run: (c: string) => Promise<{ ran: boolean; stdout?: string; exitCode?: number; message?: string }> };
const bridge = () => (globalThis as unknown as { __sproutEscalate?: Bridge }).__sproutEscalate;

const REPO = 'https://github.com/a/b';

const RUNNERS: Runner[] = [
  { runner_id: 'r-mac', name: 'MacBook', status: 'online', mode: 'native', sandbox: 'sandbox-exec' },
  { runner_id: 'r-box', name: 'box', status: 'online', mode: 'container', sandbox: '' },
  { runner_id: 'r-rig', name: 'rig', status: 'online', mode: 'bare-metal', sandbox: '' },
  { runner_id: 'r-old', name: 'old', status: 'offline', mode: 'container', sandbox: '' },
];

const OK = {
  result: { stdout: 'PASS\n', stderr: '', exit_code: 0, duration_ms: 5, timed_out: false, truncated: false },
  pulledFiles: 0,
  skippedFiles: 0,
};

function stubRunners(runners: Runner[] | null) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/runners' && runners) {
        return new Response(JSON.stringify(runners), { status: 200 });
      }
      return new Response('{"error":"not found"}', { status: 404 });
    }),
  );
}

/** Start an agent escalation and wait for the prompt with its run buttons enabled. */
async function ask(command: string): Promise<{ pending: ReturnType<Bridge['run']> }> {
  let pending!: ReturnType<Bridge['run']>;
  act(() => {
    pending = bridge()!.run(command);
  });
  await screen.findByRole('dialog');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Run once' })).toBeEnabled());
  // Wrapped: returning the promise itself would make `await ask()` wait for the run.
  return { pending };
}

beforeEach(() => {
  stubRunners(null);
});

afterEach(() => {
  runTxnCommand.mockReset();
  window.localStorage.clear();
  vi.unstubAllGlobals();
});

describe('AgentEscalationBridge', () => {
  it('asks before running an agent command in the cloud workspace', async () => {
    runTxnCommand.mockResolvedValue(OK);
    render(<AgentEscalationBridge repoURL={REPO} />);

    const { pending } = await ask('npm test');
    expect(screen.getByRole('dialog', { name: /run this in your cloud workspace/i })).toBeTruthy();
    expect(screen.getByText('npm test')).toBeTruthy();
    // No runners: no picker, today's cloud-only prompt.
    expect(screen.queryByTestId('run-host-picker')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Run once' }));
    await expect(pending).resolves.toMatchObject({ ran: true, stdout: 'PASS\n', exitCode: 0 });
    expect(runTxnCommand).toHaveBeenCalledWith(REPO, 'npm test', expect.any(Function), { kind: 'cloud' });
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('Escape declines', async () => {
    render(<AgentEscalationBridge repoURL={REPO} />);
    let pending!: ReturnType<Bridge['run']>;
    act(() => {
      pending = bridge()!.run('make');
    });
    await screen.findByRole('dialog');
    fireEvent.keyDown(document, { key: 'Escape' });
    await expect(pending).resolves.toMatchObject({ ran: false });
    expect(runTxnCommand).not.toHaveBeenCalled();
  });

  it('removes the bridge on unmount', () => {
    const { unmount } = render(<AgentEscalationBridge repoURL="r" />);
    expect(bridge()).toBeDefined();
    unmount();
    expect(bridge()).toBeUndefined();
  });

  it('offers runners with mode labels, disables offline ones, and warns on bare metal', async () => {
    stubRunners(RUNNERS);
    render(<AgentEscalationBridge repoURL={REPO} />);
    await ask('go test ./...');

    const options = screen.getAllByTestId('run-host-option');
    expect(options.map((o) => o.textContent)).toEqual([
      'MacBook· native · sandbox-exec',
      'box· container',
      'rig· bare metal',
      'old· container · offline',
    ]);
    expect(options[3]).toBeDisabled();
    expect(options[2].className).toContain('run-host-option--warning');
    expect(screen.getByTestId('run-host-option-cloud')).toBeEnabled();

    // Defaults to the first online runner, and the copy names it instead of the cloud.
    expect(options[0]).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('dialog', { name: 'Run this on MacBook?' })).toBeTruthy();
    expect(screen.queryByText(/cloud workspace\?/)).toBeNull();

    expect(screen.queryByTestId('run-host-bare-metal-warning')).toBeNull();
    fireEvent.click(options[2]);
    expect(screen.getByTestId('run-host-bare-metal-warning')).toHaveTextContent(/no sandbox/);
  });

  it('runs on the chosen runner and remembers it for the repo', async () => {
    stubRunners(RUNNERS);
    runTxnCommand.mockResolvedValue(OK);
    render(<AgentEscalationBridge repoURL={REPO} />);
    const { pending } = await ask('cargo build');

    fireEvent.click(screen.getAllByTestId('run-host-option')[1]);
    fireEvent.click(screen.getByRole('button', { name: 'Run once' }));
    await expect(pending).resolves.toMatchObject({ ran: true });
    expect(runTxnCommand).toHaveBeenCalledWith(REPO, 'cargo build', expect.any(Function), {
      kind: 'runner',
      runnerId: 'r-box',
      name: 'box',
    });
    expect(getRememberedHost(REPO)).toEqual({ kind: 'runner', runnerId: 'r-box', name: 'box' });
  });

  it('starts on the remembered host for the repo', async () => {
    stubRunners(RUNNERS);
    window.localStorage.setItem(escalationHostKey(REPO), JSON.stringify({ kind: 'cloud' }));
    render(<AgentEscalationBridge repoURL={REPO} />);
    await ask('make');
    expect(screen.getByTestId('run-host-option-cloud')).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('dialog', { name: /run this in your cloud workspace/i })).toBeTruthy();
  });

  it('"Always allow" remembers the host and later runs there without asking', async () => {
    stubRunners(RUNNERS);
    runTxnCommand.mockResolvedValue(OK);
    render(<AgentEscalationBridge repoURL={REPO} />);
    const { pending: first } = await ask('make');
    fireEvent.click(screen.getAllByTestId('run-host-option')[1]);
    fireEvent.click(screen.getByRole('button', { name: 'Always allow' }));
    await expect(first).resolves.toMatchObject({ ran: true });

    let second!: ReturnType<Bridge['run']>;
    act(() => {
      second = bridge()!.run('make test');
    });
    await expect(second).resolves.toMatchObject({ ran: true });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(runTxnCommand).toHaveBeenLastCalledWith(REPO, 'make test', expect.any(Function), {
      kind: 'runner',
      runnerId: 'r-box',
      name: 'box',
    });
  });

  it('offers the cloud when the chosen runner turns the run down', async () => {
    stubRunners(RUNNERS);
    runTxnCommand
      .mockRejectedValueOnce(new HostUnavailableError({ kind: 'runner', runnerId: 'r-mac', name: 'MacBook' }))
      .mockResolvedValueOnce(OK);
    render(<AgentEscalationBridge repoURL={REPO} />);
    const { pending } = await ask('npm test');
    fireEvent.click(screen.getByRole('button', { name: 'Run once' }));

    expect(await screen.findByTestId('run-host-notice')).toHaveTextContent('MacBook is offline or busy.');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Run once' })).toBeEnabled());
    expect(screen.getByTestId('run-host-option-cloud')).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(screen.getByRole('button', { name: 'Run once' }));
    await expect(pending).resolves.toMatchObject({ ran: true });
    expect(runTxnCommand).toHaveBeenLastCalledWith(REPO, 'npm test', expect.any(Function), { kind: 'cloud' });
  });

  describe('inline run status lifecycle', () => {
    /** Start a run whose phases the test drives, returning the phase emitter. */
    async function runWithPhases(): Promise<{ emit: (phase: string) => void; settle: () => void }> {
      let emitPhase!: (phase: string) => void;
      let resolveRun!: (v: typeof OK) => void;
      runTxnCommand.mockImplementation((_repo: string, _cmd: string, onPhase: (p: string) => void) => {
        emitPhase = onPhase;
        return new Promise((resolve) => {
          resolveRun = resolve;
        });
      });
      render(<AgentEscalationBridge repoURL={REPO} />);
      await ask('npm test');
      fireEvent.click(screen.getByRole('button', { name: 'Run once' }));
      await waitFor(() => expect(emitPhase).toBeDefined());
      return {
        emit: (phase) => act(() => emitPhase(phase)),
        settle: () => act(() => resolveRun(OK)),
      };
    }

    it('shows a status during a run and clears it on the terminal "done" phase', async () => {
      const { emit, settle } = await runWithPhases();
      emit('opening');
      expect(screen.getByRole('status')).toHaveTextContent(/Starting cloud container/);
      emit('running');
      expect(screen.getByRole('status')).toHaveTextContent(/Running command/);
      emit('done');
      expect(screen.queryByRole('status')).toBeNull();
      settle();
    });

    it('clears the status on the terminal "error" phase (a run that failed before pulling)', async () => {
      const { emit } = await runWithPhases();
      emit('opening');
      expect(screen.getByRole('status')).toBeTruthy();
      emit('error');
      expect(screen.queryByRole('status')).toBeNull();
    });

    it('clears a stalled status when no next phase arrives (watchdog)', async () => {
      vi.useFakeTimers();
      try {
        const { emit } = await runWithPhases();
        emit('opening');
        expect(screen.getByRole('status')).toBeTruthy();
        act(() => vi.advanceTimersByTime(120_000));
        expect(screen.queryByRole('status')).toBeNull();
      } finally {
        vi.useRealTimers();
      }
    });
  });
});
