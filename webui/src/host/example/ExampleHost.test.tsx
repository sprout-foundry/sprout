/**
 * host.9 — the example host (SP-160 Acceptance criteria 3).
 *
 * Pins the criterion end to end: a minimal host with its own chrome and theme
 * mounts two spaces, receives a notification and a navigation intent. The
 * example imports only the two public entry points; the companion
 * `exampleHostBoundary.test` statically enforces that, and this suite proves
 * the runtime behavior the criterion names.
 *
 * The mounted spaces are the built-in workspace modes (`code`, `design`) —
 * the two every workspace offers — so the test also demonstrates switching
 * between them. `design` is registered through the public API in one case to
 * show a host can mount a space it registered itself.
 */

import { fireEvent, render, screen, within } from '@testing-library/react';
import { MonitorPlay } from 'lucide-react';
import { registerWorkspaceMode } from '../../views/index';
import { HostProvider, useHost } from '../index';
import type { HostNotification, HostNavigationIntent } from '../index';
import type { ExampleHostState } from './ExampleHost';
import { ExampleHost, ExampleHostRoot, createExampleHost } from './ExampleHost';

function freshState(): ExampleHostState {
  return { notifications: [], intents: [], activeSpaceId: 'code' };
}

describe('the example host builds a real SproutHost', () => {
  it('supplies its own chrome nodes, theme, capabilities and transport', () => {
    const state = freshState();
    const host = createExampleHost(state);

    // Its own chrome: React nodes for both header areas.
    expect(host.chrome?.headerLeft).toBeTruthy();
    expect(host.chrome?.headerRight).toBeTruthy();
    // Its own theme: a mode plus a token override.
    expect(host.theme?.mode).toBe('dark');
    expect(host.theme?.tokens?.['--accent-primary']).toBe('#8b5cf6');
    // Transport, entitlements and capabilities are host-provided.
    expect(host.transport.apiBaseURL).toBe('https://example.test/api');
    expect(host.transport.authMode).toBe('bearer');
    expect(host.entitlements?.usageSummary?.label).toBe('42 example credits remaining');
    expect(host.capabilities.chat).toBe(true);
    expect(host.capabilities.workspaceSwitching).toBe(true);
  });
});

describe('the example host renders with its own chrome and theme', () => {
  it('renders the chrome nodes it supplied, inside its own theme', () => {
    const state = freshState();
    const host = createExampleHost(state);
    render(<ExampleHost host={host} state={state} />);

    expect(screen.getByTestId('host-example')).toBeInTheDocument();
    expect(screen.getByTestId('host-example-chrome-left')).toHaveTextContent('Example Host');
    expect(screen.getByTestId('host-example-usage')).toHaveTextContent('42 example credits remaining');
    // The theme the host supplied is the theme the rendered root carries.
    expect(screen.getByTestId('host-example')).toHaveAttribute('data-theme', 'dark');
  });

  it('reads the host through the provider (useHost) so the chrome matches it', () => {
    const host = createExampleHost(freshState());
    let seen: ReturnType<typeof useHost> | undefined;
    function Probe() {
      seen = useHost();
      return null;
    }
    render(
      <HostProvider host={host}>
        <Probe />
        <ExampleHostRoot state={freshState()} />
      </HostProvider>,
    );
    expect(seen).toBe(host);
  });
});

describe('the example host mounts two spaces and switches between them', () => {
  it('offers two spaces and mounts the active one', () => {
    const state = freshState();
    render(<ExampleHost host={createExampleHost(state)} state={state} />);

    expect(screen.getByTestId('host-example-space-code')).toBeInTheDocument();
    expect(screen.getByTestId('host-example-space-design')).toBeInTheDocument();
    expect(screen.getByTestId('host-example-space')).toHaveAttribute('data-space', 'code');
  });

  it('switching spaces mounts the other shell (the layout re-arranges)', () => {
    const state = freshState();
    render(<ExampleHost host={createExampleHost(state)} state={state} />);

    // The active space starts on code; its view lands in the center slot.
    const space = screen.getByTestId('host-example-space');
    expect(space).toHaveAttribute('data-space', 'code');
    expect(space).toHaveAttribute('data-view-slot', 'center');

    fireEvent.click(screen.getByTestId('host-example-space-design'));

    // The switch re-mounts the active space: the view moves to the left slot,
    // and the pressed control follows.
    expect(state.activeSpaceId).toBe('design');
    expect(screen.getByTestId('host-example-space')).toHaveAttribute('data-space', 'design');
    expect(screen.getByTestId('host-example-space')).toHaveAttribute('data-view-slot', 'left');
    expect(screen.getByTestId('host-example-space-design')).toHaveAttribute('aria-pressed', 'true');
  });

  it('can mount a space a host registered through the public API', () => {
    const dispose = registerWorkspaceMode({
      id: 'example-preview',
      label: 'Example Preview',
      icon: MonitorPlay,
      hint: 'A host-registered space',
      available: () => true,
      Shell: () => null,
    });
    try {
      const state = freshState();
      render(<ExampleHost host={createExampleHost(state)} state={state} />);
      expect(screen.getByTestId('host-example-space-example-preview')).toBeInTheDocument();
    } finally {
      dispose();
    }
  });
});

describe('the example host receives a notification', () => {
  it('a post to the host sink is received and displayed by the host', () => {
    const state = freshState();
    const host = createExampleHost(state);
    render(<ExampleHost host={host} state={state} />);

    // Sprout (or anything holding the host) posts to the sink...
    const notification: HostNotification = { level: 'success', title: 'Saved', message: 'All changes stored' };
    host.notifications.post(notification);

    expect(state.notifications).toEqual([notification]);
  });

  it('the host chrome surfaces a received notification', () => {
    const state = freshState();
    const host = createExampleHost(state);
    const { rerender } = render(<ExampleHost host={host} state={state} />);

    expect(screen.queryByTestId('host-example-notifications')).toBeNull();

    host.notifications.post({ level: 'error', title: 'Failed', message: 'The build failed' });
    rerender(<ExampleHost host={host} state={state} />);

    const list = screen.getByTestId('host-example-notifications');
    expect(list).toHaveTextContent('Failed: The build failed');
    expect(within(list).getByRole('listitem')).toHaveAttribute('data-level', 'error');
  });

  it('the example notify control posts through the host sink', () => {
    const state = freshState();
    render(<ExampleHost host={createExampleHost(state)} state={state} />);

    fireEvent.click(screen.getByTestId('host-example-notify'));

    expect(state.notifications.length).toBe(1);
    expect(state.notifications[0].level).toBe('info');
  });
});

describe('the example host receives a navigation intent', () => {
  it('the host resolves an account intent and records it', () => {
    const state = freshState();
    const host = createExampleHost(state);
    const { rerender } = render(<ExampleHost host={host} state={state} />);

    // Sprout asks the host to navigate out...
    const intent: HostNavigationIntent = { type: 'account' };
    host.navigation.open(intent);

    expect(state.intents).toEqual([intent]);

    rerender(<ExampleHost host={host} state={state} />);
    expect(screen.getByTestId('host-example-nav-log')).toHaveTextContent('account');
  });

  it('the example account control dispatches the intent through the host', () => {
    const state = freshState();
    const seen: HostNavigationIntent[] = [];
    render(<ExampleHost host={createExampleHost(state)} state={state} onIntent={(i) => seen.push(i)} />);

    fireEvent.click(screen.getByTestId('host-example-open-account'));

    expect(state.intents).toEqual([{ type: 'account' }]);
    expect(seen).toEqual([{ type: 'account' }]);
  });
});
