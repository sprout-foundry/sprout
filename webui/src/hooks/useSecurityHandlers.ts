/**
 * Security approval, prompt, askUser, and modelSelection handlers.
 *
 * These handlers use the eventsProvider prop directly rather than the
 * useEvents() hook, keeping the dependency explicit and testable.
 */

import type { EventsProvider } from '@sprout/events';
import { useCallback } from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';

export interface UseSecurityHandlersOptions {
  eventsProvider: EventsProvider;
  provider: string;
  setState: AppStoreSetState;
}

export interface UseSecurityHandlersReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean) => void;
  handleSecurityPromptResponse: (requestId: string, response: boolean) => void;
  handleAskUserResponse: (requestId: string, response: string) => void;
  handlePasswordResponse: (requestId: string, password: string) => void;
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
      setState((_prev) => ({ securityApprovalRequest: null }));
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
      setState((_prev) => ({ securityPromptRequest: null }));
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
      setState((_prev) => ({ askUserRequest: null }));
    },
    [eventsProvider, setState],
  );

  // handlePasswordResponse delivers the user's typed password back to the
  // agent's broker. Empty password = cancel (shell sees EOF on stdin).
  // CRITICAL: this hook never logs the password value — the dialog's
  // state lives in React (not the store) so nothing in the persistence
  // path captures it either.
  const handlePasswordResponse = useCallback(
    (requestId: string, password: string) => {
      if (!eventsProvider.isConnected()) return;
      eventsProvider.sendEvent({
        type: 'password_response',
        data: { request_id: requestId, password },
      });
      setState((_prev) => ({ passwordRequest: null }));
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
      setState((_prev) => ({ modelSelectionRequest: null }));
    },
    [eventsProvider, provider, setState],
  );

  const handleModelSelectionClose = useCallback(() => {
    setState((_prev) => ({ modelSelectionRequest: null }));
  }, [setState]);

  return {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
    handleAskUserResponse,
    handlePasswordResponse,
    handleModelSelectionResponse,
    handleModelSelectionClose,
  };
}
