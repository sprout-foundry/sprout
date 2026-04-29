import { createContext, useContext, useMemo, type ReactNode } from 'react';
import type { PlatformNavItem } from '../services/apiAdapter';
import { getAdapter } from '../services/apiAdapter';

interface PlatformNavContextValue {
  platformNavItems: readonly PlatformNavItem[];
}

const EMPTY_NAV_ITEMS: readonly PlatformNavItem[] = [];

const PlatformNavContext = createContext<PlatformNavContextValue | null>(null);

export const usePlatformNav = (): PlatformNavContextValue => {
  const context = useContext(PlatformNavContext);
  if (!context) {
    throw new Error('usePlatformNav must be used within PlatformNavProvider');
  }
  return context;
};

interface PlatformNavProviderProps {
  children: ReactNode;
}

export function PlatformNavProvider({ children }: PlatformNavProviderProps): JSX.Element {
  // The adapter is installed once at startup and never changes.
  // useMemo with empty deps ensures the value object is stable across re-renders,
  // preventing unnecessary re-renders of all consumers.
  const value = useMemo<PlatformNavContextValue>(() => {
    const adapter = getAdapter();
    return {
      platformNavItems: adapter?.platformNavItems ?? EMPTY_NAV_ITEMS,
    };
  }, []);

  return <PlatformNavContext.Provider value={value}>{children}</PlatformNavContext.Provider>;
}

export { PlatformNavContext };
