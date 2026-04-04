/**
 * Model, provider, and view change handlers.
 *
 * Manages the callbacks for switching the active LLM model, provider,
 * and application view. Keeps a pending-provider ref so that a rapid
 * model-then-provider change always pairs the most recent provider
 * with the backend model_change event.
 */

import { useCallback, useEffect, useRef } from 'react';
import { WebSocketService } from '../services/websocket';
import { debugLog } from '../utils/log';
import type { AppState } from '../types/app';

export interface UseModelProviderHandlersOptions {
  state: AppState;
  setState: React.Dispatch<React.SetStateAction<AppState>>;
}

export interface UseModelProviderHandlersReturn {
  handleModelChange: (model: string) => void;
  handleProviderChange: (provider: string) => void;
  handleViewChange: (view: 'chat' | 'editor' | 'git') => void;
}

export function useModelProviderHandlers({
  state,
  setState,
}: UseModelProviderHandlersOptions): UseModelProviderHandlersReturn {
  const wsService = WebSocketService.getInstance();

  const pendingProviderRef = useRef<string>(state.provider);
  const providerRef = useRef(state.provider);
  providerRef.current = state.provider;

  useEffect(() => {
    pendingProviderRef.current = state.provider;
    providerRef.current = state.provider;
  }, [state.provider]);

  const handleModelChange = useCallback((model: string) => {
    debugLog('Model changed to:', model);
    const provider = pendingProviderRef.current || providerRef.current;
    setState(prev => ({
      ...prev,
      model,
    }));
    wsService.sendEvent({
      type: 'model_change',
      data: { provider, model },
    });
  }, [wsService]);

  const handleProviderChange = useCallback((provider: string) => {
    debugLog('Provider changed to:', provider);
    pendingProviderRef.current = provider;
    setState(prev => ({
      ...prev,
      provider,
    }));
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider },
    });
  }, [wsService]);

  const handleViewChange = useCallback((view: 'chat' | 'editor' | 'git') => {
    setState(prev => ({
      ...prev,
      currentView: view,
    }));
  }, []);

  return { handleModelChange, handleProviderChange, handleViewChange };
}
