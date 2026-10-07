/**
 * host.7 — the web UI posts its notifications through the host sink.
 *
 * (a) With an active test host whose `notifications.post` records calls, a post
 *     made through the web UI's `notificationBus` is routed to the host sink
 *     and NOT to the raw in-app bus.
 * (b) With `localHost` active, a post reaches the local in-app bus (the path
 *     that drives toasts + the NotificationCenter) — behavior preserved.
 * (c) A host that supplies `notifications.count` exposes that value where the
 *     count is read (the header count component); localHost exposes none.
 *
 * The notification modules hold a module-level "active host" singleton, so each
 * case re-imports them after `vi.resetModules()` to get a clean slate — the same
 * seam the config/mode tests use for their live bindings.
 */

import { render, screen } from '@testing-library/react';
import { createElement } from 'react';
import { makeTestHost } from './testHost';
import type { SproutHost } from './types';

async function loadLane() {
  vi.resetModules();
  const accessor = await import('./accessor');
  const bus = await import('../services/notificationBus');
  const ui = await import('@sprout/ui');
  const { localHost } = await import('./localHost');
  const { HostNotificationCount } = await import('./index');
  return { accessor, bus, ui, localHost, HostNotificationCount };
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

describe('web UI notifications route through the host sink', () => {
  it('posts to the active host sink and not to the raw bus', async () => {
    const { accessor, bus, ui } = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    accessor.setActiveHost(host);

    const rawListener = vi.fn();
    ui.notificationBus.onNotification(rawListener);

    bus.notificationBus.notify('info', 'Title', 'Body', 3000);

    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith({ level: 'info', title: 'Title', message: 'Body', duration: 3000 });
    expect(rawListener).not.toHaveBeenCalled();
  });

  it('carries the notification action through to the host sink', async () => {
    const { accessor, bus } = await loadLane();
    const host = makeTestHost() as SproutHost;
    const post = vi.fn();
    host.notifications = { post };
    accessor.setActiveHost(host);

    const onClick = vi.fn();
    bus.notificationBus.notify('error', 'Failed', 'Retry', undefined, { label: 'Retry', onClick });

    expect(post).toHaveBeenCalledWith({
      level: 'error',
      title: 'Failed',
      message: 'Retry',
      duration: undefined,
      action: { label: 'Retry', onClick },
    });
  });

  it('falls back to the raw bus when no host is active', async () => {
    const { bus, ui } = await loadLane();
    const rawListener = vi.fn();
    ui.notificationBus.onNotification(rawListener);

    bus.notificationBus.notify('warning', 'No host', 'fallback');

    expect(rawListener).toHaveBeenCalledTimes(1);
    expect(rawListener.mock.calls[0][0]).toMatchObject({ type: 'warning', title: 'No host', message: 'fallback' });
  });

  it('delegates the non-notify bus methods to the raw bus', async () => {
    const { accessor, bus } = await loadLane();
    const host = makeTestHost() as SproutHost;
    accessor.setActiveHost(host);

    const controlListener = vi.fn();
    bus.notificationBus.onControlEvent(controlListener);
    bus.notificationBus.markAllRead();

    expect(controlListener).toHaveBeenCalledWith({ kind: 'mark_all_read' });
  });
});

describe('localHost keeps today’s in-app notifications', () => {
  it('forwards a post to the in-app bus (toasts / center path)', async () => {
    const { accessor, bus, ui, localHost } = await loadLane();
    accessor.setActiveHost(localHost);

    const rawListener = vi.fn();
    ui.notificationBus.onNotification(rawListener);

    bus.notificationBus.notify('success', 'Saved', 'All good');

    expect(rawListener).toHaveBeenCalledTimes(1);
    expect(rawListener.mock.calls[0][0]).toMatchObject({ type: 'success', title: 'Saved', message: 'All good' });
  });

  it('does not expose an unread count', async () => {
    const { localHost } = await loadLane();
    expect(localHost.notifications.count).toBeUndefined();
  });
});

describe('host notification count', () => {
  it('renders the host-provided count', async () => {
    const { accessor, HostNotificationCount } = await loadLane();
    const host = makeTestHost() as SproutHost;
    host.notifications = { post: vi.fn(), count: 7 };
    accessor.setActiveHost(host);

    render(createElement(HostNotificationCount));

    expect(screen.getByTestId('host-notification-count')).toHaveTextContent('7');
  });

  it('renders nothing when the host supplies no count (localHost)', async () => {
    const { accessor, localHost, HostNotificationCount } = await loadLane();
    accessor.setActiveHost(localHost);

    render(createElement(HostNotificationCount));

    expect(screen.queryByTestId('host-notification-count')).toBeNull();
  });

  it('renders nothing for a zero count', async () => {
    const { accessor, HostNotificationCount } = await loadLane();
    const host = makeTestHost() as SproutHost;
    host.notifications = { post: vi.fn(), count: 0 };
    accessor.setActiveHost(host);

    render(createElement(HostNotificationCount));

    expect(screen.queryByTestId('host-notification-count')).toBeNull();
  });
});
