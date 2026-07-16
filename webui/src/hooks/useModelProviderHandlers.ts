/**
 * Model, provider, and view change handlers.
 *
 * Manages the callbacks for switching the active LLM model, provider,
 * and application view. Keeps a pending-provider ref so that a rapid
 * model-then-provider change always pairs the most recent provider
 * with the backend model_change event.
 */

import { useCallback, useEffect, useRef } from 'react';
import type { MutableRefObject } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';
import { useEvents } from '../contexts/EventsContext';
import type { AppState } from '../types/app';
import { debugLog } from '../utils/log';

export interface UseModelProviderHandlersOptions {
  state: AppState;
  setState: AppStoreSetState;
  /** Shared refs for tracking pending provider changes across hooks. */
  pendingProviderChangeRef?: MutableRefObject<boolean>;
  pendingProviderChangeValueRef?: MutableRefObject<string | null>;
}

export interface UseModelProviderHandlersReturn {
  handleModelChange: (model: string) => void;
  handleProviderChange: (provider: string) => void;
  handleViewChange: (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners' | 'dashboard' | 'workspaces') => void;
  handlePersonaChange: (persona: string) => void;
  /** Refs exposed for sharing with other hooks (e.g., WS event handler). */
  pendingProviderRef: MutableRefObject<string>;
}

export function useModelProviderHandlers({
  state,
  setState,
  pendingProviderChangeRef,
  pendingProviderChangeValueRef,
}: UseModelProviderHandlersOptions): UseModelProviderHandlersReturn {
  const events = useEvents();

  const pendingProviderRef = useRef<string>(state.provider);
  const providerRef = useRef(state.provider);
  providerRef.current = state.provider;

  useEffect(() => {
    pendingProviderRef.current = state.provider;
    providerRef.current = state.provider;
  }, [state.provider]);

  const handleModelChange = useCallback(
    (model: string) => {
      debugLog('Model changed to:', model);
      const provider = pendingProviderRef.current || providerRef.current;
      // Mirror the optimistic write into state.stats so ChatStatusBarItems
      // (which renders stats.provider/stats.model) updates at the same instant
      // as the settings-sidebar dropdown. Without this, the status bar stays
      // on the previous model until the backend's next metrics_update arrives.
      setState((prev) => ({
        model,
        stats: { ...prev.stats, model },
      }));
      events.sendEvent({
        type: 'model_change',
        data: { provider, model },
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [events],
  );

  const handleProviderChange = useCallback(
    (provider: string) => {
      debugLog('Provider changed to:', provider);
      pendingProviderRef.current = provider;
      if (pendingProviderChangeRef) {
        pendingProviderChangeRef.current = true;
      }
      if (pendingProviderChangeValueRef) {
        pendingProviderChangeValueRef.current = provider;
      }
      // See handleModelChange — same reason for mirroring into state.stats.
      setState((prev) => ({
        provider,
        stats: { ...prev.stats, provider },
      }));
      events.sendEvent({
        type: 'provider_change',
        data: { provider },
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [events],
  );

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners' | 'dashboard' | 'workspaces') => {
    setState((prev) => ({
      currentView: view,
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handlePersonaChange = useCallback(
    (persona: string) => {
      debugLog('Persona changed to:', persona);
      events.sendEvent({
        type: 'persona_change',
        data: { persona },
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [events],
  );

  return { handleModelChange, handleProviderChange, handleViewChange, handlePersonaChange, pendingProviderRef };
}
