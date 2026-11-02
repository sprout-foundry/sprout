/**
 * The agent's blocking prompts as a workspace unit.
 *
 * While a turn runs, the agent can stop and wait for the user: a tool call
 * that needs approval (including the shell-command elevation choices and the
 * filesystem-tier "allow this folder" choice), a file-access prompt, an
 * `ask_user` question, a password for a command, a per-hunk edit approval or
 * a per-part shell approval. The chat unit's event reducer records each
 * request in the chat state; this unit renders the dialog for it and sends
 * the answer back, so the agent resumes. It also renders the model picker the
 * chat opens when a provider switch needs a model.
 *
 * Anything that mounts the chat unit must mount this with it, or a request
 * leaves the agent waiting with nothing on screen: Sprout's app root and an
 * own-mode `SproutWorkspace` both do. A host that composes its own stack
 * (`WorkspaceChatProvider` by hand) mounts it inside that provider.
 *
 * The dialogs answer through the events transport the provider stack carries
 * (`useEvents()`), and the edit and shell approvals through their REST
 * decision endpoints on the active backend. The dialogs are fixed overlays,
 * so they open over the workspace wherever it is mounted, and a request from
 * a chat other than the one on screen is shown too, named by the origin
 * notice.
 */

import { useEvents } from '@sprout/events';
import { useCallback } from 'react';
import { ApprovalOriginNotice } from '../components/ApprovalOriginNotice';
import AskUserDialog from '../components/AskUserDialog';
import EditApprovalPanel from '../components/EditApprovalPanel';
import ModelSelectionModal from '../components/ModelSelectionModal';
import PasswordPromptDialog from '../components/PasswordPromptDialog';
import SecurityApprovalDialog from '../components/SecurityApprovalDialog';
import SecurityPromptDialog from '../components/SecurityPromptDialog';
import ShellApprovalPanel from '../components/ShellApprovalPanel';
import { useSecurityHandlers } from '../hooks/useSecurityHandlers';
import { clientFetch } from '../services/clientSession';
import { useWorkspaceChat } from './WorkspaceChatContext';

/**
 * Render the agent's pending prompts from the chat unit's state. Renders
 * nothing while no request is pending. Must be mounted inside
 * `WorkspaceChatProvider` and the provider stack (`SproutProviders`).
 */
