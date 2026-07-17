import { useCallback } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { useEvents } from '../contexts/EventsContext';
import type { AppState } from '../types/app';

// The WebUI approval dialog supports several option layouts. The action
// names here must match the server-side ApprovalDecision wire format in
// pkg/security/approval_manager.go (ApprovalDecisionFromString).
//
// - approve_once / deny: every dialog mode
// - approve_always / elevate: shell-command 4-option mode (SP-058)
// - allow_folder_session: filesystem External-tier mode
//
// Backend rejects unknown values; keep this union in sync with the Go
// validator in pkg/webui/websocket_message_types.go.
export type SecurityApprovalAction =
  | 'approve_once'
  | 'approve_always'
  | 'always_ask'
  | 'elevate'
  | 'allow_folder_session'
  | 'deny';

export interface UseSecurityApprovalReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean, action?: SecurityApprovalAction) => void;
}

export function useSecurityApproval(setState: Dispatch<SetStateAction<AppState>>): UseSecurityApprovalReturn {
  const events = useEvents();

  const handleSecurityApprovalResponse = useCallback(
    (requestId: string, approved: boolean, action?: SecurityApprovalAction) => {
      if (!events.isConnected()) {
        // Keep the dialog open — the approval was not delivered.
        // The user can retry once the connection is restored.
        return;
      }
      // Legacy bool stays for old call sites (Allow / Block on the classic
      // 2-button dialog). 4-option callers pass action for the typed
      // outcome; server falls back to bool when action is empty.
      events.sendEvent({
        type: 'security_approval_response',
        data: { request_id: requestId, approved, ...(action ? { action } : {}) },
      });
      // Only clear the dialog after successfully sending
      setState((prev) => ({ ...prev, securityApprovalRequest: null }));
    },
    [events, setState],
  );

  return { handleSecurityApprovalResponse };
}
