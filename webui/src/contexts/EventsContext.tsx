import { createContext, useContext, useMemo, type ReactNode } from 'react';
import type { EventsProvider } from '../types/events';

/**
 * React context for the events transport.
 * Wrap your component tree with `<EventsContextProvider>` and use
 * `useEvents()` in descendant components to access the transport.
 *
 * NOTE: This context is duplicated in
 * packages/ui/src/contexts/EventsContext.tsx for the @sprout/ui component library.
 * Changes here MUST be mirrored there until the context is extracted to a shared package.
 */

interface EventsContextValue {
  provider: EventsProvider;
}

const EventsContext = createContext<EventsContextValue | null>(null);

/**
 * Hook that returns the installed EventsProvider.
 * Components use this to subscribe to events, send events, and check
 * connection state without depending on a specific transport (WebSocket,
 * SSE, etc.).
 *
 * Must be used within an `<EventsContextProvider>`.
 */
export function useEvents(): EventsProvider {
  const ctx = useContext(EventsContext);
  if (!ctx) {
    throw new Error('useEvents() must be used within an EventsContextProvider');
  }
  return ctx.provider;
}

export interface EventsContextProviderProps {
  provider: EventsProvider;
  children: ReactNode;
}

/**
 * Provider component that makes an EventsProvider available to the
 * component tree. The provider reference is stable — no re-renders
 * are triggered after mount.
 */
export function EventsContextProvider({ provider, children }: EventsContextProviderProps): JSX.Element {
  const value = useMemo(() => ({ provider }), [provider]);

  return (
    <EventsContext.Provider value={value}>
      {children}
    </EventsContext.Provider>
  );
}

EventsContextProvider.displayName = 'EventsContextProvider';
