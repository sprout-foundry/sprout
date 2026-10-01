import { afterEach, describe, expect, it, vi } from 'vitest';
import { escalateCommand, getEscalationPolicy, installEscalationBridge, setEscalationPolicy } from './agentEscalation';
import type { TxnCommandOutcome } from './cloudTxnEscalate';

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
    expect(run).toHaveBeenCalledWith('https://github.com/a/b', 'npm test', expect.any(Function));
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
