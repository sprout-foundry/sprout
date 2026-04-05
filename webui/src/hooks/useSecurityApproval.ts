import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { WebSocketService } from '../services/websocket';
import type { AppState } from '../types/app';

export interface UseSecurityApprovalReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean) => void;
}

export function useSecurityApproval(setState: Dispatch<SetStateAction<AppState>>): UseSecurityApprovalReturn {
  const handleSecurityApprovalResponse = useCallback(
    (requestId: string, approved: boolean) => {
      const wsService = WebSocketService.getInstance();
      if (!wsService.isConnected()) {
        // Keep the dialog open — the approval was not delivered.
        // The user can retry once the connection is restored.
        return;
      }
      wsService.sendEvent({
        type: 'security_approval_response',
        data: { request_id: requestId, approved },
      });
      // Only clear the dialog after successfully sending
      setState((prev) => ({ ...prev, securityApprovalRequest: null }));
    },
    [setState],
  );

  return { handleSecurityApprovalResponse };
}
