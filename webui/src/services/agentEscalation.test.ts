import { afterEach, describe, expect, it, vi } from 'vitest';
import { escalateCommand, getEscalationPolicy, installEscalationBridge, setEscalationPolicy } from './agentEscalation';
import { HostUnavailableError, type TxnCommandOutcome } from './cloudTxnEscalate';
import { getRememberedHost, rememberHost } from './escalationHost';

const outcome = (over: Partial<TxnCommandOutcome['result']> = {}): TxnCommandOutcome => ({
  result: { stdout: 'ok\n', stderr: '', exit_code: 0, duration_ms: 10, timed_out: false, truncated: false, ...over },
  pulledFiles: 1,
  skippedFiles: 0,
});

afterEach(() => {
  window.localStorage.clear();
});

describe('escalateCommand', () => {
  it('asks, runs on approval, and returns the real result', async () => {
    const run = vi.fn().mockResolvedValue(outcome());
    const requestConsent = vi.fn().mockResolvedValue('once');
    const res = await escalateCommand('npm test', { repoURL: 'https://github.com/a/b', requestConsent, run });
    expect(requestConsent).toHaveBeenCalledWith('npm test');
    // A bare decision (embedded hosts) runs on "auto" — the platform picks.
    expect(run).toHaveBeenCalledWith('https://github.com/a/b', 'npm test', expect.any(Function), { kind: 'auto' });
    expect(res).toEqual({ ran: true, stdout: 'ok\n', stderr: '', exitCode: 0 });
    expect(getEscalationPolicy()).toBe('ask');
  });

  it('remembers "always" and stops asking', async () => {
    const run = vi.fn().mockResolvedValue(outcome());
    const requestConsent = vi.fn().mockResolvedValue('always');
    await escalateCommand('go build ./...', { repoURL: 'r', requestConsent, run });
    await escalateCommand('go test ./...', { repoURL: 'r', requestConsent, run });
    expect(requestConsent).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it('explains a denial to the model without running', async () => {
    const run = vi.fn();
    const res = await escalateCommand('rm -rf build', { repoURL: 'r', requestConsent: async () => 'deny', run });
    expect(run).not.toHaveBeenCalled();
    expect(res.ran).toBe(false);
    expect(res.message).toMatch(/declined/);
  });

  it('never runs when the policy is off', async () => {
    setEscalationPolicy('never');
    const requestConsent = vi.fn();
    const res = await escalateCommand('make', { repoURL: 'r', requestConsent });
    expect(requestConsent).not.toHaveBeenCalled();
    expect(res).toMatchObject({ ran: false });
  });

  it('needs a repository to pick a workspace', async () => {
    const res = await escalateCommand('make', { requestConsent: vi.fn() });
    expect(res.ran).toBe(false);
    expect(res.message).toMatch(/repository/);
  });

  it('maps a timeout to 124 and notes truncation', async () => {
    setEscalationPolicy('always');
    const run = vi.fn().mockResolvedValue(outcome({ timed_out: true, truncated: true, exit_code: -1 }));
    const res = await escalateCommand('sleep 999', { repoURL: 'r', requestConsent: vi.fn(), run });
    expect(res.exitCode).toBe(124);
    expect(res.stderr).toMatch(/timed out/);
    expect(res.stderr).toMatch(/truncated/);
  });

  it('turns a run failure into an explanation', async () => {
    setEscalationPolicy('always');
    const run = vi.fn().mockRejectedValue(new Error('Starting cloud container failed: workspaces are not available'));
    const res = await escalateCommand('make', { repoURL: 'r', requestConsent: vi.fn(), run });
    expect(res).toMatchObject({ ran: false });
    expect(res.message).toMatch(/workspaces are not available/);
  });
});

describe('installEscalationBridge', () => {
  it('installs and removes the global the WASM executor calls', async () => {
    const uninstall = installEscalationBridge({ repoURL: 'r', requestConsent: async () => 'deny' });
    const g = globalThis as { __sproutEscalate?: { run: (c: string) => Promise<unknown> } };
    expect(typeof g.__sproutEscalate?.run).toBe('function');
    uninstall();
    expect(g.__sproutEscalate).toBeUndefined();
  });
});

describe('escalateCommand host choice', () => {
  const MAC = { kind: 'runner' as const, runnerId: 'r-mac', name: 'MacBook' };

  it('runs on the host the user picked and remembers it for the repo', async () => {
    const run = vi.fn().mockResolvedValue(outcome());
    const res = await escalateCommand('make', {
      repoURL: 'repo',
      requestConsent: async () => ({ decision: 'once', host: MAC }),
      run,
    });
    expect(res.ran).toBe(true);
    expect(run).toHaveBeenCalledWith('repo', 'make', expect.any(Function), MAC);
    expect(getRememberedHost('repo')).toEqual(MAC);
    expect(getEscalationPolicy()).toBe('ask');
  });

  it('"always" runs on the remembered host without asking', async () => {
    setEscalationPolicy('always');
    rememberHost('repo', MAC);
    const run = vi.fn().mockResolvedValue(outcome());
    const requestConsent = vi.fn();
    await escalateCommand('make', {
      repoURL: 'repo',
      requestConsent,
      alwaysHost: () => getRememberedHost('repo') ?? { kind: 'auto' },
      isHostAvailable: async () => true,
      run,
    });
    expect(requestConsent).not.toHaveBeenCalled();
    expect(run).toHaveBeenCalledWith('repo', 'make', expect.any(Function), MAC);
  });

  it('asks again with the reason when the runner is unavailable, then runs where the user picks', async () => {
    const run = vi.fn().mockRejectedValueOnce(new HostUnavailableError(MAC)).mockResolvedValueOnce(outcome());
    const requestConsent = vi
      .fn()
      .mockResolvedValueOnce({ decision: 'once', host: MAC })
      .mockResolvedValueOnce({ decision: 'once', host: { kind: 'cloud' } });
    const res = await escalateCommand('make', { repoURL: 'repo', requestConsent, run });
    expect(res.ran).toBe(true);
    expect(requestConsent).toHaveBeenLastCalledWith('make', {
      notice: 'MacBook is offline or busy.',
      unavailableRunnerId: 'r-mac',
    });
    expect(run).toHaveBeenLastCalledWith('repo', 'make', expect.any(Function), { kind: 'cloud' });
  });

  it('an "always" run on an offline runner asks instead of waiting on it', async () => {
    setEscalationPolicy('always');
    const run = vi.fn().mockResolvedValue(outcome());
    const requestConsent = vi.fn().mockResolvedValue('deny');
    const res = await escalateCommand('make', {
      repoURL: 'repo',
      requestConsent,
      alwaysHost: () => MAC,
      isHostAvailable: async () => false,
      run,
    });
    expect(run).not.toHaveBeenCalled();
    expect(requestConsent).toHaveBeenCalledWith('make', expect.objectContaining({ unavailableRunnerId: 'r-mac' }));
    expect(res).toMatchObject({ ran: false });
    expect(res.message).toMatch(/MacBook is offline or busy/);
  });

  it('names the runner when a run there fails', async () => {
    const run = vi.fn().mockRejectedValue(new Error('daemon unreachable'));
    const res = await escalateCommand('make', {
      repoURL: 'repo',
      requestConsent: async () => ({ decision: 'once', host: MAC }),
      run,
    });
    expect(res.message).toBe('Running this on MacBook failed: daemon unreachable');
  });
});
