import { FolderOpen, Folder, FolderSearch } from 'lucide-react';
import './WorkspacePicker.css';

/* ── Types ────────────────────────────────────────────────────────── */

export interface ProjectSuggestion {
  path: string;
  name: string;
  markers: string[];
}

export interface RecentWorkspace {
  path: string;
  name: string;
  last_used: string;
  markers: string[];
  session_count: number;
}

export interface WorkspacePickerProps {
  daemonRoot: string;
  currentWorkspace: string;
  suggestedProjects: ProjectSuggestion[];
  recentWorkspaces: RecentWorkspace[];
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
    <span className="workspace-picker-markers">
      {markers.map((m) => (
        <span key={m} className="workspace-picker-marker">
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
}: {
  icon: JSX.Element;
  name: string;
  path: string;
  markers: string[];
  timeAgo?: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      className="workspace-picker-row"
      type="button"
      onClick={onClick}
      title={path}
      data-testid="workspace-picker-option"
    >
      <div className="workspace-picker-row-icon">{icon}</div>
      <div className="workspace-picker-row-info">
        <span className="workspace-picker-row-name">{name}</span>
        <MarkerBadges markers={markers} />
      </div>
      {timeAgo && <span className="workspace-picker-row-time">{timeAgo}</span>}
    </button>
  );
}

/* ── Main Component ───────────────────────────────────────────────── */

function WorkspacePicker({
  daemonRoot,
  currentWorkspace,
  suggestedProjects,
  recentWorkspaces,
  onSelect,
  onBrowse,
}: WorkspacePickerProps): JSX.Element {
  // Derive home directory from daemon root (e.g. /home/user/.sprout → /home/user)
  const homeDir = (() => {
    if (!daemonRoot) return '';
    const idx = daemonRoot.includes('.sprout') ? daemonRoot.lastIndexOf('/.sprout') : -1;
    if (idx !== -1) return daemonRoot.slice(0, idx);
    if (/^\/home\/[^/]+\/?$/.test(daemonRoot) || /^\/root\/?$/.test(daemonRoot)) return daemonRoot.replace(/\/+$/, '');
    return '';
  })();

  const displayWorkspace = expandHomePath(currentWorkspace, homeDir) || '/';

  return (
    <div className="workspace-picker" data-testid="workspace-picker">
      {/* ── Header ──────────────────────────────────────────────── */}
      <div className="workspace-picker-header">
        <div className="workspace-picker-header-icon">
          <FolderOpen size={24} />
        </div>
        <div>
          <h2 className="workspace-picker-title">No project workspace detected</h2>
          <p className="workspace-picker-subtitle">
            Current: <span className="workspace-picker-path">{displayWorkspace}</span>
          </p>
        </div>
      </div>

      {/* ── Recent Projects ─────────────────────────────────────── */}
      <section className="workspace-picker-section">
        <h3 className="workspace-picker-section-title">Recent Projects</h3>
        {recentWorkspaces.length > 0 ? (
          recentWorkspaces.map((ws) => (
            <ProjectRow
              key={ws.path}
              icon={<Folder size={18} />}
              name={ws.name || ws.path.split('/').filter(Boolean).pop() || ws.path}
              path={expandHomePath(ws.path, homeDir)}
              markers={ws.markers}
              timeAgo={formatTimeAgo(ws.last_used)}
              onClick={() => onSelect(ws.path)}
            />
          ))
        ) : (
          <p className="workspace-picker-empty">No recent projects</p>
        )}
      </section>

      {/* ── Nearby Projects ─────────────────────────────────────── */}
      <section className="workspace-picker-section">
        <h3 className="workspace-picker-section-title">Nearby Projects</h3>
        {suggestedProjects.length > 0 ? (
          suggestedProjects.map((proj) => (
            <ProjectRow
              key={proj.path}
              icon={<FolderSearch size={18} />}
              name={proj.name || proj.path.split('/').filter(Boolean).pop() || proj.path}
              path={expandHomePath(proj.path, homeDir)}
              markers={proj.markers}
              onClick={() => onSelect(proj.path)}
            />
          ))
        ) : (
          <p className="workspace-picker-empty">No nearby projects found</p>
        )}
      </section>

      {/* ── Browse Button ───────────────────────────────────────── */}
      <button className="workspace-picker-browse-btn" type="button" onClick={onBrowse}>
        Browse&hellip;
      </button>
    </div>
  );
}

export default WorkspacePicker;
