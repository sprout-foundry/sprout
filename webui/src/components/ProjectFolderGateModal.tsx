import { AlertTriangle, FolderPlus, Loader2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { useHostCapabilities } from '../host';
import type { ProjectFolderInfo } from '../hooks/useProjectFolder';
import { ApiService, type Starter } from '../services/api';
import { createProjectFolderNative, pickProjectFolderNative } from '../services/nativeFs';
import { setProjectFolderGateOpen } from '../services/projectFolderGate';
import { refreshProjectFolder } from '../services/projectFolderStore';
import { consentToHomeProjectFolder } from '../services/projectFolderSync';
import ProjectFolderBrowser from './ProjectFolderBrowser';
import ProjectFolderPicker from './ProjectFolderPicker';
import './ProjectFolderGateModal.css';

/**
 * ProjectFolderGateModal — a full-screen blocking overlay (SP-130) shown when
 * the resolved project folder is the user's home directory without consent.
 *
 * Unlike WelcomeTab's inline ProjectFolderPicker (shown for a generic
 * non-project directory), this modal *blocks* the editor/chat/files until
 * the user either selects a project folder or explicitly consents to the
 * home directory.
 *
 * Renders only in local mode — cloud mode has a single virtual FS, so
 * `supportsProjectFolderSwitching` (false in cloud) short-circuits to null.
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
interface ProjectFolderGateModalProps {
  projectFolderInfo: ProjectFolderInfo;
  onSelectProjectFolder: (path: string) => void;
  onConsentHome: () => void;
}

/** Friendly copy for the bridge createProjectFolder error codes. */
const CREATE_ERROR_COPY: Record<string, string> = {
  invalidName: 'That name can’t be used for a folder. Try letters, numbers, spaces, and dashes.',
  alreadyExists: 'A folder with that name already exists. Pick a different name.',
};

function ProjectFolderGateModal({
  projectFolderInfo,
  onSelectProjectFolder,
  onConsentHome,
}: ProjectFolderGateModalProps): JSX.Element | null {
  const { folderPicker: supportsFolderPicker, projectFolderSwitching: supportsProjectFolderSwitching } =
    useHostCapabilities();
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
  // The embedded starter catalogue for the studio create form.
  // `starterOptions` is populated when the form is revealed; `selectedStarter`
  // is the chosen starter id, or '' for "blank" (no starter — the default).
  const [starterOptions, setStarterOptions] = useState<Starter[]>([]);
  const [selectedStarter, setSelectedStarter] = useState('');

  const run = useCallback(
    (kind: 'select' | 'consent' | 'pick' | 'create', action: () => void) => {
      if (pending) return;
      setError(null);
      setPending(kind);
      try {
        // The callers are async but not awaited here on purpose: a resolved
        // setProjectFolder triggers a page reload, so post-success cleanup is moot.
        // Only the failure path matters, and it is captured below.
        Promise.resolve(action()).catch((err: unknown) => {
          const message = err instanceof Error ? err.message : String(err);
          setError(message || 'Failed to update the project folder');
          setPending(null);
        });
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setPending(null);
      }
    },
    [pending],
  );

  useEffect(() => {
    if (!supportsProjectFolderSwitching) return undefined;
    setProjectFolderGateOpen(true);
    return () => setProjectFolderGateOpen(false);
  }, []);

  // Studio create form: load the embedded starter catalogue
  // when the form is revealed, so the chooser offers the starters. The create
  // form is only reachable in the studio variant, so `creating` implies
  // studio mode. A fetch failure (or an empty catalogue) degrades to "blank"
  // only — create is never blocked.
  useEffect(() => {
    if (!creating) return undefined;
    let live = true;
    void (async () => {
      try {
        const list = await ApiService.getInstance().listStarters();
        if (live) setStarterOptions(Array.isArray(list) ? list : []);
      } catch {
        if (live) setStarterOptions([]);
      }
    })();
    return () => {
      live = false;
    };
  }, [creating]);

  // Cloud mode (and any mode without project folder switching) is never gated.
  if (!supportsProjectFolderSwitching) return null;

  // Studio variant — native folder picker + project create instead of the
  // desktop picker/browser. Sits AFTER the cloud short-circuit so a studio
  // shell that somehow reports both still never renders in cloud mode.
  if (supportsFolderPicker) {
    return (
      <div className="project-folder-gate-overlay" data-testid="project-folder-gate-modal">
        <div className={`project-folder-gate-content${pending ? ' is-busy' : ''}`}>
          <div className="project-folder-gate-header">
            <div className="project-folder-gate-header-icon">
              <AlertTriangle size={28} />
            </div>
            <div>
              <h2 className="project-folder-gate-title">Choose a project folder</h2>
              <p className="project-folder-gate-subtitle">
                Pick where this project lives — or create a new project folder.
              </p>
            </div>
          </div>

          {creating ? (
            <form
              className="project-folder-gate-create-form"
              onSubmit={(e) => {
                e.preventDefault();
                run('create', async () => {
                  const result = await createProjectFolderNative(newName);
                  if (result.ok) {
                    // A starter was picked: the bridge created the folder and
                    // made it the project folder, so the new project's
                    // absolute path is the current project folder (GET
                    // /api/project-folder → project_folder). Instantiate the
                    // starter there. "Blank" skips this step, leaving the
                    // create flow byte-identical to a plain folder create.
                    if (selectedStarter !== '') {
                      const ws = await refreshProjectFolder({ force: true });
                      await ApiService.getInstance().instantiateStarter({
                        starter: selectedStarter,
                        path: ws.project_folder,
                        name: newName,
                      });
                    }
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
              <label className="project-folder-gate-create-label" htmlFor="project-folder-gate-create-input">
                Project name
              </label>
              <input
                id="project-folder-gate-create-input"
                className="project-folder-gate-create-input"
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="my-project"
                autoComplete="off"
                autoFocus
                disabled={pending !== null}
                data-testid="project-folder-gate-create-input"
              />
              <label className="project-folder-gate-create-label" htmlFor="project-folder-gate-starter-select">
                Starter
              </label>
              <select
                id="project-folder-gate-starter-select"
                className="project-folder-gate-starter-select"
                value={selectedStarter}
                onChange={(e) => setSelectedStarter(e.target.value)}
                disabled={pending !== null}
                data-testid="project-folder-gate-starter-select"
              >
                <option value="">Blank (no starter)</option>
                {starterOptions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.id} (v{s.version}, {s.files} files)
                  </option>
                ))}
              </select>
              <div className="project-folder-gate-create-actions">
                <button
                  className="project-folder-gate-home-btn"
                  type="button"
                  onClick={() => {
                    // Reset the form. setPending is defensive — the button is
                    // disabled while a create is in flight (pending !== null) —
                    // but it guarantees the form is idle when re-revealed.
                    setCreating(false);
                    setNewName('');
                    setError(null);
                    setSelectedStarter('');
                    setPending(null);
                  }}
                  disabled={pending !== null}
                  data-testid="project-folder-gate-create-cancel"
                >
                  Cancel
                </button>
                <button
                  className="project-folder-gate-primary-btn"
                  type="submit"
                  disabled={pending !== null || newName.trim() === ''}
                  data-testid="project-folder-gate-create-submit"
                >
                  {pending === 'create' ? (
                    <>
                      <Loader2 size={14} className="project-folder-gate-btn-spin" aria-hidden="true" />
                      Creating&hellip;
                    </>
                  ) : (
                    'Create'
                  )}
                </button>
              </div>
            </form>
          ) : (
            <div className="project-folder-gate-studio-actions">
              <button
                className="project-folder-gate-primary-btn"
                type="button"
                onClick={() =>
                  run('pick', async () => {
                    const result = await pickProjectFolderNative();
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
                data-testid="project-folder-gate-pick-btn"
              >
                {pending === 'pick' ? (
                  <>
                    <Loader2 size={14} className="project-folder-gate-btn-spin" aria-hidden="true" />
                    Choosing&hellip;
                  </>
                ) : (
                  'Choose Folder…'
                )}
              </button>
              <button
                className="project-folder-gate-secondary-btn"
                type="button"
                onClick={() => {
                  setError(null);
                  setCreating(true);
                }}
                disabled={pending !== null}
                data-testid="project-folder-gate-new-btn"
              >
                <FolderPlus size={16} aria-hidden="true" />
                New Project…
              </button>
            </div>
          )}

          {error && (
            <div className="project-folder-gate-error" role="alert" data-testid="project-folder-gate-error">
              {error}
            </div>
          )}

          <div className="project-folder-gate-home-consent">
            <button
              className="project-folder-gate-home-btn"
              type="button"
              onClick={() =>
                // Same path as the desktop variant's consent handler in
                // AppContent: re-read the project folder, then POST
                // /api/project-folder with consent_home: true if still needed.
                run('consent', () => consentToHomeProjectFolder())
              }
              disabled={pending !== null}
              data-testid="project-folder-gate-home-btn"
            >
              {pending === 'consent' ? (
                <>
                  <Loader2 size={14} className="project-folder-gate-btn-spin" aria-hidden="true" />
                  Dismissing&hellip;
                </>
              ) : (
                'Continue in Documents'
              )}
            </button>
            <p className="project-folder-gate-home-warning">
              Running in your Documents folder keeps Sprout working, but the agent sees everything there. Choosing a
              dedicated project folder keeps its scope small.
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="project-folder-gate-overlay" data-testid="project-folder-gate-modal">
      <div className={`project-folder-gate-content${pending ? ' is-busy' : ''}`}>
        <div className="project-folder-gate-header">
          <div className="project-folder-gate-header-icon">
            <AlertTriangle size={28} />
          </div>
          <div>
            <h2 className="project-folder-gate-title">Select a project folder</h2>
            <p className="project-folder-gate-subtitle">
              Sprout is running in your home directory, which gives the agent access to all your files. Select a project
              folder to limit its scope.
            </p>
          </div>
        </div>

        {browsing ? (
          <ProjectFolderBrowser
            initialPath={projectFolderInfo.project_folder || projectFolderInfo.daemon_root}
            onSelect={(path) => run('select', () => onSelectProjectFolder(path))}
            onCancel={() => setBrowsing(false)}
          />
        ) : (
          <ProjectFolderPicker
            daemonRoot={projectFolderInfo.daemon_root}
            currentProjectFolder={projectFolderInfo.project_folder}
            suggestedProjects={projectFolderInfo.suggested_projects}
            recentProjectFolders={projectFolderInfo.recent_project_folders}
            onSelect={(path) => run('select', () => onSelectProjectFolder(path))}
            onBrowse={() => setBrowsing(true)}
          />
        )}

        {error && (
          <div className="project-folder-gate-error" role="alert" data-testid="project-folder-gate-error">
            {error}
          </div>
        )}

        <div className="project-folder-gate-home-consent">
          <button
            className="project-folder-gate-home-btn"
            type="button"
            onClick={() => run('consent', onConsentHome)}
            disabled={pending !== null}
            data-testid="project-folder-gate-home-btn"
          >
            {pending === 'consent' ? (
              <>
                <Loader2 size={14} className="project-folder-gate-btn-spin" aria-hidden="true" />
                Dismissing&hellip;
              </>
            ) : (
              'Use my home directory anyway'
            )}
          </button>
          <p className="project-folder-gate-home-warning">
            Running in your home directory gives the agent unrestricted access to all files and may trigger macOS
            permission prompts for protected folders like Music and Photos.
          </p>
        </div>
      </div>
    </div>
  );
}

export default ProjectFolderGateModal;
