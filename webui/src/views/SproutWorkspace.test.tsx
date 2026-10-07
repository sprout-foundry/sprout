/**
 * SproutWorkspace.
 *
 * Pins the mount contract: one project + one space renders the resolved
 * space's registered shell (through the SP-155 registry), the supplied host
 * reaches every consumer below, `layout` swaps the space's shell for the host's
 * own view arrangement, and `onSpaceChange` fires on a change — not on the
 * initial mount.
 *
 * The shells are mocked (they need the full webui state object), but the
 * registry, the provider stack and the host contract are the real ones. The
 * wrapper's default events transport is stubbed at its module boundary so no
 * real service is reached; the rest of the provider stack runs unmocked, which
 * is what makes the host-supply and resolution cases meaningful.
 */

import { act, render, screen, within } from '@testing-library/react';
import { useState } from 'react';
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

// The registry pulls the built-in shells in with it; stub them so the space is
// a distinct, assertable node without the shell's real chrome and state.
vi.mock('../workspaces/CodeShell', () => ({
  default: (props: { currentView?: string }) => (
    <div data-testid="sprout-workspace-code-shell">view={props.currentView ?? ''}</div>
  ),
}));
vi.mock('../workspaces/DesignShell', () => ({
  default: () => <div data-testid="sprout-workspace-design-shell" />,
}));

// The web UI services the provider stack reaches. They are not the unit under
// test; the adapter singleton and the hotkey API touch the network.
vi.mock('../services/api', () => {
  class MockApiService {
    private static instance: MockApiService;
    static getInstance() {
      if (!MockApiService.instance) MockApiService.instance = new MockApiService();
      return MockApiService.instance;
    }
    async getHotkeys() {
      return { hotkeys: [], platform: 'test' };
    }
    async applyHotkeyPreset() {
      return { hotkeys: [] };
    }
    async getWorkspace() {
      return { workspace_root: '' };
    }
  }
  return { ApiService: MockApiService };
});

import { useHost } from '../host';
import type { SproutHost } from '../host';
import { headlessHost } from '../host/HostProvider';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { registerWorkspaceMode } from '../workspaces/registry';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';

const PROJECT: SproutProject = { id: '/Users/dev/project', name: 'project', root: '/Users/dev/project' };

/** A host distinguishable from the headless default by its transport URL. */
function makeHost(overrides: Partial<SproutHost> = {}): SproutHost {
  return {
    ...headlessHost(),
    transport: { apiBaseURL: 'https://host.test/api', wsURL: 'wss://host.test/ws', authMode: 'bearer' },
    ...overrides,
  };
}

/** A consumer that reveals the host it reads through `useHost()`. */
function HostProbe(): JSX.Element {
  const host = useHost();
  return <div data-testid="host-probe" data-api={host.transport.apiBaseURL} data-auth={host.transport.authMode} />;
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

/** A space registered by a rule test, disposed after each test. */
let registeredSpaceDisposer: (() => void) | null = null;

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  registeredSpaceDisposer?.();
  registeredSpaceDisposer = null;
  vi.clearAllMocks();
});

