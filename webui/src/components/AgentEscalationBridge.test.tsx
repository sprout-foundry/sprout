import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import { AgentEscalationBridge } from './AgentEscalationBridge';

const runTxnCommand = vi.fn();
vi.mock('../services/cloudTxnEscalate', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/cloudTxnEscalate')>()),
  runTxnCommand: (...args: unknown[]) => runTxnCommand(...args),
}));

// The bridge reads its host capability through the provider; this suite
// exercises the hosted (WASM) shell, which has no local terminal.
function renderBridge(props: { repoURL?: string }) {
  return render(
    <HostProvider host={makeTestHost({ localTerminal: false })}>
      <AgentEscalationBridge repoURL={props.repoURL} />
    </HostProvider>,
  );
}

type Bridge = { run: (c: string) => Promise<{ ran: boolean; stdout?: string; exitCode?: number; message?: string }> };
const bridge = () => (globalThis as unknown as { __sproutEscalate?: Bridge }).__sproutEscalate;

afterEach(() => {
  runTxnCommand.mockReset();
  window.localStorage.clear();
});

describe('AgentEscalationBridge', () => {
  it('asks before running an agent command in the cloud workspace', async () => {
    runTxnCommand.mockResolvedValue({
      result: { stdout: 'PASS\n', stderr: '', exit_code: 0, duration_ms: 5, timed_out: false, truncated: false },
      pulledFiles: 0,
      skippedFiles: 0,
    });
    renderBridge({ repoURL: 'https://github.com/a/b' });

    let pending!: ReturnType<Bridge['run']>;
    act(() => {
      pending = bridge()!.run('npm test');
    });
    expect(await screen.findByRole('dialog', { name: /run this in your cloud workspace/i })).toBeTruthy();
    expect(screen.getByText('npm test')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Run once' }));
    await expect(pending).resolves.toMatchObject({ ran: true, stdout: 'PASS\n', exitCode: 0 });
    expect(runTxnCommand).toHaveBeenCalledWith('https://github.com/a/b', 'npm test', expect.any(Function));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('Escape declines', async () => {
    renderBridge({ repoURL: 'https://github.com/a/b' });
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
    const { unmount } = renderBridge({ repoURL: 'r' });
    expect(bridge()).toBeDefined();
    unmount();
    expect(bridge()).toBeUndefined();
  });
});
