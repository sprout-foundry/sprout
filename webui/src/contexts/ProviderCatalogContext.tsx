import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
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
}

const EMPTY_PROVIDERS: ProviderOption[] = [];

const FALLBACK_VALUE: ProviderCatalogContextValue = {
  providers: EMPTY_PROVIDERS,
  isLoading: false,
  currentProvider: '',
  currentModel: '',
  refresh: () => {},
  // Without a catalog, return the raw id — consumers (e.g. status bar)
  // already treat that as a safe baseline rather than crashing.
  getProviderName: (id) => id ?? '',
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

  const refresh = useCallback(() => setRefreshTick((n) => n + 1), []);

  useEffect(() => {
    if (!isConnected || !supportsSettings) {
      setProviders(EMPTY_PROVIDERS);
      setCurrentProvider('');
      setCurrentModel('');
      return;
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
    }),
    [providers, isLoading, currentProvider, currentModel, refresh, getProviderName],
  );

  return <ProviderCatalogContext.Provider value={value}>{children}</ProviderCatalogContext.Provider>;
}
