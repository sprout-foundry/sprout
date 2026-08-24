/**
 * Security approval, prompt, askUser, and modelSelection handlers.
 *
 * These handlers use the eventsProvider prop directly rather than the
 * useEvents() hook, keeping the dependency explicit and testable.
 */

import type { EventsProvider } from '@sprout/events';
import { useCallback } from 'react';
import { isCloud } from '../config/mode';
import type { AppStoreSetState } from '../contexts/AppStore';
import { clientFetch } from '../services/clientSession';

// The action names must match the server-side ApprovalDecisionFromString in
// pkg/security/approval_manager.go.
export type SecurityApprovalAction =
  | 'approve_once'
  | 'approve_always'
  | 'always_ask'
  | 'elevate'
  | 'allow_folder_session'
  | 'deny';

export interface UseSecurityHandlersOptions {
  eventsProvider: EventsProvider;
  provider: string;
  setState: AppStoreSetState;
}

export interface UseSecurityHandlersReturn {
  handleSecurityApprovalResponse: (requestId: string, approved: boolean, action?: SecurityApprovalAction) => void;
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
    (requestId: string, approved: boolean, action?: SecurityApprovalAction) => {
      if (!eventsProvider.isConnected()) {
        // Never silently swallow a user decision. A silent return here
        // leaves the server-side approval pending for the full timeout
        // (DefaultTimeout = 30m) with the dialog open and zero feedback —
        // the "approve hangs" symptom. Surface a visible error and keep
        // the dialog open so the user can retry once the socket reconnects.
        setState((prev) =>
          prev.securityApprovalRequest?.requestId === requestId
            ? {
                securityApprovalRequest: {
                  ...prev.securityApprovalRequest,
                  deliveryError:
                    'Connection lost — approval not delivered. Please retry once the connection is restored.',
                },
              }
            : prev,
        );
        return;
      }
      eventsProvider.sendEvent({
        type: 'security_approval_response',
        data: { request_id: requestId, approved, ...(action ? { action } : {}) },
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
      // Cloud mode: the agent loop runs in the WASM binary, so the
      // ask_user response must be delivered to the in-process
      // AskUserManager via the wasm-local endpoint (no backend is
      // listening for the WebSocket event in cloud mode).
      if (isCloud) {
        clientFetch('/api/ask-user/response', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ request_id: requestId, response }),
        })
          .then(async (res) => {
            if (!res.ok) {
              const text = await res.text().catch(() => '');
              setState((prev) =>
                prev.askUserRequest?.requestId === requestId
                  ? {
                      askUserRequest: {
                        ...prev.askUserRequest,
                        deliveryError: `Failed to deliver response (HTTP ${res.status}).`,
                      },
                    }
                  : prev,
              );
              console.warn('[ask_user] Failed to deliver response to WASM agent:', text);
              return;
            }
            const body = await res.json().catch(() => null);
            if (body && body.delivered === true) {
              setState((_prev) => ({ askUserRequest: null }));
            } else {
              setState((prev) =>
                prev.askUserRequest?.requestId === requestId
                  ? {
                      askUserRequest: {
                        ...prev.askUserRequest,
                        deliveryError: 'Ask user request not found or already expired.',
                      },
                    }
                  : prev,
              );
              console.warn('[ask_user] Response not delivered (unknown/expired request ID)');
            }
          })
          .catch((err) => {
            setState((prev) =>
              prev.askUserRequest?.requestId === requestId
                ? {
                    askUserRequest: {
                      ...prev.askUserRequest,
                      deliveryError: `Failed to deliver response: ${err instanceof Error ? err.message : String(err)}`,
                    },
                  }
                : prev,
            );
            console.warn('[ask_user] Failed to deliver response to WASM agent:', err);
          });
        return;
      }
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
