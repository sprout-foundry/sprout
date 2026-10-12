/**
 * The composed space shell's terminal (own mode, no `layout`), per host.
 *
 * - A host with `capabilities.localTerminal` and no in-browser agent gets the
 *   PTY terminal docked under the shell, which Ctrl+` inside the workspace
 *   toggles.
 * - A host whose agent runs in the browser gets the docked terminal on the
 *   in-browser shell, even if it also declares `localTerminal`.
 * - A host with neither gets no terminal and no toggle handler.
 *
 * In every case the shell is told not to mount its own viewport-fixed
 * in-browser terminal: the docked one takes its place.
 *
 * The docked terminal and the Code shell are stubs that expose their props;
 * the provider stack, the hotkey scope and the composition are the real ones.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const hotkeysLoaded = vi.hoisted(() => ({ count: 0 }));

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    connect = vi.fn();
    disconnect = vi.fn();
    onEvent = vi.fn();
    removeEvent = vi.fn();
    sendEvent = vi.fn();
    isConnected = vi.fn(() => true);
    onReconnect = vi.fn();
    freeze = vi.fn();
    resume = vi.fn();
    resetAndReconnect = vi.fn();
    getQueuedMessageCount = vi.fn(() => 0);
  },
}));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('./WorkspaceCommandPalette', () => ({ WorkspaceCommandPalette: () => null }));
vi.mock('../services/api', () => {
  const instance = {
    getHotkeys: async () => {
      hotkeysLoaded.count += 1;
      return { hotkeys: [{ key: 'Ctrl+`', command_id: 'toggle_terminal', global: true }], platform: 'test' };
    },
    applyHotkeyPreset: async () => ({ hotkeys: [] }),
    getWorkspace: async () => ({ workspace_root: '' }),
    getStats: async () => ({}),
    getSettings: async () => ({}),
  };
  return { ApiService: { getInstance: () => instance } };
});
vi.mock('./WorkspaceDockedTerminal', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./WorkspaceDockedTerminal')>()),
  WorkspaceDockedTerminal: (props: { backend: string; expanded: boolean }) => (
    <div data-testid="composed-docked-terminal" data-backend={props.backend} data-expanded={String(props.expanded)} />
  ),
}));
vi.mock('../workspaces/CodeShell', () => ({
  default: (props: { supportsLocalTerminal: boolean; inBrowserTerminal?: boolean; isTerminalExpanded: boolean }) => (
    <div
      data-testid="composed-code-shell"
      tabIndex={0}
      data-local-terminal={String(props.supportsLocalTerminal)}
      data-in-browser-terminal={String(props.inBrowserTerminal ?? true)}
      data-terminal-expanded={String(props.isTerminalExpanded)}
    />
  ),
}));

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { makeTestHost } from '../host/testHost';
import { __resetCommandHandlersForTests, hasCommandHandler } from '../services/commandHandlers';
import { SproutWorkspace } from './SproutWorkspace';

const TOGGLE_TERMINAL = { key: '`', code: 'Backquote', ctrlKey: true };

function ptyHost(): SproutHost {
  return makeTestHost({ localTerminal: true });
}

function wasmHost(): SproutHost {
  const host = makeTestHost({ localTerminal: true });
  return { ...host, transport: { ...host.transport, agent: { kind: 'wasm', modelEndpoint: '' } } };
}

function renderWorkspace(host: SproutHost) {
  return render(<SproutWorkspace project={{ id: 'acme/widgets' }} space="code" host={host} />);
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  hotkeysLoaded.count = 0;
  __resetCommandHandlersForTests();
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => ({}),
      text: async () => '',
    })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('SproutWorkspace composed shell terminal', () => {
  it('docks the PTY terminal for a local-terminal host and toggles it with Ctrl+` inside the workspace', async () => {
    setActiveHost(ptyHost());
    renderWorkspace(ptyHost());
    const shell = await screen.findByTestId('composed-code-shell');
    expect(shell).toHaveAttribute('data-local-terminal', 'true');
    expect(shell).toHaveAttribute('data-in-browser-terminal', 'false');
    const terminal = screen.getByTestId('composed-docked-terminal');
    expect(terminal).toHaveAttribute('data-backend', 'pty');
    expect(terminal).toHaveAttribute('data-expanded', 'false');
    expect(hasCommandHandler('toggle_terminal')).toBe(true);
    expect(hasCommandHandler('clear_terminal')).toBe(true);

    await waitFor(() => expect(hotkeysLoaded.count).toBeGreaterThan(0));
    await act(async () => {
      await Promise.resolve();
    });
    fireEvent.keyDown(shell, TOGGLE_TERMINAL);
    expect(screen.getByTestId('composed-docked-terminal')).toHaveAttribute('data-expanded', 'true');
    expect(screen.getByTestId('composed-code-shell')).toHaveAttribute('data-terminal-expanded', 'true');

    fireEvent.keyDown(screen.getByTestId('composed-code-shell'), TOGGLE_TERMINAL);
    expect(screen.getByTestId('composed-docked-terminal')).toHaveAttribute('data-expanded', 'false');
  });

  it('docks the in-browser terminal for a host whose agent runs in the browser', async () => {
    setActiveHost(wasmHost());
    renderWorkspace(wasmHost());
    const shell = await screen.findByTestId('composed-code-shell');
    expect(shell).toHaveAttribute('data-local-terminal', 'false');
    expect(shell).toHaveAttribute('data-in-browser-terminal', 'false');
    expect(screen.getByTestId('composed-docked-terminal')).toHaveAttribute('data-backend', 'wasm');
    expect(hasCommandHandler('toggle_terminal')).toBe(true);
    // The in-browser shell has no splits or process control.
    expect(hasCommandHandler('clear_terminal')).toBe(false);
  });

  it('mounts no terminal and no toggle for a host with neither', async () => {
    const host = makeTestHost({ localTerminal: false });
    setActiveHost(host);
    renderWorkspace(host);
    const shell = await screen.findByTestId('composed-code-shell');
    expect(shell).toHaveAttribute('data-local-terminal', 'false');
    expect(shell).toHaveAttribute('data-in-browser-terminal', 'false');
    expect(screen.queryByTestId('composed-docked-terminal')).not.toBeInTheDocument();
    expect(hasCommandHandler('toggle_terminal')).toBe(false);
  });
});
