import { AlertTriangle, Loader2 } from 'lucide-react';
import { useCallback, useState } from 'react';
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
 *
 * The overlay sits above the app's toast layer, so every failure surfaces
 * as an inline error here — a toast would be invisible behind the scrim.
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

  // The select/consent handlers in AppContent fire-and-forget promises.
  // They throw on failure (daemon stall, 409 query_in_progress, terminal
  // teardown error), and before this state existed those rejections were
  // unhandled: the button appeared dead and the modal never explained why.
  const [pending, setPending] = useState<'select' | 'consent' | null>(null);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    (kind: 'select' | 'consent', action: () => void) => {
      if (pending) return;
      setError(null);
      setPending(kind);
      try {
        // The callers are async but not awaited here on purpose: a resolved
        // setWorkspace triggers a page reload, so post-success cleanup is moot.
        // Only the failure path matters, and it is captured below.
        Promise.resolve(action()).catch((err: unknown) => {
          const message = err instanceof Error ? err.message : String(err);
          setError(message || 'Failed to update workspace');
          setPending(null);
        });
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setPending(null);
      }
    },
    [pending],
  );

  // Cloud mode (and any mode without workspace switching) is never gated.
  if (!supportsWorkspaceSwitching) return null;

  return (
    <div className="workspace-gate-overlay" data-testid="workspace-gate-modal">
      <div className={`workspace-gate-content${pending ? ' is-busy' : ''}`}>
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
            onSelect={(path) => run('select', () => onSelectWorkspace(path))}
            onCancel={() => setBrowsing(false)}
          />
        ) : (
          <WorkspacePicker
            daemonRoot={workspaceInfo.daemon_root}
            currentWorkspace={workspaceInfo.workspace_root}
            suggestedProjects={workspaceInfo.suggested_projects}
            recentWorkspaces={workspaceInfo.recent_workspaces}
            onSelect={(path) => run('select', () => onSelectWorkspace(path))}
            onBrowse={() => setBrowsing(true)}
          />
        )}

        {error && (
          <div className="workspace-gate-error" role="alert" data-testid="workspace-gate-error">
            {error}
          </div>
        )}

        <div className="workspace-gate-home-consent">
          <button
            className="workspace-gate-home-btn"
            type="button"
            onClick={() => run('consent', onConsentHome)}
            disabled={pending !== null}
            data-testid="workspace-gate-home-btn"
          >
            {pending === 'consent' ? (
              <>
                <Loader2 size={14} className="workspace-gate-btn-spin" aria-hidden="true" />
                Dismissing&hellip;
              </>
            ) : (
              'Use my home directory anyway'
            )}
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
