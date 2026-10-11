/**
 * The command palette unit in an own-mode SproutWorkspace.
 *
 * - Its shortcut opens it when the key is pressed inside the workspace, and
 *   not when it is pressed in the host page around it.
 * - `OPEN_COMMAND_PALETTE_EVENT` opens it, and its registration for that
 *   event makes the header search control appear.
 * - It lists the commands something registered a handler for (and the host's
 *   own), runs them, and leaves unhandled built-ins out.
 * - An arrangement that supplies its own overlay without it opts out.
 *
 * The provider stack, the hotkey scope and the palette are the real ones; the
 * chat view is a focusable stub, and the hotkey config binds the palette.
 */

import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

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
vi.mock('../components/ChatView', () => ({
  default: () => <div data-testid="palette-test-chat" tabIndex={0} />,
}));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
const hotkeysLoaded = vi.hoisted(() => ({ count: 0 }));

vi.mock('../services/api', () => {
  const instance = {
    getHotkeys: async () => {
      hotkeysLoaded.count += 1;
      return { hotkeys: [{ key: 'Ctrl+Shift+P', command_id: 'command_palette' }], platform: 'test' };
    },
    applyHotkeyPreset: async () => ({ hotkeys: [] }),
    getWorkspace: async () => ({ workspace_root: '/work/widgets' }),
    getStats: async () => ({}),
    getSettings: async () => ({}),
  };
  return { ApiService: { getInstance: () => instance } };
});

import { LayeredSearchButton } from '../components/layered/LayeredTopBar';
import { OPEN_COMMAND_PALETTE_EVENT } from '../config/layout';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { __resetCommandHandlersForTests, registerCommandHandler } from '../services/commandHandlers';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutWorkspaceProps } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const CHAT_ONLY: ViewsArrangement = { left: [], center: ['chat'], right: [] };
const PALETTE_SHORTCUT = { key: 'P', code: 'KeyP', ctrlKey: true, shiftKey: true };

function Page(props: Partial<SproutWorkspaceProps>) {
  return (
    <>
      <input data-testid="host-input" />
      <div data-testid="host-panel" tabIndex={0} />
      <LayeredSearchButton />
      <SproutWorkspace
        project={{ id: 'acme/widgets', root: '/work/widgets' }}
        space="code"
        host={headlessHost()}
        layout={CHAT_ONLY}
        {...props}
      />
    </>
  );
}

function palette(): HTMLElement | null {
  return screen.queryByRole('dialog', { name: 'Command palette' });
}

/** The hotkey provider answers keys once its config has loaded. */
async function pressInsideWorkspace(): Promise<void> {
  const target = await screen.findByTestId('palette-test-chat');
  await waitFor(() => expect(hotkeysLoaded.count).toBeGreaterThan(0));
  await act(async () => {
    await Promise.resolve();
  });
  fireEvent.keyDown(target, PALETTE_SHORTCUT);
  expect(palette()).not.toBeNull();
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  hotkeysLoaded.count = 0;
  __resetCommandHandlersForTests();
  setActiveHost(headlessHost());
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => ({ files: [], workspace: '/work/widgets' }),
      text: async () => '',
    })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('WorkspaceCommandPalette in an own-mode workspace', () => {
  it('opens on its shortcut inside the workspace but not from the host page', async () => {
    render(<Page />);
    await pressInsideWorkspace();
    expect(within(screen.getByTestId('sprout-workspace')).getByRole('dialog')).toBe(palette());

    // The shortcut toggles: pressed again inside, the palette closes.
    fireEvent.keyDown(screen.getByTestId('palette-test-chat'), PALETTE_SHORTCUT);
    expect(palette()).toBeNull();

    // The hotkey config is loaded (the press above worked), so a miss here is
    // the scope, not a config race.
    fireEvent.keyDown(screen.getByTestId('host-input'), PALETTE_SHORTCUT);
    fireEvent.keyDown(screen.getByTestId('host-panel'), PALETTE_SHORTCUT);
    fireEvent.keyDown(document.body, PALETTE_SHORTCUT);
    expect(palette()).toBeNull();
  });

  it('opens from the open event, and its registration shows the header search control', async () => {
    const view = render(<Page />);
    const search = await screen.findByTestId('header-search');
    expect(palette()).toBeNull();

    act(() => {
      window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT));
    });
    expect(palette()).not.toBeNull();

    fireEvent.click(palette() as HTMLElement);
    expect(palette()).toBeNull();
    fireEvent.click(search);
    expect(palette()).not.toBeNull();

    view.unmount();
    render(<LayeredSearchButton />);
    expect(screen.queryByTestId('header-search')).toBeNull();
  });

  it('lists and runs registered commands, and leaves unhandled built-ins out', async () => {
    const unregister = registerCommandHandler(['toggle_minimap']);
    const hotkeys: string[] = [];
    const onHotkey = (event: Event) => hotkeys.push((event as CustomEvent<{ commandId: string }>).detail.commandId);
    window.addEventListener('sprout:hotkey', onHotkey);
    const onExecuteCommand = vi.fn();
    try {
      render(
        <Page
          viewProps={{
            commandPalette: {
              commands: [{ id: 'host.deploy', label: 'Deploy Preview', category: 'Host' }],
              onExecuteCommand,
            },
          }}
        />,
      );
      await screen.findByTestId('palette-test-chat');
      act(() => {
        window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT));
      });
      fireEvent.click(screen.getByRole('tab', { name: 'Commands' }));

      const options = screen.getAllByRole('option').map((option) => option.textContent ?? '');
      expect(options.some((text) => text.includes('Toggle Minimap'))).toBe(true);
      expect(options.some((text) => text.includes('Deploy Preview'))).toBe(true);
      expect(options.some((text) => text.includes('Toggle Sidebar'))).toBe(false);
      expect(options.some((text) => text.includes('Edit Keyboard Shortcuts'))).toBe(false);

      fireEvent.click(screen.getByRole('option', { name: /Toggle Minimap/ }));
      expect(hotkeys).toContain('toggle_minimap');
      expect(palette()).toBeNull();

      act(() => {
        window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT));
      });
      fireEvent.click(screen.getByRole('tab', { name: 'Commands' }));
      fireEvent.click(screen.getByRole('option', { name: /Deploy Preview/ }));
      expect(onExecuteCommand).toHaveBeenCalledWith('host.deploy');
    } finally {
      unregister();
      window.removeEventListener('sprout:hotkey', onHotkey);
    }
  });

  it('is left out by an arrangement whose overlay omits it', async () => {
    render(<Page layout={{ ...CHAT_ONLY, overlay: [] }} />);
    await screen.findByTestId('palette-test-chat');
    expect(screen.queryByTestId('header-search')).toBeNull();
    act(() => {
      window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT));
    });
    expect(palette()).toBeNull();
  });
});
