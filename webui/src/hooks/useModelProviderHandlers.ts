/**
 * Model, provider, and view change handlers.
 *
 * Manages the callbacks for switching the active LLM model, provider,
 * and application view. Keeps a pending-provider ref so that a rapid
 * model-then-provider change always pairs the most recent provider
 * with the backend model_change event.
 */

import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { debugLog } from '../utils/log';
import { useEvents } from '../contexts/EventsContext';
import type { AppState } from '../types/app';

export interface UseModelProviderHandlersOptions {
  state: AppState;
  setState: Dispatch<SetStateAction<AppState>>;
}

export interface UseModelProviderHandlersReturn {
  handleModelChange: (model: string) => void;
  handleProviderChange: (provider: string) => void;
  handleViewChange: (view: 'chat' | 'editor' | 'git') => void;
  handlePersonaChange: (persona: string) => void;
}

export function useModelProviderHandlers({
  state,
  setState,
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
      setState((prev) => ({
        ...prev,
        model,
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
      setState((prev) => ({
        ...prev,
        provider,
      }));
      events.sendEvent({
        type: 'provider_change',
        data: { provider },
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [events],
  );

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git') => {
    setState((prev) => ({
      ...prev,
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

  return { handleModelChange, handleProviderChange, handleViewChange, handlePersonaChange };
}
