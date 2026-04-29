import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { useEvents } from '../contexts/EventsContext';
import type { AppState } from '../types/app';

export interface UseSecurityPromptReturn {
  handleSecurityPromptResponse: (requestId: string, response: boolean) => void;
}

export function useSecurityPrompt(setState: Dispatch<SetStateAction<AppState>>): UseSecurityPromptReturn {
  const events = useEvents();

  const handleSecurityPromptResponse = useCallback(
    (requestId: string, response: boolean) => {
      if (!events.isConnected()) {
        // Keep the dialog open — the response was not delivered.
        // The user can retry once the connection is restored.
        return;
      }
      events.sendEvent({
        type: 'security_prompt_response',
        data: { request_id: requestId, response },
      });
      // Only clear the dialog after successfully sending
      setState((prev) => ({ ...prev, securityPromptRequest: null }));
    },
    [events, setState],
  );

  return { handleSecurityPromptResponse };
}
