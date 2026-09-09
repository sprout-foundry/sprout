import { AlertTriangle, FolderPlus, Loader2 } from 'lucide-react';
import { useCallback, useState } from 'react';
import { supportsFolderPicker, supportsWorkspaceSwitching } from '../config/mode';
import type { WorkspaceInfo } from '../hooks/useWorkspace';
import { ApiService } from '../services/api';
import { createWorkspaceNative, pickWorkspaceNative } from '../services/nativeFs';
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
 * Studio variant: when `supportsFolderPicker` is true (a studio shell
 * advertising the bridge's native folder picker via capabilities.json), the
 * desktop picker/browser body is REPLACED by the studio flow — a native
 * [Choose Folder…] picker op, an inline [New Project…] create form, and the
 * same "Continue in Documents" consent escape hatch. The desktop variant
 * stays byte-identical when the flag is absent.
 *
 * The overlay sits above the app's toast layer, so every failure surfaces
 * as an inline error here — a toast would be invisible behind the scrim.
 */
interface WorkspaceGateModalProps {
  workspaceInfo: WorkspaceInfo;
  onSelectWorkspace: (path: string) => void;
  onConsentHome: () => void;
}

/** Friendly copy for the bridge createWorkspace error codes. */
const CREATE_ERROR_COPY: Record<string, string> = {
  invalidName: 'That name can’t be used for a folder. Try letters, numbers, spaces, and dashes.',
  alreadyExists: 'A folder with that name already exists. Pick a different name.',
};

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
  const [pending, setPending] = useState<'select' | 'consent' | 'pick' | 'create' | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Studio variant: the inline New Project form. `creating` reveals the
  // name input; `newName` is its controlled value.
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');

  const run = useCallback(
    (kind: 'select' | 'consent' | 'pick' | 'create', action: () => void) => {
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

  // Studio variant — native folder picker + project create instead of the
  // desktop picker/browser. Sits AFTER the cloud short-circuit so a studio
  // shell that somehow reports both still never renders in cloud mode.
  if (supportsFolderPicker) {
    return (
      <div className="workspace-gate-overlay" data-testid="workspace-gate-modal">
        <div className={`workspace-gate-content${pending ? ' is-busy' : ''}`}>
          <div className="workspace-gate-header">
            <div className="workspace-gate-header-icon">
              <AlertTriangle size={28} />
            </div>
            <div>
              <h2 className="workspace-gate-title">Choose a workspace</h2>
              <p className="workspace-gate-subtitle">Pick where this project lives — or create a new project folder.</p>
            </div>
          </div>

          {creating ? (
            <form
              className="workspace-gate-create-form"
              onSubmit={(e) => {
                e.preventDefault();
                run('create', async () => {
                  const result = await createWorkspaceNative(newName);
                  if (result.ok) {
                    window.location.reload();
                    return;
                  }
                  if (result.error === 'userCancelled') {
                    // A cancelled create is not an error — just close the form.
                    setPending(null);
                    setCreating(false);
                    return;
                  }
                  throw new Error(CREATE_ERROR_COPY[result.error] ?? result.error);
                });
              }}
            >
              <label className="workspace-gate-create-label" htmlFor="workspace-gate-create-input">
                Project name
              </label>
              <input
                id="workspace-gate-create-input"
                className="workspace-gate-create-input"
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="my-project"
                autoComplete="off"
                autoFocus
                disabled={pending !== null}
                data-testid="workspace-gate-create-input"
              />
              <div className="workspace-gate-create-actions">
                <button
                  className="workspace-gate-home-btn"
                  type="button"
                  onClick={() => {
                    setCreating(false);
                    setNewName('');
                    setError(null);
                  }}
                  disabled={pending !== null}
                  data-testid="workspace-gate-create-cancel"
                >
                  Cancel
                </button>
                <button
                  className="workspace-gate-primary-btn"
                  type="submit"
                  disabled={pending !== null || newName.trim() === ''}
                  data-testid="workspace-gate-create-submit"
                >
                  {pending === 'create' ? (
                    <>
                      <Loader2 size={14} className="workspace-gate-btn-spin" aria-hidden="true" />
                      Creating&hellip;
                    </>
                  ) : (
                    'Create'
                  )}
                </button>
              </div>
            </form>
          ) : (
            <div className="workspace-gate-studio-actions">
              <button
                className="workspace-gate-primary-btn"
                type="button"
                onClick={() =>
                  run('pick', async () => {
                    const result = await pickWorkspaceNative();
                    if (result.ok) {
                      window.location.reload();
                      return;
                    }
                    if (result.error === 'userCancelled') {
                      // A cancel is not an error — clear pending, no reload.
                      setPending(null);
                      return;
                    }
                    throw new Error(result.error === 'ioFailed' ? 'Could not open the folder picker.' : result.error);
                  })
                }
                disabled={pending !== null}
                data-testid="workspace-gate-pick-btn"
              >
                {pending === 'pick' ? (
                  <>
                    <Loader2 size={14} className="workspace-gate-btn-spin" aria-hidden="true" />
                    Choosing&hellip;
                  </>
                ) : (
                  'Choose Folder…'
                )}
              </button>
              <button
                className="workspace-gate-secondary-btn"
                type="button"
                onClick={() => {
                  setError(null);
                  setCreating(true);
                }}
                disabled={pending !== null}
                data-testid="workspace-gate-new-btn"
              >
                <FolderPlus size={16} aria-hidden="true" />
                New Project…
              </button>
            </div>
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
              onClick={() =>
                // Same api-service path as the desktop variant's consent
                // handler in AppContent: setWorkspace POSTs /api/workspace
                // with consent_home: true and reloads on success.
                run('consent', () => ApiService.getInstance().setWorkspace(workspaceInfo.workspace_root, true))
              }
              disabled={pending !== null}
              data-testid="workspace-gate-home-btn"
            >
              {pending === 'consent' ? (
                <>
                  <Loader2 size={14} className="workspace-gate-btn-spin" aria-hidden="true" />
                  Dismissing&hellip;
                </>
              ) : (
                'Continue in Documents'
              )}
            </button>
            <p className="workspace-gate-home-warning">
              Running in your Documents folder keeps Sprout working, but the agent sees everything there. Choosing a
              dedicated project folder keeps its scope small.
            </p>
          </div>
        </div>
      </div>
    );
  }

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
