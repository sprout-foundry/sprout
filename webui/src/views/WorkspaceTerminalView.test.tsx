/**
 * WorkspaceTerminalView — the arranged terminal, per host backend.
 *
 * - A host whose agent runs in the browser gets a terminal on the WASM shell:
 *   a typed command runs in the shell and its output is written back.
 * - A host that declares `capabilities.localTerminal` gets the PTY terminal:
 *   its socket and its `/api/terminal/*` calls go through the host transport.
 * - Any other host gets the unavailable notice instead of a dead surface.
 *
 * The real terminal surface, panes and transports run; xterm is a recording
 * stub (jsdom cannot lay it out), the WASM shell is a fake, and the WebSocket
 * never opens.
 */

import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const xterm = vi.hoisted(() => ({
  instances: [] as Array<{ written: string[]; emit: (data: string) => void }>,
}));

vi.mock('@xterm/xterm', () => ({
  Terminal: vi.fn(function (this: Record<string, unknown>) {
    const written: string[] = [];
    let onData: (data: string) => void = () => undefined;
    Object.assign(this, {
      cols: 80,
      rows: 24,
      options: {},
      buffer: { active: { baseY: 0, getLine: () => null } },
      loadAddon: vi.fn(),
      open: vi.fn(),
      focus: vi.fn(),
      clear: vi.fn(),
      reset: vi.fn(),
      dispose: vi.fn(),
      hasSelection: () => false,
      getSelection: () => '',
      selectAll: vi.fn(),
      attachCustomKeyEventHandler: vi.fn(),
      registerLinkProvider: vi.fn(() => ({ dispose: vi.fn() })),
      onSelectionChange: vi.fn(() => ({ dispose: vi.fn() })),
      onTitleChange: vi.fn(() => ({ dispose: vi.fn() })),
      onData: vi.fn((handler: (data: string) => void) => {
        onData = handler;
        return { dispose: vi.fn() };
      }),
      write: vi.fn((text: string) => written.push(text)),
      writeln: vi.fn((text: string) => written.push(`${text}\r\n`)),
    });
    xterm.instances.push({ written, emit: (data) => onData(data) });
  }),
}));
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: vi.fn(function (this: Record<string, unknown>) {
    this.fit = vi.fn();
  }),
}));
vi.mock('@xterm/addon-search', () => ({
  SearchAddon: vi.fn(function (this: Record<string, unknown>) {
    this.findNext = vi.fn();
    this.findPrevious = vi.fn();
    this.clearDecorations = vi.fn();
    this.onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    this.dispose = vi.fn();
  }),
}));
vi.mock('../contexts/ThemeContext', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../contexts/ThemeContext')>()),
  useTheme: () => ({ theme: 'dark', themePack: { id: 'default' } }),
}));
vi.mock('../hooks/useAvailableShells', () => ({
  useAvailableShells: () => ({
    availableShells: [],
    shellsLoaded: true,
    selectedShell: null,
    setSelectedShell: vi.fn(),
  }),
}));

const wasm = vi.hoisted(() => ({ commands: [] as string[] }));
vi.mock('../services/wasmShell', () => ({
  initWasmShell: vi.fn(async () => ({
    executeCommand: (input: string) => {
      wasm.commands.push(input);
      return { stdout: `ran ${input}\n`, stderr: '', exitCode: 0 };
    },
    executeCommandAsync: async (input: string) => {
      wasm.commands.push(input);
      return { stdout: `ran ${input}\n`, stderr: '', exitCode: 0 };
    },
    autoComplete: () => ({ completions: [] }),
    getCwd: () => '/home/user',
    getWorkspaceRoot: () => '/home/user',
  })),
}));

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import { resolveWorkspaceTerminalBackend, WorkspaceTerminalView } from './WorkspaceTerminalView';

