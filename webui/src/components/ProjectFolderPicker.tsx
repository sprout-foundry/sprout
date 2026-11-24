import { FolderOpen, Folder, FolderSearch, Loader2, AlertTriangle } from 'lucide-react';
import { useState } from 'react';
import type { ReactElement } from 'react';
import { toUserErrorMessage } from '../utils/errorMessage';
import './ProjectFolderPicker.css';

/* ── Types ────────────────────────────────────────────────────────── */

export interface ProjectSuggestion {
  path: string;
  name: string;
  markers: string[];
}

export interface RecentProjectFolder {
  path: string;
  name: string;
  last_used: string;
  markers: string[];
  session_count: number;
}

export interface ProjectFolderPickerProps {
  daemonRoot: string;
  currentProjectFolder: string;
  suggestedProjects: ProjectSuggestion[];
  recentProjectFolders: RecentProjectFolder[];
  onSelect: (path: string) => void;
  onBrowse: () => void;
}

/* ── Helpers ──────────────────────────────────────────────────────── */

function formatTimeAgo(isoString: string): string {
  const diff = Date.now() - new Date(isoString).getTime();
  if (diff < 0) return 'just now';
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function expandHomePath(path: string, homeDir: string): string {
  if (homeDir && path.startsWith(homeDir)) {
    return '~' + path.slice(homeDir.length) || '~';
  }
  return path;
}

/* ── Sub-components ───────────────────────────────────────────────── */

function MarkerBadges({ markers }: { markers: string[] }): JSX.Element | null {
  if (!markers.length) return null;
  return (
    <span className="project-folder-picker-markers">
      {markers.map((m) => (
        <span key={m} className="project-folder-picker-marker">
          {m}
        </span>
      ))}
    </span>
  );
}

function ProjectRow({
  icon,
  name,
  path,
  markers,
  timeAgo,
  onClick,
  pending,
  disabled,
}: {
  icon: ReactElement;
  name: string;
  path: string;
  markers: string[];
  timeAgo?: string;
  onClick: () => void;
  pending?: boolean;
  disabled?: boolean;
}): ReactElement {
  return (
    <button
      className="project-folder-picker-row"
      type="button"
      onClick={onClick}
      title={path}
      disabled={disabled}
      aria-busy={pending || undefined}
      data-testid="project-folder-picker-option"
    >
      <div className="project-folder-picker-row-icon">
        {pending ? <Loader2 size={18} className="spin" aria-hidden="true" /> : icon}
      </div>
      <div className="project-folder-picker-row-info">
        <span className="project-folder-picker-row-name">{name}</span>
        <MarkerBadges markers={markers} />
      </div>
      {timeAgo && <span className="project-folder-picker-row-time">{timeAgo}</span>}
    </button>
  );
}

/* ── Main Component ───────────────────────────────────────────────── */

function ProjectFolderPicker({
  daemonRoot,
  currentProjectFolder,
  suggestedProjects,
  recentProjectFolders,
  onSelect,
  onBrowse,
}: ProjectFolderPickerProps): JSX.Element {
  // Switching a project folder is a user-initiated action that can fail (backend
  // unreachable, permission denied on the target). Track the in-flight row so
  // the button shows progress, and surface failures inline instead of
  // leaving an unhandled rejection.
  const [switchingPath, setSwitchingPath] = useState<string | null>(null);
  const [switchError, setSwitchError] = useState<string | null>(null);

  // Derive home directory from daemon root (e.g. /home/user/.sprout → /home/user)
  const homeDir = (() => {
    if (!daemonRoot) return '';
    const idx = daemonRoot.includes('.sprout') ? daemonRoot.lastIndexOf('/.sprout') : -1;
    if (idx !== -1) return daemonRoot.slice(0, idx);
    if (/^\/home\/[^/]+\/?$/.test(daemonRoot) || /^\/root\/?$/.test(daemonRoot)) return daemonRoot.replace(/\/+$/, '');
    return '';
  })();

  const displayProjectFolder = expandHomePath(currentProjectFolder, homeDir) || '/';
  const isSwitching = switchingPath !== null;

  const handleSelect = (path: string) => {
    if (isSwitching) return;
    setSwitchingPath(path);
    setSwitchError(null);
    // onSelect is allowed to be synchronous; only treat rejections as errors.
    Promise.resolve(onSelect(path)).catch((err: unknown) => {
      setSwitchError(toUserErrorMessage(err, 'Could not switch to that project folder.'));
      // Release the lock so the user can retry or pick a different project.
      // The success path never lands here — the page reloads instead.
      setSwitchingPath(null);
    });
  };

  return (
    <div className="project-folder-picker" data-testid="project-folder-picker">
      {/* ── Header ──────────────────────────────────────────────── */}
      <div className="project-folder-picker-header">
        <div className="project-folder-picker-header-icon">
          <FolderOpen size={24} />
        </div>
        <div>
          <h2 className="project-folder-picker-title">No project folder detected</h2>
          <p className="project-folder-picker-subtitle">
            Current: <span className="project-folder-picker-path">{displayProjectFolder}</span>
          </p>
        </div>
      </div>

      {/* Switch failure — first thing in the body, above the lists the user
          just acted on, so it can't land below the fold on a tablet. */}
      {switchError && (
        <div className="project-folder-picker-error" role="alert" data-testid="project-folder-picker-error">
          <AlertTriangle size={14} aria-hidden="true" />
          <span>{switchError}</span>
        </div>
      )}

      {/* ── Recent Projects ─────────────────────────────────────── */}
      <section className="project-folder-picker-section">
        <h3 className="project-folder-picker-section-title">Recent Projects</h3>
        {recentProjectFolders.length > 0 ? (
          recentProjectFolders.map((ws) => (
            <ProjectRow
              key={ws.path}
              icon={<Folder size={18} />}
              name={ws.name || ws.path.split('/').filter(Boolean).pop() || ws.path}
              path={expandHomePath(ws.path, homeDir)}
              markers={ws.markers}
              timeAgo={formatTimeAgo(ws.last_used)}
              onClick={() => handleSelect(ws.path)}
              pending={switchingPath === ws.path}
              disabled={isSwitching}
            />
          ))
        ) : (
          <p className="project-folder-picker-empty">No recent projects</p>
        )}
      </section>

      {/* ── Nearby Projects ─────────────────────────────────────── */}
      <section className="project-folder-picker-section">
        <h3 className="project-folder-picker-section-title">Nearby Projects</h3>
        {suggestedProjects.length > 0 ? (
          suggestedProjects.map((proj) => (
            <ProjectRow
              key={proj.path}
              icon={<FolderSearch size={18} />}
              name={proj.name || proj.path.split('/').filter(Boolean).pop() || proj.path}
              path={expandHomePath(proj.path, homeDir)}
              markers={proj.markers}
              onClick={() => handleSelect(proj.path)}
              pending={switchingPath === proj.path}
              disabled={isSwitching}
            />
          ))
        ) : (
          <p className="project-folder-picker-empty">No nearby projects found</p>
        )}
      </section>

      {/* ── Browse Button ───────────────────────────────────────── */}
      <button
        className="project-folder-picker-browse-btn"
        type="button"
        onClick={onBrowse}
        disabled={isSwitching}
        aria-busy={isSwitching || undefined}
      >
        {isSwitching ? 'Switching…' : 'Browse…'}
      </button>
    </div>
  );
}

export default ProjectFolderPicker;
