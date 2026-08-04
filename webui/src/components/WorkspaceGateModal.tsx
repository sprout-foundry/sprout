import { AlertTriangle } from 'lucide-react';
import { useState } from 'react';
import { supportsWorkspaceSwitching } from '../config/mode';
import type { WorkspaceInfo } from '../hooks/useWorkspace';
import WorkspaceBrowser from './WorkspaceBrowser';
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
}

function WorkspaceGateModal({
  workspaceInfo,
  onSelectWorkspace,
  onConsentHome,
}: WorkspaceGateModalProps): JSX.Element | null {
  // Browsing happens inside the modal. Delegating to the chrome's location
  // switcher (as this used to) opened that popover *behind* the gate — it sits
  // far below this overlay in the stacking order, anchored to a trigger the
  // scrim has already covered.
  const [browsing, setBrowsing] = useState(false);

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
              Sprout is running in your home directory, which gives the agent access to all your files. Select a project
              folder to limit its scope.
            </p>
          </div>
        </div>

        {browsing ? (
          <WorkspaceBrowser
            initialPath={workspaceInfo.workspace_root || workspaceInfo.daemon_root}
            onSelect={onSelectWorkspace}
            onCancel={() => setBrowsing(false)}
          />
        ) : (
          <WorkspacePicker
            daemonRoot={workspaceInfo.daemon_root}
            currentWorkspace={workspaceInfo.workspace_root}
            suggestedProjects={workspaceInfo.suggested_projects}
            recentWorkspaces={workspaceInfo.recent_workspaces}
            onSelect={onSelectWorkspace}
            onBrowse={() => setBrowsing(true)}
          />
        )}

        <div className="workspace-gate-home-consent">
          <button className="workspace-gate-home-btn" type="button" onClick={onConsentHome}>
            Use my home directory anyway
          </button>
          <p className="workspace-gate-home-warning">
            Running in your home directory gives the agent unrestricted access to all files and may trigger macOS
            permission prompts for protected folders like Music and Photos.
          </p>
        </div>
      </div>
    </div>
  );
}

export default WorkspaceGateModal;