/** Records the URL of every socket the terminal opens; none ever connects. */
class RecordingWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static urls: string[] = [];
  readyState = 0;
  onopen: unknown = null;
  onclose: unknown = null;
  onerror: unknown = null;
  onmessage: unknown = null;
  constructor(url: string) {
    RecordingWebSocket.urls.push(url);
  }
  send(): void {}
  close(): void {}
}

const fetchMock = vi.fn(
  async () => new Response(JSON.stringify({ sessions: [] }), { headers: { 'Content-Type': 'application/json' } }),
);

function wasmHost(): SproutHost {
  const host = makeTestHost({ localTerminal: false });
  return { ...host, transport: { ...host.transport, agent: { kind: 'wasm', modelEndpoint: '' } } };
}

function ptyHost(): SproutHost {
  const host = makeTestHost({ localTerminal: true });
  return { ...host, transport: { ...host.transport, apiBaseURL: 'https://backend.example/sprout' } };
}

function renderView(host: SproutHost) {
  setActiveHost(host);
  return render(
    <HostProvider host={host}>
      <WorkspaceTerminalView />
    </HostProvider>,
  );
}

beforeEach(() => {
  xterm.instances.length = 0;
  wasm.commands.length = 0;
  RecordingWebSocket.urls = [];
  fetchMock.mockClear();
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('WebSocket', RecordingWebSocket);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('resolveWorkspaceTerminalBackend', () => {
  it('prefers the in-browser shell, then a declared local terminal, else none', () => {
    expect(resolveWorkspaceTerminalBackend(wasmHost())).toBe('wasm');
    expect(resolveWorkspaceTerminalBackend(ptyHost())).toBe('pty');
    expect(resolveWorkspaceTerminalBackend(makeTestHost({ localTerminal: false }))).toBe('none');
  });
});

describe('WorkspaceTerminalView', () => {
  it('runs commands in the WASM shell for an in-browser-agent host and shows their output', async () => {
    renderView(wasmHost());
    expect(screen.getByTestId('workspace-terminal')).toHaveAttribute('data-backend', 'wasm');
    await waitFor(() => expect(xterm.instances).toHaveLength(1));
    const term = xterm.instances[0];
    await waitFor(() => expect(term.written.join('')).toContain('user@sprout-wasm'));

    act(() => {
      for (const ch of 'ls') term.emit(ch);
      term.emit('\r');
    });

    await waitFor(() => expect(wasm.commands).toEqual(['ls']));
    await waitFor(() => expect(term.written.join('')).toContain('ran ls\r\n'));
    expect(RecordingWebSocket.urls).toEqual([]);
  });

  it('attaches to the PTY through the host transport for a host with a local terminal', async () => {
    renderView(ptyHost());
    expect(screen.getByTestId('workspace-terminal')).toHaveAttribute('data-backend', 'pty');

    await waitFor(() => expect(RecordingWebSocket.urls).toHaveLength(1));
    expect(RecordingWebSocket.urls[0].startsWith('wss://backend.example/sprout/terminal')).toBe(true);
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        'https://backend.example/sprout/api/terminal/agent-sessions',
        expect.anything(),
      ),
    );
  });

  it('shows the unavailable state when the host offers no terminal', () => {
    renderView(makeTestHost({ localTerminal: false }));
    expect(screen.getByTestId('workspace-terminal-unavailable')).toHaveTextContent(
      'Terminal not available in this workspace.',
    );
    expect(screen.queryByTestId('workspace-terminal')).not.toBeInTheDocument();
    expect(xterm.instances).toHaveLength(0);
  });

  it('takes the backend and the unavailable message from viewProps', () => {
    setActiveHost(ptyHost());
    render(
      <HostProvider host={ptyHost()}>
        <WorkspaceTerminalView backend="none" unavailableMessage="No shell here" />
      </HostProvider>,
    );
    expect(screen.getByTestId('workspace-terminal-unavailable')).toHaveTextContent('No shell here');
  });
});
