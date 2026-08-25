import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { supportsSettings } from '../config/mode';
import { ApiService, type ProviderOption } from '../services/api';
import { debugLog } from '../utils/log';

interface ProviderCatalogContextValue {
  providers: ProviderOption[];
  isLoading: boolean;
  /** Daemon-reported active provider id from /api/providers (best-effort; the
   *  WebSocket `metrics_update.provider` is the live source during a chat). */
  currentProvider: string;
  currentModel: string;
  refresh: () => void;
  /** Resolve a raw provider id (e.g. `openrouter`) to its display name
   *  (`OpenRouter (Recommended)`). Returns the id unchanged when the catalog
   *  is empty or the id is unknown — keeps the call site safe for the
   *  pre-fetch and offline-disconnected states. */
  getProviderName: (id: string | undefined | null) => string;
  /** Force a catalog load when the caller knows it may be stale (e.g. modal
   *  opened during a disconnect, or tab activated after a cold start).
   *  No-op when data is already present, loading is in-flight, or the
   *  connection is down. Callers should invoke from useEffect mount or
   *  user-action handlers such as modal-open and tab-activate. */
  ensureLoaded: () => void;
}

const EMPTY_PROVIDERS: ProviderOption[] = [];

const FALLBACK_VALUE: ProviderCatalogContextValue = {
  providers: EMPTY_PROVIDERS,
  isLoading: false,
  currentProvider: '',
  currentModel: '',
  /* eslint-disable @typescript-eslint/no-empty-function --
     Fallthrough no-ops used only when the provider isn't mounted. They
     satisfy the context shape so consumers (e.g. useProviderCatalog()
     during the brief disconnected render) never crash on `undefined()`.
     The real fetch logic lives in ProviderCatalogProvider below. */
  refresh: () => {},
  // Without a catalog, return the raw id — consumers (e.g. status bar)
  // already treat that as a safe baseline rather than crashing.
  getProviderName: (id) => id ?? '',
  ensureLoaded: () => {},
  /* eslint-enable @typescript-eslint/no-empty-function */
};

const ProviderCatalogContext = createContext<ProviderCatalogContextValue>(FALLBACK_VALUE);

export function useProviderCatalog(): ProviderCatalogContextValue {
  return useContext(ProviderCatalogContext);
}

interface ProviderCatalogProviderProps {
  isConnected: boolean;
  children: ReactNode;
}

/**
 * Single source of truth for the provider catalog. Replaces the per-hook
 * fetches in `useSidebarModel` (drives Sidebar + agent dropdowns) and
 * `useSettingsState` (drives Settings → Subagents/Providers/Commit-Review),
 * and feeds the bottom status bar's display-name lookup.
 *
 * Before this existed, the same catalog was fetched from three endpoints
 * (`/api/providers`, `/api/onboarding/status`, `/api/settings/subagent-types`)
 * and held in three independent state slots — any backend change had to be
 * mirrored or the UIs drifted (e.g. one tab showed `Test Provider`, the
 * status bar showed the raw `openrouter` id, etc.). This provider fetches
 * once per connect and exposes a stable lookup to every consumer.
 */
export function ProviderCatalogProvider({ isConnected, children }: ProviderCatalogProviderProps): JSX.Element {
  const [providers, setProviders] = useState<ProviderOption[]>(EMPTY_PROVIDERS);
  const [isLoading, setIsLoading] = useState(false);
  const [currentProvider, setCurrentProvider] = useState('');
  const [currentModel, setCurrentModel] = useState('');
  const [refreshTick, setRefreshTick] = useState(0);

  // Grace timer for Bug A: prevents catalog blanking on brief WS disconnects.
  // If the connection drops for < 1500 ms we keep the stale data; only a
  // persistent disconnect clears it.
  const disconnectTimerRef = useRef<NodeJS.Timeout | null>(null);

  const refresh = useCallback(() => setRefreshTick((n) => n + 1), []);

  // Bump the tick only when connected, the catalog is empty, and no fetch is
  // already in-flight. This is the primary "I think the catalog is stale"
  // signal — call from modal-open, tab-activate, or explicit user refresh.
  // `refreshTick` is intentionally read so this callback closes over the
  // current tick value (which the bump itself just incremented).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const ensureLoaded = useCallback(() => {
    if (providers.length === 0 && !isLoading && isConnected) {
      setRefreshTick((n) => n + 1);
    }
  }, [providers.length, isLoading, isConnected]);

  useEffect(() => {
    // We clear the catalog on disconnect so a stale server-side provider
    // list doesn't leak past a reconnect. Without this, a backend restart
    // between WS flips could leave the UI showing providers that no longer
    // resolve. The grace timer bounds the flapping window.
    if (!isConnected || !supportsSettings) {
      // Cancel any pending reconnect timer and schedule the actual clear.
      if (disconnectTimerRef.current !== null) {
        clearTimeout(disconnectTimerRef.current);
        disconnectTimerRef.current = null;
      }
      disconnectTimerRef.current = setTimeout(() => {
        setProviders(EMPTY_PROVIDERS);
        setCurrentProvider('');
        setCurrentModel('');
      }, 1500);
      return;
    }

    // Connected: cancel any pending clear and kick off the fetch.
    if (disconnectTimerRef.current !== null) {
      clearTimeout(disconnectTimerRef.current);
      disconnectTimerRef.current = null;
    }

    let cancelled = false;
    setIsLoading(true);
    ApiService.getInstance()
      .getProviders()
      .then((data) => {
        if (cancelled) return;
        setProviders(data.providers ?? EMPTY_PROVIDERS);
        setCurrentProvider(data.current_provider ?? '');
        setCurrentModel(data.current_model ?? '');
      })
      .catch((err) => {
        if (cancelled) return;
        debugLog('[ProviderCatalog] failed to load providers:', err);
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
      // Clean up any pending disconnect timer if the effect unmounts.
      if (disconnectTimerRef.current !== null) {
        clearTimeout(disconnectTimerRef.current);
        disconnectTimerRef.current = null;
      }
    };
  }, [isConnected, refreshTick]);

  const getProviderName = useCallback(
    (id: string | undefined | null): string => {
      if (!id) return '';
      const entry = providers.find((p) => p.id === id);
      return entry?.name ?? id;
    },
    [providers],
  );

  const value = useMemo<ProviderCatalogContextValue>(
    () => ({
      providers,
      isLoading,
      currentProvider,
      currentModel,
      refresh,
      getProviderName,
      ensureLoaded,
    }),
    [providers, isLoading, currentProvider, currentModel, refresh, getProviderName, ensureLoaded],
  );

  return <ProviderCatalogContext.Provider value={value}>{children}</ProviderCatalogContext.Provider>;
}
