import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { useEvents } from '../contexts/EventsContext';
import type { AppState } from '../types/app';

export interface UseSecurityApprovalReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean) => void;
}

export function useSecurityApproval(setState: Dispatch<SetStateAction<AppState>>): UseSecurityApprovalReturn {
  const events = useEvents();

  const handleSecurityApprovalResponse = useCallback(
    (requestId: string, approved: boolean) => {
      if (!events.isConnected()) {
        // Keep the dialog open — the approval was not delivered.
        // The user can retry once the connection is restored.
        return;
      }
      events.sendEvent({
        type: 'security_approval_response',
        data: { request_id: requestId, approved },
      });
      // Only clear the dialog after successfully sending
      setState((prev) => ({ ...prev, securityApprovalRequest: null }));
    },
    [events, setState],
  );

  return { handleSecurityApprovalResponse };
}
