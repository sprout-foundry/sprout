import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { WebSocketService } from '../services/websocket';
import type { AppState } from '../types/app';

export interface UseSecurityPromptReturn {
  handleSecurityPromptResponse: (requestId: string, response: boolean) => void;
}

export function useSecurityPrompt(setState: Dispatch<SetStateAction<AppState>>): UseSecurityPromptReturn {
  const handleSecurityPromptResponse = useCallback(
    (requestId: string, response: boolean) => {
      const wsService = WebSocketService.getInstance();
      if (!wsService.isConnected()) {
        // Keep the dialog open — the response was not delivered.
        // The user can retry once the connection is restored.
        return;
      }
      wsService.sendEvent({
        type: 'security_prompt_response',
        data: { request_id: requestId, response },
      });
      // Only clear the dialog after successfully sending
      setState((prev) => ({ ...prev, securityPromptRequest: null }));
    },
    [setState],
  );

  return { handleSecurityPromptResponse };
}
