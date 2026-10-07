/**
 * SproutProviders.
 *
 * Pins the contract the views rely on: rendering through the wrapper gives a
 * consumer every context the views read — the adapter (`useSproutAdapter` /
 * `useSproutFetch`), the events transport (`useEvents`), notifications
 * (`useNotifications`), the buffer/editor/pane stack (`useEditorManager`,
 * `useBufferManager`, `usePaneManager`), hotkeys (`useHotkeys`) and the theme
 * (`useTheme`). It also pins the state a host passes in: a supplied events
 * transport is the one every consumer sees, and the wrapper provides an events
 * transport exactly once.
 *
 * The providers underneath reach real services (the hotkey API, the theme
 * store, the adapter singleton). The tests mock the ones that touch the
 * network or global singletons so the cases assert context presence, not the
 * service behavior each provider's own suite covers.
 */

import { render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useEvents } from '@sprout/events';
import type { EventsProvider } from '@sprout/events';
import { useEditorManager, useBufferManager, usePaneManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useNotifications } from '../contexts/NotificationContext';
import { useTheme } from '../contexts/ThemeContext';
import { useSproutAdapter, useSproutFetch } from '../contexts/SproutAdapterContext';
import { HostProvider } from '../host/HostProvider';
import { SproutProviders } from './SproutProviders';

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

function makeEventsProvider(overrides: Partial<EventsProvider> = {}): EventsProvider {
  return {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onEvent: vi.fn(),
    removeEvent: vi.fn(),
    sendEvent: vi.fn(),
    isConnected: vi.fn(() => true),
    onReconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn(() => 0),
    ...overrides,
  } as EventsProvider;
}

let seenEvents: EventsProvider[] = [];

function Consumer(): JSX.Element {
  const events = useEvents();
  const adapter = useSproutAdapter();
  const fetch = useSproutFetch();
  const { addNotification } = useNotifications();
  const editor = useEditorManager();
  const buffers = useBufferManager();
  const panes = usePaneManager();
  const { hotkeys, isLoaded } = useHotkeys();
  const { theme, themePack } = useTheme();

  seenEvents.push(events);

  return (
    <div
      data-testid="consumer"
      data-adapter={adapter === null ? 'null' : 'resolved'}
      data-fetch={typeof fetch}
      data-notify={typeof addNotification}
      data-editor={typeof editor?.openFile}
      data-buffers={typeof buffers?.openWorkspaceBuffer}
      data-panes={typeof panes?.switchPane}
      data-hotkeys={hotkeys === null ? 'null' : 'loaded'}
      data-hotkeys-loaded={String(isLoaded)}
      data-theme={theme}
      data-theme-pack={themePack?.id}
    />
  );
}

function renderWithProviders(children: ReactNode, props: Parameters<typeof SproutProviders>[0] = {}) {
  // The host contract wraps the stack — the host supplies it (index.tsx does
  // this in the app). The wrapper itself does not own the host, so tests mount
  // it the same way the app does.
  return render(
    <HostProvider>
      <SproutProviders {...props}>{children}</SproutProviders>
    </HostProvider>,
  );
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  seenEvents = [];
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('SproutProviders', () => {
  it('renders its children', async () => {
    renderWithProviders(<div data-testid="child">hello</div>, { eventsProvider: makeEventsProvider() });
    expect(await screen.findByTestId('child')).toHaveTextContent('hello');
  });

  it('provides every context the views read without throwing', async () => {
    renderWithProviders(<Consumer />, { eventsProvider: makeEventsProvider() });

    const consumer = await screen.findByTestId('consumer');
    expect(consumer).toHaveAttribute('data-fetch', 'function');
    expect(consumer).toHaveAttribute('data-notify', 'function');
    expect(consumer).toHaveAttribute('data-editor', 'function');
    expect(consumer).toHaveAttribute('data-buffers', 'function');
    expect(consumer).toHaveAttribute('data-panes', 'function');
    expect(consumer.getAttribute('data-theme')).toBeTruthy();
    expect(consumer.getAttribute('data-theme-pack')).toBeTruthy();
  });

  it('hands the supplied events transport to every consumer unchanged', async () => {
    const provider = makeEventsProvider();
    renderWithProviders(<Consumer />, { eventsProvider: provider });

    await screen.findByTestId('consumer');
    expect(seenEvents).toContain(provider);
  });

  it('provides an events transport exactly once (no nested double-provide)', async () => {
    const provider = makeEventsProvider();
    renderWithProviders(<Consumer />, { eventsProvider: provider });

    await screen.findByTestId('consumer');
    // Each event in seenEvents is the same reference — a second, nested
    // provider would surface a different one and break the identity contract.
    expect(seenEvents.length).toBeGreaterThan(0);
    expect(new Set(seenEvents).size).toBe(1);
    expect(seenEvents[0]).toBe(provider);
  });

  it('mints a default events transport when none is supplied', async () => {
    // Without a supplied transport the wrapper defaults to the lazily loaded
    // local one. The consumer only mounts once that transport is present, so a
    // view never renders against a missing events context — and when it does
    // mount, the context it reads is a real transport.
    renderWithProviders(<Consumer />);

    const consumer = await screen.findByTestId('consumer');
    expect(consumer).toBeInTheDocument();
    expect(seenEvents.length).toBeGreaterThan(0);
    expect(seenEvents[0]).toBeDefined();

    // The wrapper provides its default transport exactly once: every consumer
    // reads the same reference (a nested second provider would differ).
    expect(new Set(seenEvents).size).toBe(1);
  });
});
