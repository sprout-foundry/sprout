import { createContext, useContext, useMemo, type ReactNode, useCallback } from 'react';
import type { APIAdapter } from '../services/apiAdapter';
import { getAdapter } from '../services/apiAdapter';
import { clientFetch, resolveWebUIClientId, WEBUI_CLIENT_ID_HEADER } from '../services/clientSession';

interface SproutAdapterContextValue {
  adapter: APIAdapter | null;
}

const SproutAdapterContext = createContext<SproutAdapterContextValue | null>(null);

/**
 * Hook to access the installed Sprout adapter.
 * Throws an error if used outside of SproutAdapterProvider.
 */
export function useSproutAdapter(): APIAdapter | null {
  const context = useContext(SproutAdapterContext);
  if (!context) {
    throw new Error('useSproutAdapter must be used within SproutAdapterProvider');
  }
  return context.adapter;
}

/**
 * Hook to get a fetch function that routes through the adapter or falls back to clientFetch.
 *
 * This is the preferred way for components to make API calls. It:
 * - Delegates to the adapter's fetch() when an adapter is installed (cloud mode)
 * - Falls back to clientFetch() in local mode
 * - Always sets the X-Sprout-Client-ID header for request routing
 *
 * @returns A fetch function with the same signature as clientFetch
 */
export function useSproutFetch(): (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> {
  const adapter = useSproutAdapter();

  return useCallback(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      // Resolve the client ID asynchronously. In cloud mode (cross-origin),
      // getWebUIClientId() (synchronous) would generate a bogus UUID because
      // document.cookie is unreadable — that bogus ID poisons sessionStorage
      // and makes resolveWebUIClientId()'s fast-path skip server recovery.
      // Awaiting resolveWebUIClientId() ensures the real ID is used.
      const clientId = await resolveWebUIClientId();
      const headers = new Headers(init?.headers || {});
      headers.set(WEBUI_CLIENT_ID_HEADER, clientId);

      if (adapter) {
        // Cloud mode: route through adapter (adapter.fetch does NOT add client ID header)
        return adapter.fetch(input, { ...init, headers });
      }

      // Local mode: fall back to clientFetch.
      // Note: clientFetch will ALSO set the X-Sprout-Client-ID header — this is
      // intentional double-set. Headers.set overwrites with the same value, so
      // the header is set correctly once in the final request.
      return clientFetch(input, { ...init, headers });
    },
    [adapter],
  );
}

export interface SproutAdapterProviderProps {
  children: ReactNode;
}

/**
 * Provider component that makes the Sprout adapter available to all child components.
 *
 * The adapter is read from the singleton getAdapter() and memoized to prevent
 * unnecessary context updates. This provider bridges the singleton adapter pattern
 * used by the rest of the webui with the React context pattern expected by
 * @sprout/ui components.
 *
 * This provider should be placed at the top of the provider tree, outside
 * ThemeProvider, to ensure all components have access to the adapter.
 */
export function SproutAdapterProvider({ children }: SproutAdapterProviderProps): JSX.Element {
  // Read adapter from singleton - this is the source of truth
  const adapter = getAdapter();

  // Memoize context value to prevent unnecessary re-renders
  // The adapter reference is stable, so this only updates when getAdapter() returns a different value
  const value = useMemo(() => ({ adapter }), [adapter]);

  return <SproutAdapterContext.Provider value={value}>{children}</SproutAdapterContext.Provider>;
}

SproutAdapterProvider.displayName = 'SproutAdapterProvider';