describe('SproutWorkspace', () => {
  it('mounts the requested project and space', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} />);

    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-project', PROJECT.id);
    expect(workspace).toHaveAttribute('data-space', 'code');
    expect(screen.getByTestId('sprout-workspace-code-shell')).toBeInTheDocument();
  });

  it('mounts a different space through the registry', async () => {
    render(<SproutWorkspace project={PROJECT} space="design" host={makeHost()} />);

    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-space', 'design');
    expect(screen.getByTestId('sprout-workspace-design-shell')).toBeInTheDocument();
    expect(screen.queryByTestId('sprout-workspace-code-shell')).not.toBeInTheDocument();
  });

  it('resolves an unknown space to the registry fallback (the built-in default)', async () => {
    render(<SproutWorkspace project={PROJECT} space="not-a-space" host={makeHost()} />);

    // resolveWorkspaceMode's contract: an unknown id degrades to the built-in
    // default (`code`), never to nothing.
    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-space', 'code');
    expect(screen.getByTestId('sprout-workspace-code-shell')).toBeInTheDocument();
  });

  it('supplies the host prop to every consumer below', async () => {
    const host = makeHost();
    render(
      <SproutWorkspace project={PROJECT} space="code" host={host}>
        <HostProbe />
      </SproutWorkspace>,
    );

    const probe = await screen.findByTestId('host-probe');
    expect(probe).toHaveAttribute('data-api', 'https://host.test/api');
    expect(probe).toHaveAttribute('data-auth', 'bearer');
  });

  it('honours a supplied layout: the space content renders through ViewsLayout', async () => {
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        layout={{ left: [], center: ['previewPane'], right: [], overlay: [] }}
      />,
    );

    const workspace = await screen.findByTestId('sprout-workspace');
    // The layout path: the space's registered shell is replaced by the host's
    // arrangement, and the view lands in the slot the arrangement names.
    expect(within(workspace).getByTestId('views-layout')).toBeInTheDocument();
    expect(within(workspace).getByTestId('views-slot-center')).toBeInTheDocument();
    expect(screen.queryByTestId('sprout-workspace-code-shell')).not.toBeInTheDocument();
  });

  it('calls onSpaceChange only when the mounted space changes, not on mount', async () => {
    const onSpaceChange = vi.fn();

    function Harness(): JSX.Element {
      const [space, setSpace] = useState<'code' | 'design'>('code');
      return (
        <>
          <button type="button" data-testid="to-design" onClick={() => setSpace('design')}>
            design
          </button>
          <SproutWorkspace project={PROJECT} space={space} host={makeHost()} onSpaceChange={onSpaceChange} />
        </>
      );
    }

    render(<Harness />);
    await screen.findByTestId('sprout-workspace');
    expect(onSpaceChange).not.toHaveBeenCalled();

    act(() => {
      screen.getByTestId('to-design').click();
    });
    await screen.findByTestId('sprout-workspace-design-shell');
    expect(onSpaceChange).toHaveBeenCalledTimes(1);
    expect(onSpaceChange).toHaveBeenCalledWith('design');
  });

  it('does not report a change when a request resolves to the already-mounted space', async () => {
    const onSpaceChange = vi.fn();

    function Harness(): JSX.Element {
      const [space, setSpace] = useState<string>('code');
      return (
        <>
          <button type="button" data-testid="to-unknown" onClick={() => setSpace('mystery')}>
            mystery
          </button>
          <SproutWorkspace project={PROJECT} space={space} host={makeHost()} onSpaceChange={onSpaceChange} />
        </>
      );
    }

    render(<Harness />);
    await screen.findByTestId('sprout-workspace');

    // An unknown id resolves back to `code` (the mounted space), so the mounted
    // space is unchanged and there is nothing to report.
    act(() => {
      screen.getByTestId('to-unknown').click();
    });
    await screen.findByTestId('sprout-workspace-code-shell');
    expect(onSpaceChange).not.toHaveBeenCalled();
  });

  it('mounts the resolved space behind the app provider stack (real SproutProviders)', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} />);
    // SproutProviders renders nothing until its events transport resolves;
    // the workspace appearing proves the stack mounted around the space.
    expect(await screen.findByTestId('sprout-workspace')).toBeInTheDocument();
  });

  it('warns when both layout and shellProps are supplied (layout wins, shellProps is dropped)', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        layout={{ left: [], center: ['previewPane'], right: [], overlay: [] }}
        shellProps={{ currentView: 'chat' } as WorkspaceShellProps}
      />,
    );
    await screen.findByTestId('sprout-workspace');
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('layout'));
    // The layout path wins: the space's registered shell is not rendered.
    expect(screen.queryByTestId('sprout-workspace-code-shell')).not.toBeInTheDocument();
    warn.mockRestore();
  });
});

/**
 * Rule tests: the invariants a caller could try to break.
 */
