import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { WasmShell, WasmShellResult } from '../services/wasmShell';
import { useWasmTerminalInput, type UseWasmTerminalInputReturn } from './useWasmTerminalInput';

const shellMock = vi.hoisted(() => ({ shell: null as unknown }));

vi.mock('../services/wasmShell', () => ({
  initWasmShell: () => Promise.resolve(shellMock.shell),
}));

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

async function mountTerminal(shell: Partial<WasmShell>) {
  shellMock.shell = { getCwd: () => '/home/user', ...shell };
  const term = { write: vi.fn(), writeln: vi.fn() };
  const xtermRef = { current: term } as unknown as React.RefObject<never>;
  let api: UseWasmTerminalInputReturn | null = null;
  function Harness(): null {
    const value = useWasmTerminalInput({ xtermRef, isActive: true, isConnected: false });
    useEffect(() => {
      api = value;
    });
    return null;
  }
  await act(async () => {
    root.render(<Harness />);
  });
  await act(async () => {
    await Promise.resolve();
  });
  return { term, type: (data: string) => api!.handleWasmInput(data) };
}

describe('useWasmTerminalInput', () => {
  it('runs commands through the async shell path and waits for the result', async () => {
    const pending = deferred<WasmShellResult>();
    const executeCommand = vi.fn();
    const executeCommandAsync = vi.fn(() => pending.promise);
    const { term, type } = await mountTerminal({ executeCommand, executeCommandAsync });

    act(() => {
      for (const ch of 'git status') type(ch);
      type('\r');
    });
    expect(executeCommandAsync).toHaveBeenCalledWith('git status');
    expect(executeCommand).not.toHaveBeenCalled();

    // Keystrokes while the command runs are dropped, not queued into the line.
    act(() => type('x'));
    const writesBefore = term.write.mock.calls.length;

    await act(async () => {
      pending.resolve({ stdout: 'On branch main\n', stderr: '', exitCode: 0 });
      await pending.promise;
    });
    const output = term.write.mock.calls
      .slice(writesBefore)
      .map((c) => c[0])
      .join('');
    expect(output).toContain('On branch main\r\n');
    expect(output).toContain('user@sprout-wasm');
    expect(output).not.toContain('x');
  });
});
