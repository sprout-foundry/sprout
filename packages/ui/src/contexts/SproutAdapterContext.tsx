import { createContext, useContext, useMemo, useCallback, type ReactNode } from 'react';
import type { APIAdapter } from '../types/adapter';

interface SproutAdapterContextValue {
  adapter: APIAdapter | null;
}

const SproutAdapterContext = createContext<SproutAdapterContextValue | null>(null);

/**
 * Hook to access the installed Sprout adapter.
 * Throws an error if used outside of SproutProvider.
 */
export function useSproutAdapter(): APIAdapter | null {
  const context = useContext(SproutAdapterContext);
  if (!context) {
    throw new Error('useSproutAdapter must be used within SproutProvider');
  }
  return context.adapter;
}

/**
 * Hook to get a fetch function that routes through the adapter.
 *
 * This is the preferred way for components to make API calls when using
 * the @sprout/ui package. It delegates to the adapter's fetch() when
 * an adapter is installed.
 *
 * Note: This hook does NOT add any client ID headers - the adapter's
 * fetch implementation is responsible for any necessary headers.
 *
 * Consumers who need fallback behavior when no adapter is installed
 * should provide their own wrapper (e.g., webui's useSproutFetch).
 *
 * @returns A fetch function with the same signature as the global fetch
 */
export function useSproutFetch(): (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> {
  const adapter = useSproutAdapter();

  return useCallback(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      if (!adapter) {
        throw new Error('useSproutFetch requires an adapter to be installed. ' +
          'Use SproutProvider with an adapter, or wrap with a custom provider that provides fallback behavior.');
      }
      return adapter.fetch(input, init);
    },
    [adapter]
  );
}

export interface SproutProviderProps {
  adapter: APIAdapter | null;
  children: ReactNode;
}

/**
 * Provider component that makes a Sprout adapter available to all child components.
 * The adapter is installed once at app startup and does not change.
 */
export function SproutProvider({ adapter, children }: SproutProviderProps): JSX.Element {
  const value = useMemo(() => ({ adapter }), [adapter]);

  return <SproutAdapterContext.Provider value={value}>{children}</SproutAdapterContext.Provider>;
}

SproutProvider.displayName = 'SproutProvider';