describe('SproutWorkspace rules', () => {
  it('rule: an unregistered space id never mounts a missing space — it degrades through the registry', async () => {
    // A caller can pass any string as a space id. The rule is that the registry
    // — not this component — decides what an unregistered id means, and it
    // always resolves to something renderable. Break the rule by having this
    // component render nothing (or throw) for an unknown id and this fails.
    render(<SproutWorkspace project={PROJECT} space="totally-unknown" host={makeHost()} />);
    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-space', 'code');
    expect(screen.getByTestId('sprout-workspace-code-shell')).toBeInTheDocument();
  });

  it('rule: a host supplied as a prop is the one the subtree reads (not the headless default)', async () => {
    // The tempting bug is not providing the host — then useHost() falls back to
    // a headless host (or throws without a provider). Break the rule by
    // dropping HostProvider and the probe's URL stops matching.
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost({ transport: { apiBaseURL: 'https://real.test', wsURL: '', authMode: 'bearer' } })}
      >
        <HostProbe />
      </SproutWorkspace>,
    );
    const probe = await screen.findByTestId('host-probe');
    expect(probe).toHaveAttribute('data-api', 'https://real.test');
    expect(probe).not.toHaveAttribute('data-api', '');
  });

  it('rule: onSpaceChange is not fired for mount, and fires exactly once per resolved-space change', async () => {
    const onSpaceChange = vi.fn();

    function Harness(): JSX.Element {
      const [space, setSpace] = useState<'code' | 'design'>('code');
      return (
        <>
          <button type="button" data-testid="to-design" onClick={() => setSpace('design')}>
            design
          </button>
          <button type="button" data-testid="same-design" onClick={() => setSpace('design')}>
            design again
          </button>
          <SproutWorkspace project={PROJECT} space={space} host={makeHost()} onSpaceChange={onSpaceChange} />
        </>
      );
    }

    render(<Harness />);
    await screen.findByTestId('sprout-workspace');
    expect(onSpaceChange).not.toHaveBeenCalled();

    act(() => {
      screen.getByTestId('to-design').click();
    });
    await screen.findByTestId('sprout-workspace-design-shell');
    expect(onSpaceChange).toHaveBeenCalledTimes(1);
    expect(onSpaceChange).toHaveBeenCalledWith('design');

    // Re-selecting the same space is not a change.
    act(() => {
      screen.getByTestId('same-design').click();
    });
    expect(onSpaceChange).toHaveBeenCalledTimes(1);
  });

  it('rule: a space registered after import is mountable (the registry is the source of truth)', async () => {
    // The registry is module-global mutable state, so the entry this test
    // creates is tracked at describe scope and disposed in `afterEach` — an
    // assertion failure must not leave a stray `test-space` behind for later
    // tests.
    registeredSpaceDisposer = registerWorkspaceMode({
      id: 'test-space',
      label: 'Test space',
      icon: (() => null) as never,
      available: () => true,
      Shell: () => <div data-testid="sprout-workspace-test-shell" />,
    });
    render(<SproutWorkspace project={PROJECT} space="test-space" host={makeHost()} />);
    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-space', 'test-space');
    expect(screen.getByTestId('sprout-workspace-test-shell')).toBeInTheDocument();
  });

  it('rule: the workspace root carries the `sprout-workspace` class the package stylesheet scopes to', async () => {
    // ws.5 (SP-160 §160a): the package's stylesheet is scoped to the workspace
    // root so it cannot leak onto the host page. That scope is only real if the
    // root element actually bears the class the stylesheet targets — this test
    // is the DOM side of that contract (the artifact test asserts the CSS side).
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} />);

    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveClass('sprout-workspace');
  });

  it('rule: the workspace root carries the resolved data-theme the scoped guards key on', async () => {
    // The stylesheet's light-theme guards were re-scoped from `:root[data-theme]`
    // to `.sprout-workspace[data-theme]`, so the attribute must live on the
    // workspace root for those rules to match. Without it the scoped guards are
    // dead.
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} />);

    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-theme', 'dark');
  });

  it('rule: a supplied className extends the workspace root, never replaces the scope class', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} className="host-shell" />);

    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveClass('sprout-workspace');
    expect(workspace).toHaveClass('host-shell');
  });
});
