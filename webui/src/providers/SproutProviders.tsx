import { EventsContextProvider } from '@sprout/events';
import type { EventsProvider } from '@sprout/events';
import { useCallback, useSyncExternalStore, type ReactNode } from 'react';
import { EditorManagerProvider } from '../contexts/EditorManagerContext';
import { HotkeyProvider } from '../contexts/HotkeyContext';
import { NotificationProvider, type NotificationSink } from '../contexts/NotificationContext';
import { PlatformNavProvider } from '../contexts/PlatformNavContext';
import { PluginContextProvider } from '../contexts/PluginContext';
import { ProviderCatalogProvider } from '../contexts/ProviderCatalogContext';
import { SproutAdapterProvider } from '../contexts/SproutAdapterContext';
import { ThemeProvider } from '../contexts/ThemeContext';
import { getActiveHost } from '../host/accessor';
import { notificationBus } from '../services/notificationBus';

/**
 * The provider stack a view needs, behind one wrapper.
 *
 * A host that composes the individual views would otherwise assemble the
 * whole web UI context stack by hand — the adapter, the events transport,
 * notifications, the buffer/editor/pane/hotkey contexts, the theme, the
 * plugin and provider catalogs — in exactly the order the app root does. This
 * wrapper is that order, written once: a view rendered inside it finds every
 * context it reads.
 *
 * The order is the app root's and is load-bearing: `NotificationProvider`
 * wraps everything (the hotkey context notifies through it), the events
 * provider wraps the views that read the transport, and the adapter sits
 * below the events provider but above the contexts that fetch through it.
 * The wrapper adds no behavior of its own.
 *
 * Two provider values are supplied by the caller when it owns them upstream,
 * and defaulted here otherwise:
 *
 * - `eventsProvider` — the events transport. A host that runs its own
 *   transport (or shares one across several mounted workspaces) passes it in;
 *   omitted, the wrapper owns a single default transport for its own tree,
 *   created once and never replaced, so the provider identity stays stable
 *   across re-renders.
 * - `isConnected` — the connection state `ProviderCatalogProvider` fetches
 *   its catalog against, defaulting to the default transport's own state (and
 *   to `false` when a transport is supplied without one).
 */
export interface SproutProvidersProps {
  /** The events transport every view under the wrapper reads. */
  eventsProvider?: EventsProvider;
  /** The connection state the provider catalog fetches against. */
  isConnected?: boolean;
  /**
   * Overrides the notification sink. Omitted, the wrapper routes
   * `addNotification` to the active host's sink (falling back to the in-app
   * bus when no host is active).
   */
  sink?: NotificationSink;
  children: ReactNode;
}

interface EventsProviderModule {
  LocalEventsProvider: new () => EventsProvider;
}

/**
 * The default transport, loaded lazily.
 *
 * The concrete transport reaches the WebSocket service, the browser git and
 * the WASM shell — the heavy parts a host should not pull in just by
 * importing this wrapper. A caller that supplies its own `eventsProvider`
 * never triggers the load; one that does not gets the local transport the app
 * uses.
 */
let eventsProviderModule: Promise<EventsProviderModule> | null = null;
let defaultEventsProvider: EventsProvider | null = null;
const defaultProviderListeners = new Set<() => void>();

function loadDefaultEventsProvider(): Promise<EventsProviderModule> {
  if (!eventsProviderModule) {
    eventsProviderModule = import('../services/localEventsProvider');
  }
  return eventsProviderModule;
}

function getDefaultEventsProvider(): EventsProvider | null {
  return defaultEventsProvider;
}

function setDefaultEventsProvider(provider: EventsProvider): void {
  defaultEventsProvider = provider;
  defaultProviderListeners.forEach((listener) => listener());
}

function subscribeDefaultEventsProvider(listener: () => void): () => void {
  defaultProviderListeners.add(listener);
  return () => defaultProviderListeners.delete(listener);
}

function useDefaultEventsProvider(): EventsProvider | null {
  const provider = useSyncExternalStore(subscribeDefaultEventsProvider, getDefaultEventsProvider);

  // Kick off the lazy load and publish the instance once it resolves. The
  // promise is shared, so several wrappers (and a StrictMode double-invoke)
  // resolve to the single default transport.
  if (!provider) {
    void loadDefaultEventsProvider().then((mod) => {
      if (!defaultEventsProvider) setDefaultEventsProvider(new mod.LocalEventsProvider());
    });
  }

  return provider;
}

/**
 * The wrapper itself. Nests the provider stack around `children` in the app
 * root's order and renders its children unchanged. Until a lazily loaded
 * default transport is present nothing renders: a view must never mount
 * against a half-built stack.
 */
export function SproutProviders({
  eventsProvider,
  isConnected,
  sink: sinkProp,
  children,
}: SproutProvidersProps): JSX.Element {
  const defaultProvider = useDefaultEventsProvider();
  const events = eventsProvider ?? defaultProvider;

  // Route notifications raised through `addNotification` to the active host's
  // sink, so every notification reaches the host. The function is stable (it
  // reads the host at call time, not at render time), so the provider's
  // `addNotification` identity does not churn. When no host is active (pure
  // service contexts, tests) it falls back to the in-app bus, and the local
  // host's sink IS that bus, so local behavior is unchanged either way.
  const defaultSink = useCallback<NotificationSink>((notification) => {
    const host = getActiveHost();
    if (host) {
      host.notifications.post(notification);
    } else {
      notificationBus.notify(
        notification.level,
        notification.title,
        notification.message,
        notification.duration,
        notification.action,
      );
    }
  }, []);

  const sink = sinkProp ?? defaultSink;

  if (!events) return <></>;

  const connected = isConnected ?? events.isConnected();

  return (
    <NotificationProvider sink={sink}>
      <EventsContextProvider provider={events}>
        <SproutAdapterProvider>
          <PlatformNavProvider>
            <PluginContextProvider>
              <ThemeProvider>
                <HotkeyProvider>
                  <EditorManagerProvider>
                    <ProviderCatalogProvider isConnected={connected}>{children}</ProviderCatalogProvider>
                  </EditorManagerProvider>
                </HotkeyProvider>
              </ThemeProvider>
            </PluginContextProvider>
          </PlatformNavProvider>
        </SproutAdapterProvider>
      </EventsContextProvider>
    </NotificationProvider>
  );
}

SproutProviders.displayName = 'SproutProviders';

export default SproutProviders;
