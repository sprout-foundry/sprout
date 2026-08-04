import { ArrowLeft, ChevronRight, Folder, Loader2 } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { ApiService } from '../services/api';
import type { BrowseEntry } from '../services/api/workspaceApi';
import './WorkspaceBrowser.css';

/**
 * WorkspaceBrowser — an inline directory picker for the workspace gate.
 *
 * The gate is a blocking overlay, so it cannot delegate browsing to the
 * chrome's location switcher: that popover renders far below the overlay in
 * the stacking order and opens *behind* the gate, anchored to a trigger the
 * scrim has already hidden. Browsing therefore lives inside the modal.
 *
 * Navigation is bounded by the daemon root, matching the backend, which
 * rejects any path outside it.
 */
interface WorkspaceBrowserProps {
  /** Directory to open first — usually the current workspace root. */
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

function WorkspaceBrowser({ initialPath, onSelect, onCancel }: WorkspaceBrowserProps): JSX.Element {
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
    <div className="workspace-browser" data-testid="workspace-browser">
      <div className="workspace-browser-bar">
        <button
          className="workspace-browser-up"
          type="button"
          onClick={() => void load(parentOf(path))}
          disabled={!canGoUp || loading}
          title={canGoUp ? `Up to ${parentOf(path)}` : 'Already at the top of the allowed area'}
          aria-label="Go to parent directory"
        >
          <ArrowLeft size={16} />
        </button>
        <span className="workspace-browser-path" title={path}>
          {displayPath}
        </span>
      </div>

      <div className="workspace-browser-list" data-testid="workspace-browser-list">
        {loading ? (
          <div className="workspace-browser-status">
            <Loader2 size={16} className="workspace-browser-spin" aria-hidden="true" />
            <span>Loading&hellip;</span>
          </div>
        ) : error ? (
          <div className="workspace-browser-status workspace-browser-status--error">{error}</div>
        ) : directories.length === 0 ? (
          <div className="workspace-browser-status">No sub-folders here</div>
        ) : (
          directories.map((dir) => (
            <button
              key={dir.path}
              className="workspace-browser-row"
              type="button"
              onClick={() => void load(dir.path)}
              title={dir.path}
              data-testid="workspace-browser-entry"
            >
              <Folder size={16} className="workspace-browser-row-icon" />
              <span className="workspace-browser-row-name">{dir.name}</span>
              <ChevronRight size={14} className="workspace-browser-row-chevron" />
            </button>
          ))
        )}
      </div>

      <div className="workspace-browser-actions">
        <button className="workspace-browser-cancel" type="button" onClick={onCancel}>
          Back
        </button>
        <button
          className="workspace-browser-confirm"
          type="button"
          onClick={() => onSelect(path)}
          disabled={loading || !path}
          data-testid="workspace-browser-confirm"
        >
          Use this folder
        </button>
      </div>
    </div>
  );
}

export default WorkspaceBrowser;
