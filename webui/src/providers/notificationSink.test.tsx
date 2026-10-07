/**
 * Every notification reaches the host.
 *
 * `useNotifications().addNotification` is the web UI's own notification entry
 * point (the ~61 call sites across `webui/src`). This suite pins that a
 * notification raised through it is delivered to the active host's sink
 * exactly once, while the local host keeps today's single in-app entry and a
 * sink-less provider (pure `@sprout/ui`) still dispatches locally — proving
 * the double-post hazard is closed.
 *
 * A separate path, `notificationBus.notify` (host.7), posts once to the host
 * sink; pinned here too so routing `addNotification` does not regress it.
 *
 * The active host is a module-level singleton, so each case re-imports the
 * host-aware modules after `vi.resetModules()` for a clean slate. The API
 * service is mocked so the real `HotkeyProvider` in the stack does not post
 * its own "Hotkey Load Error" notification through the same sink.
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { makeTestHost } from '../host/testHost';
import type { SproutHost } from '../host/types';

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

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

type SproutProvidersModule = typeof import('./SproutProviders');
type NotificationsModule = typeof import('../contexts/NotificationContext');
type UiModule = typeof import('@sprout/ui');

interface Lane {
  accessor: typeof import('../host/accessor');
  bus: typeof import('../services/notificationBus');
  ui: UiModule;
  localHost: typeof import('../host/localHost').localHost;
  SproutProviders: SproutProvidersModule['SproutProviders'];
  HostProvider: typeof import('../host/HostProvider').HostProvider;
}

/**
 * A provider stack whose events transport is a no-op stub, so the wrapper does
 * not lazily pull in the real transport (which reaches the WebSocket service).
 */
function stubEventsProvider() {
  return {
    connect: () => {},
    disconnect: () => {},
    onEvent: () => () => {},
    removeEvent: () => {},
    sendEvent: () => {},
    isConnected: () => true,
    onReconnect: () => () => {},
    freeze: () => {},
    resume: () => {},
    resetAndReconnect: () => {},
    getQueuedMessageCount: () => 0,
  };
}

async function loadLane(): Promise<Lane> {
  vi.resetModules();
  const accessor = await import('../host/accessor');
  const bus = await import('../services/notificationBus');
  const ui = await import('@sprout/ui');
  const { localHost } = await import('../host/localHost');
  const { SproutProviders } = await import('./SproutProviders');
  const { HostProvider } = await import('../host/HostProvider');
  return { accessor, bus, ui, localHost, SproutProviders, HostProvider };
}

function Consumer({ Notifications, duration }: { Notifications: NotificationsModule; duration?: number }): JSX.Element {
  const { notifications, addNotification } = Notifications.useNotifications();
  return (
    <div>
      <span data-testid="count">{notifications.length}</span>
      <button
        type="button"
        data-testid="add"
        onClick={() => addNotification('warning', 'Sink Title', 'Sink body', duration)}
      >
        add
      </button>
    </div>
  );
}

function renderWithinProviders(
  SproutProviders: SproutProvidersModule['SproutProviders'],
  HostProvider: typeof import('../host/HostProvider').HostProvider,
  child: JSX.Element,
) {
  act(() => {
    root.render(
      <HostProvider>
        <SproutProviders eventsProvider={stubEventsProvider() as never}>{child}</SproutProviders>
      </HostProvider>,
    );
  });
}

describe('addNotification reaches the active host sink', () => {
  it('routes a recorded host sink exactly once, with the right shape', async () => {
    const lane = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    lane.accessor.setActiveHost(host);

    renderWithinProviders(lane.SproutProviders, lane.HostProvider, <Consumer Notifications={lane.ui} />);

    await act(async () => {
      container.querySelector('[data-testid="add"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith({
      level: 'warning',
      title: 'Sink Title',
      message: 'Sink body',
      duration: undefined,
      action: undefined,
    });
    // No double-post: the sink is the only delivery path for a host, so the
    // provider's own state stays empty (the host owns display).
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('0');
  });

  it('clamps an oversized duration before the sink', async () => {
    const lane = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    lane.accessor.setActiveHost(host);

    renderWithinProviders(
      lane.SproutProviders,
      lane.HostProvider,
      <Consumer Notifications={lane.ui} duration={999999} />,
    );

    await act(async () => {
      container.querySelector('[data-testid="add"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith({
      level: 'warning',
      title: 'Sink Title',
      message: 'Sink body',
      duration: 60000,
      action: undefined,
    });
  });

  it('clamps a negative duration before the sink', async () => {
    const lane = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    lane.accessor.setActiveHost(host);

    renderWithinProviders(
      lane.SproutProviders,
      lane.HostProvider,
      <Consumer Notifications={lane.ui} duration={-100} />,
    );

    await act(async () => {
      container.querySelector('[data-testid="add"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(post).toHaveBeenCalledWith({
      level: 'warning',
      title: 'Sink Title',
      message: 'Sink body',
      duration: 0,
      action: undefined,
    });
  });
});

describe('localHost keeps today’s in-app behavior via addNotification', () => {
  it('produces exactly one in-app entry (no double-post)', async () => {
    const lane = await loadLane();
    lane.ui.notificationBus._resetForTesting();
    const postSpy = vi.fn();
    lane.accessor.setActiveHost({
      ...lane.localHost,
      notifications: {
        post: (n) => {
          postSpy(n);
          lane.localHost.notifications.post(n);
        },
      },
    });

    renderWithinProviders(lane.SproutProviders, lane.HostProvider, <Consumer Notifications={lane.ui} />);

    await act(async () => {
      container.querySelector('[data-testid="add"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // The local host's sink forwards to the in-app bus exactly once, and the
    // provider's bus subscription adds exactly one entry: no double-post.
    expect(postSpy).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
  });
});

describe('no host active — sink-less provider keeps local dispatch', () => {
  it('still adds one entry when the provider is used directly (pure @sprout/ui)', async () => {
    const lane = await loadLane();
    // No host set: the raw @sprout/ui provider (no sink) is the delivery path.
    function DirectConsumer(): JSX.Element {
      const { notifications, addNotification } = lane.ui.useNotifications();
      return (
        <div>
          <span data-testid="count">{notifications.length}</span>
          <button type="button" data-testid="add-direct" onClick={() => addNotification('info', 'Direct', 'Body')}>
            add
          </button>
        </div>
      );
    }

    act(() => {
      root.render(
        <lane.ui.NotificationProvider>
          <DirectConsumer />
        </lane.ui.NotificationProvider>,
      );
    });

    act(() => {
      container.querySelector('[data-testid="add-direct"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('[data-testid="count"]')?.textContent).toBe('1');
  });
});

describe('notificationBus.notify (host.7) still posts once to a recording host', () => {
  it('records exactly one sink call', async () => {
    const lane = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    lane.accessor.setActiveHost(host);

    lane.bus.notificationBus.notify('success', 'Bus Title', 'Bus Body', 1000);

    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith({
      level: 'success',
      title: 'Bus Title',
      message: 'Bus Body',
      duration: 1000,
      action: undefined,
    });
  });
});
