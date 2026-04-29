import { createContext, useContext, useMemo, type ReactNode } from 'react';
import type { APIAdapter } from '@/types/adapter';

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

interface SproutProviderProps {
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
