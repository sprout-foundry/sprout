/**
 * Security approval, prompt, askUser, and modelSelection handlers.
 *
 * These handlers use the eventsProvider prop directly rather than the
 * useEvents() hook, keeping the dependency explicit and testable.
 */

import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import type { LocalEventsProvider } from '../services/localEventsProvider';
import type { AppState } from '../types/app';

export interface UseSecurityHandlersOptions {
  eventsProvider: LocalEventsProvider;
  provider: string;
  setState: Dispatch<SetStateAction<AppState>>;
}

export interface UseSecurityHandlersReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean) => void;
  handleSecurityPromptResponse: (requestId: string, response: boolean) => void;
  handleAskUserResponse: (requestId: string, response: string) => void;
  handleModelSelectionResponse: (model: string) => void;
  handleModelSelectionClose: () => void;
}

export function useSecurityHandlers({
  eventsProvider,
  provider,
  setState,
}: UseSecurityHandlersOptions): UseSecurityHandlersReturn {
  const handleSecurityApprovalResponse = useCallback(
    (requestId: string, approved: boolean) => {
      if (!eventsProvider.isConnected()) return;
      eventsProvider.sendEvent({
        type: 'security_approval_response',
        data: { request_id: requestId, approved },
      });
      setState((prev) => ({ ...prev, securityApprovalRequest: null }));
    },
    [eventsProvider, setState],
  );

  const handleSecurityPromptResponse = useCallback(
    (requestId: string, response: boolean) => {
      if (!eventsProvider.isConnected()) return;
      eventsProvider.sendEvent({
        type: 'security_prompt_response',
        data: { request_id: requestId, response },
      });
      setState((prev) => ({ ...prev, securityPromptRequest: null }));
    },
    [eventsProvider, setState],
  );

  const handleAskUserResponse = useCallback(
    (requestId: string, response: string) => {
      if (!eventsProvider.isConnected()) return;
      eventsProvider.sendEvent({
        type: 'ask_user_response',
        data: { request_id: requestId, response },
      });
      setState((prev) => ({ ...prev, askUserRequest: null }));
    },
    [eventsProvider, setState],
  );

  const handleModelSelectionResponse = useCallback(
    (model: string) => {
      if (!eventsProvider.isConnected()) return;
      eventsProvider.sendEvent({
        type: 'model_change',
        data: { provider, model },
      });
      setState((prev) => ({ ...prev, modelSelectionRequest: null }));
    },
    [eventsProvider, provider, setState],
  );

  const handleModelSelectionClose = useCallback(() => {
    setState((prev) => ({ ...prev, modelSelectionRequest: null }));
  }, [setState]);

  return {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
    handleAskUserResponse,
    handleModelSelectionResponse,
    handleModelSelectionClose,
  };
}
