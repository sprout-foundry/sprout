/**
 * Security approval and prompt handlers.
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
  setState: Dispatch<SetStateAction<AppState>>;
}

export interface UseSecurityHandlersReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean) => void;
  handleSecurityPromptResponse: (requestId: string, response: boolean) => void;
}

export function useSecurityHandlers({
  eventsProvider,
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

  return {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
  };
}
