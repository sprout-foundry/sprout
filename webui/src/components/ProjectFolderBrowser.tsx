import { ArrowLeft, ChevronRight, Folder, Loader2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { ApiService } from '../services/api';
import type { BrowseEntry } from '../services/api/projectFolderApi';
import './ProjectFolderBrowser.css';

/**
 * ProjectFolderBrowser — an inline directory picker for the project folder gate.
 *
 * The gate is a blocking overlay, so it cannot delegate browsing to the
 * chrome's location switcher: that popover renders far below the overlay in
 * the stacking order and opens *behind* the gate, anchored to a trigger the
 * scrim has already hidden. Browsing therefore lives inside the modal.
 *
 * Navigation is bounded by the daemon root, matching the backend, which
 * rejects any path outside it.
 */
interface ProjectFolderBrowserProps {
  /** Directory to open first — usually the current project folder. */
  initialPath: string;
  /** Called with the directory the user commits to. */
  onSelect: (path: string) => void;
  /** Return to the suggestion list without choosing. */
  onCancel: () => void;
}

function parentOf(path: string): string {
  const trimmed = path.replace(/\/+$/, '');
  const idx = trimmed.lastIndexOf('/');
  if (idx <= 0) return '/';
  return trimmed.slice(0, idx);
}

function ProjectFolderBrowser({ initialPath, onSelect, onCancel }: ProjectFolderBrowserProps): JSX.Element {
  const [path, setPath] = useState(initialPath);
  const [daemonRoot, setDaemonRoot] = useState('');
  const [directories, setDirectories] = useState<BrowseEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (target: string) => {
    setLoading(true);
    setError(null);
    try {
      const result = await ApiService.getInstance().browseDirectory(target || undefined);
      setPath(result.path);
      setDaemonRoot(result.daemonRoot);
      setDirectories(result.directories);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to read directory');
      setDirectories([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(initialPath);
  }, [initialPath, load]);

  // The daemon root is the browse boundary; the backend rejects anything above it.
  const canGoUp = daemonRoot !== '' && path !== daemonRoot && path.startsWith(daemonRoot);

  const displayPath = daemonRoot && path.startsWith(daemonRoot) ? '~' + path.slice(daemonRoot.length) || '~' : path;

  return (
    <div className="project-folder-browser" data-testid="project-folder-browser">
      <div className="project-folder-browser-bar">
        <button
          className="project-folder-browser-up"
          type="button"
          onClick={() => void load(parentOf(path))}
          disabled={!canGoUp || loading}
          title={canGoUp ? `Up to ${parentOf(path)}` : 'Already at the top of the allowed area'}
          aria-label="Go to parent directory"
        >
          <ArrowLeft size={16} />
        </button>
        <span className="project-folder-browser-path" title={path}>
          {displayPath}
        </span>
      </div>

      <div className="project-folder-browser-list" data-testid="project-folder-browser-list">
        {loading ? (
          <div className="project-folder-browser-status">
            <Loader2 size={16} className="project-folder-browser-spin" aria-hidden="true" />
            <span>Loading&hellip;</span>
          </div>
        ) : error ? (
          <div className="project-folder-browser-status project-folder-browser-status--error">{error}</div>
        ) : directories.length === 0 ? (
          <div className="project-folder-browser-status">No sub-folders here</div>
        ) : (
          directories.map((dir) => (
            <button
              key={dir.path}
              className="project-folder-browser-row"
              type="button"
              onClick={() => void load(dir.path)}
              title={dir.path}
              data-testid="project-folder-browser-entry"
            >
              <Folder size={16} className="project-folder-browser-row-icon" />
              <span className="project-folder-browser-row-name">{dir.name}</span>
              <ChevronRight size={14} className="project-folder-browser-row-chevron" />
            </button>
          ))
        )}
      </div>

      <div className="project-folder-browser-actions">
        <button className="project-folder-browser-cancel" type="button" onClick={onCancel}>
          Back
        </button>
        <button
          className="project-folder-browser-confirm"
          type="button"
          onClick={() => onSelect(path)}
          disabled={loading || !path}
          data-testid="project-folder-browser-confirm"
        >
          Use this folder
        </button>
      </div>
    </div>
  );
}

export default ProjectFolderBrowser;
