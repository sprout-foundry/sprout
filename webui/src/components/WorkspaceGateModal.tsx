import { AlertTriangle } from 'lucide-react';
import { supportsWorkspaceSwitching } from '../config/mode';
import type { WorkspaceInfo } from '../hooks/useWorkspace';
import WorkspacePicker from './WorkspacePicker';
import './WorkspaceGateModal.css';

/**
 * WorkspaceGateModal — a full-screen blocking overlay (SP-130) shown when
 * the resolved workspace root is the user's home directory without consent.
 *
 * Unlike WelcomeTab's inline WorkspacePicker (shown for a generic
 * non-project directory), this modal *blocks* the editor/chat/files until
 * the user either selects a project folder or explicitly consents to the
 * home directory.
 *
 * Renders only in local mode — cloud mode has a single virtual FS, so
 * `supportsWorkspaceSwitching` (false in cloud) short-circuits to null.
 */
interface WorkspaceGateModalProps {
  workspaceInfo: WorkspaceInfo;
  onSelectWorkspace: (path: string) => void;
  onConsentHome: () => void;
  onBrowse: () => void;
}

function WorkspaceGateModal({
  workspaceInfo,
  onSelectWorkspace,
  onConsentHome,
  onBrowse,
}: WorkspaceGateModalProps): JSX.Element | null {
  // Cloud mode (and any mode without workspace switching) is never gated.
  if (!supportsWorkspaceSwitching) return null;

  return (
    <div className="workspace-gate-overlay" data-testid="workspace-gate-modal">
      <div className="workspace-gate-content">
        <div className="workspace-gate-header">
          <div className="workspace-gate-header-icon">
            <AlertTriangle size={28} />
          </div>
          <div>
            <h2 className="workspace-gate-title">Select a workspace</h2>
            <p className="workspace-gate-subtitle">
              Sprout is running in your home directory, which gives the agent access to all your files.
              Select a project folder to limit its scope.
            </p>
          </div>
        </div>

        <WorkspacePicker
          daemonRoot={workspaceInfo.daemon_root}
          currentWorkspace={workspaceInfo.workspace_root}
          suggestedProjects={workspaceInfo.suggested_projects}
          recentWorkspaces={workspaceInfo.recent_workspaces}
          onSelect={onSelectWorkspace}
          onBrowse={onBrowse}
        />

        <div className="workspace-gate-home-consent">
          <button className="workspace-gate-home-btn" type="button" onClick={onConsentHome}>
            Use my home directory anyway
          </button>
          <p className="workspace-gate-home-warning">
            Running in your home directory gives the agent unrestricted access to all files and may
            trigger macOS permission prompts for protected folders like Music and Photos.
          </p>
        </div>
      </div>
    </div>
  );
}

export default WorkspaceGateModal;
