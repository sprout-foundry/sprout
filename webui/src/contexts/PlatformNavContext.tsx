import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import type { APIAdapter, PlatformNavItem } from '../services/apiAdapter';
import { getAdapter, ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';

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
  // The adapter is installed asynchronously after the bootstrap fetch resolves,
  // so we start with the current value (which may be null) and listen for the
  // ADAPTER_INSTALLED_EVENT to update when the adapter becomes available.
  const [adapter, setAdapter] = useState<APIAdapter | null>(() => getAdapter());

  useEffect(() => {
    const handler = () => {
      setAdapter(getAdapter());
    };
    window.addEventListener(ADAPTER_INSTALLED_EVENT, handler);
    return () => {
      window.removeEventListener(ADAPTER_INSTALLED_EVENT, handler);
    };
  }, []);

  const value = useMemo<PlatformNavContextValue>(
    () => ({ platformNavItems: adapter?.platformNavItems ?? EMPTY_NAV_ITEMS }),
    [adapter],
  );

  return <PlatformNavContext.Provider value={value}>{children}</PlatformNavContext.Provider>;
}

export { PlatformNavContext };