export function WorkspaceAgentPrompts(): JSX.Element {
  const events = useEvents();
  const { state, setState } = useWorkspaceChat();

  const {
    handleSecurityApprovalResponse,
    handleSecurityPromptResponse,
    handleAskUserResponse,
    handlePasswordResponse,
    handleModelSelectionResponse,
    handleModelSelectionClose,
  } = useSecurityHandlers({ eventsProvider: events, provider: state.provider, setState });

  // The edit approval panel POSTs its own decision; it calls back once the
  // backend has it, and the request is cleared then.
  const handleEditApprovalResolved = useCallback(() => {
    setState(() => ({ editApprovalRequest: null }));
  }, [setState]);

  // Per-part shell approval decisions go to the backend, which hands them to
  // the agent waiting on the shell approval broker. A failed delivery throws
  // into the panel's error state, so the dialog stays open for a retry.
  const handleShellApprovalSubmit = useCallback(
    async (requestId: string, decisions: Record<string, boolean>) => {
      const resp = await clientFetch(`/api/shell-approvals/${encodeURIComponent(requestId)}/decision`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: requestId, decisions }),
      });
      if (!resp.ok) {
        const body = await resp.text();
        throw new Error(`Decision POST failed: ${body || `HTTP ${resp.status}`}`);
      }
      const result = await resp.json();
      if (!result.delivered) {
        throw new Error('Decision not delivered by server (expired or unknown request)');
      }
      setState(() => ({ shellApprovalRequest: null }));
    },
    [setState],
  );

  const {
    securityApprovalRequest,
    securityPromptRequest,
    askUserRequest,
    passwordRequest,
    editApprovalRequest,
    shellApprovalRequest,
    modelSelectionRequest,
  } = state;

  return (
    <>
      <ApprovalOriginNotice
        chatName={
          securityApprovalRequest?.fromChat ??
          securityPromptRequest?.fromChat ??
          askUserRequest?.fromChat ??
          editApprovalRequest?.fromChat ??
          shellApprovalRequest?.fromChat
        }
      />
      {securityApprovalRequest && (
        <SecurityApprovalDialog
          requestId={securityApprovalRequest.requestId}
          toolName={securityApprovalRequest.toolName}
          riskLevel={securityApprovalRequest.riskLevel as 'SAFE' | 'CAUTION' | 'DANGEROUS'}
          reasoning={securityApprovalRequest.reasoning}
          command={securityApprovalRequest.command}
          riskType={securityApprovalRequest.riskType}
          target={securityApprovalRequest.target}
          allowOptions={securityApprovalRequest.allowOptions}
          fsKind={securityApprovalRequest.fsKind}
          fsFolder={securityApprovalRequest.fsFolder}
          fsPath={securityApprovalRequest.fsPath}
          securityAnalysis={securityApprovalRequest.securityAnalysis}
          deliveryError={securityApprovalRequest.deliveryError}
          onRespond={handleSecurityApprovalResponse}
        />
      )}
      {securityPromptRequest && (
        <SecurityPromptDialog
          requestId={securityPromptRequest.requestId}
          prompt={securityPromptRequest.prompt}
          filePath={securityPromptRequest.filePath}
          concern={securityPromptRequest.concern}
          onRespond={handleSecurityPromptResponse}
        />
      )}
      {askUserRequest && (
        <AskUserDialog
          requestId={askUserRequest.requestId}
          question={askUserRequest.question}
          header={askUserRequest.header}
          options={askUserRequest.options}
          multiSelect={askUserRequest.multiSelect}
          defaultValue={askUserRequest.default}
          sensitive={askUserRequest.sensitive}
          credentialKey={askUserRequest.credentialKey}
          deliveryError={askUserRequest.deliveryError}
          onRespond={handleAskUserResponse}
        />
      )}
      {passwordRequest && (
        <PasswordPromptDialog
          key={passwordRequest.requestId}
          requestId={passwordRequest.requestId}
          command={passwordRequest.command}
          prompt={passwordRequest.prompt}
          secret={passwordRequest.secret}
          onRespond={handlePasswordResponse}
        />
      )}
      {editApprovalRequest && (
        <EditApprovalPanel
          requestId={editApprovalRequest.requestId}
          filePath={editApprovalRequest.filePath}
          unifiedDiff={editApprovalRequest.unifiedDiff}
          hunks={editApprovalRequest.hunks}
          onRespond={handleEditApprovalResolved}
        />
      )}
      {shellApprovalRequest && (
        <ShellApprovalPanel
          request={{
            request_id: shellApprovalRequest.requestId,
            command: shellApprovalRequest.command,
            parts: shellApprovalRequest.parts,
            unified_view: shellApprovalRequest.unifiedView,
            risk_level: shellApprovalRequest.riskLevel,
            security_analysis: shellApprovalRequest.securityAnalysis
              ? {
                  summary: shellApprovalRequest.securityAnalysis.summary,
                  modifies: shellApprovalRequest.securityAnalysis.modifies,
                  risk_assessment: shellApprovalRequest.securityAnalysis.riskAssessment,
                  recommendation: shellApprovalRequest.securityAnalysis.recommendation,
                }
              : undefined,
          }}
          onSubmit={async (decisions) => {
            await handleShellApprovalSubmit(shellApprovalRequest.requestId, decisions);
          }}
        />
      )}
      {modelSelectionRequest && (
        <ModelSelectionModal
          provider={modelSelectionRequest.provider}
          reason={modelSelectionRequest.reason}
          onClose={handleModelSelectionClose}
          onSelectModel={handleModelSelectionResponse}
        />
      )}
    </>
  );
}

export default WorkspaceAgentPrompts;
