/**
 * SproutProviders — the connection state the provider catalog fetches against.
 *
 * Omitted `isConnected`, the wrapper tracks the events transport: the state
 * follows the transport's `connection_status` events, so a transport that
 * opens after mount (the composed workspace calls `connect()` in an effect)
 * still lets the catalog load. The catalog provider is stubbed to expose the
 * flag it receives.
 */

import { act, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import type { EventsProvider, SproutEventCallback } from '@sprout/events';
import { HostProvider } from '../host/HostProvider';
import { SproutProviders } from './SproutProviders';

vi.mock('../contexts/ProviderCatalogContext', () => ({
  ProviderCatalogProvider: ({ isConnected, children }: { isConnected: boolean; children: ReactNode }) => (
    <div data-testid="catalog" data-connected={String(isConnected)}>
      {children}
    </div>
  ),
}));
vi.mock('../services/api', () => {
  const instance = {
    getHotkeys: async () => ({ hotkeys: [], platform: 'test' }),
    applyHotkeyPreset: async () => ({ hotkeys: [] }),
    getWorkspace: async () => ({ workspace_root: '' }),
  };
  return { ApiService: { getInstance: () => instance } };
});

function makeTransport() {
  let open = false;
  const listeners = new Set<SproutEventCallback>();
  const provider = {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onEvent: vi.fn((cb: SproutEventCallback) => listeners.add(cb)),
    removeEvent: vi.fn((cb: SproutEventCallback) => listeners.delete(cb)),
    sendEvent: vi.fn(),
    isConnected: vi.fn(() => open),
    onReconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn(() => 0),
    flushQueuedMessages: vi.fn(() => 0),
  } as unknown as EventsProvider;
  const setOpen = (next: boolean) => {
    open = next;
    listeners.forEach((cb) => cb({ type: 'connection_status', data: { connected: next } }));
  };
  return { provider, setOpen, listeners };
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

describe('SproutProviders connection state', () => {
  it('follows the transport as it opens and closes after mount', async () => {
    const { provider, setOpen } = makeTransport();
    render(
      <HostProvider>
        <SproutProviders eventsProvider={provider}>
          <span />
        </SproutProviders>
      </HostProvider>,
    );

    const catalog = await screen.findByTestId('catalog');
    expect(catalog).toHaveAttribute('data-connected', 'false');

    act(() => setOpen(true));
    expect(catalog).toHaveAttribute('data-connected', 'true');

    act(() => setOpen(false));
    expect(catalog).toHaveAttribute('data-connected', 'false');
  });

  it('lets an explicit isConnected win and unsubscribes on unmount', async () => {
    const { provider, setOpen, listeners } = makeTransport();
    const { unmount } = render(
      <HostProvider>
        <SproutProviders eventsProvider={provider} isConnected={true}>
          <span />
        </SproutProviders>
      </HostProvider>,
    );

    const catalog = await screen.findByTestId('catalog');
    act(() => setOpen(false));
    expect(catalog).toHaveAttribute('data-connected', 'true');

    unmount();
    expect(listeners.size).toBe(0);
  });
});
