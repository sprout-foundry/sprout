import { Server } from 'lucide-react';
import { createPortal } from 'react-dom';
import { collapseHomePath, getPathDisplayName } from './pathUtils';

export interface SSHWorkspacePickerDialogProps {
  show: boolean;
  sshPickerHostAlias: string;
  sshPickerPath: string;
  remoteRecentWorkspaces: Record<string, string[]>;
  sshFavoriteWorkspaces: Record<string, string[]>;
  sshHomePaths: Record<string, string>;
  submitWorkspaceChange: (path: string) => Promise<void>;
  setShow: (show: boolean) => void;
  setSshPickerPath: (path: string) => void;
}

export const SSHWorkspacePickerDialog: React.FC<SSHWorkspacePickerDialogProps> = ({
  show,
  sshPickerHostAlias,
  sshPickerPath,
  remoteRecentWorkspaces,
  sshFavoriteWorkspaces,
  sshHomePaths,
  submitWorkspaceChange,
  setShow,
  setSshPickerPath,
}) => {
  if (!show) return null;

  return createPortal(
    <div className="ssh-workspace-picker-overlay" role="dialog" aria-modal="true" aria-label="Select SSH workspace">
      <div className="ssh-workspace-picker-dialog">
        <div className="ssh-workspace-picker-header">
          <Server size={14} />
          <span>
            Select workspace on <strong>{sshPickerHostAlias}</strong>
          </span>
        </div>
        <div className="ssh-workspace-picker-body">
          {/* Recent workspaces for this host */}
          {(remoteRecentWorkspaces[sshPickerHostAlias] || []).length > 0 ? (
            <div className="ssh-workspace-picker-section">
              <div className="ssh-workspace-picker-section-label">Recent</div>
              {(remoteRecentWorkspaces[sshPickerHostAlias] || []).map((path) => (
                <button
                  key={`picker-recent-${path}`}
                  type="button"
                  className="ssh-workspace-picker-item"
                  onClick={() => {
                    setShow(false);
                    void submitWorkspaceChange(path);
                  }}
                >
                  <span className="ssh-workspace-picker-item-name">{getPathDisplayName(path)}</span>
                  <span className="ssh-workspace-picker-item-path">
                    {collapseHomePath(path, sshHomePaths[sshPickerHostAlias])}
                  </span>
                </button>
              ))}
            </div>
          ) : null}
          {/* Saved favorites for this host */}
          {(sshFavoriteWorkspaces[sshPickerHostAlias] || []).length > 0 ? (
            <div className="ssh-workspace-picker-section">
              <div className="ssh-workspace-picker-section-label">Favorites</div>
              {(sshFavoriteWorkspaces[sshPickerHostAlias] || []).map((path) => (
                <button
                  key={`picker-fav-${path}`}
                  type="button"
                  className="ssh-workspace-picker-item"
                  onClick={() => {
                    setShow(false);
                    void submitWorkspaceChange(path);
                  }}
                >
                  <span className="ssh-workspace-picker-item-name">{getPathDisplayName(path)}</span>
                  <span className="ssh-workspace-picker-item-path">
                    {collapseHomePath(path, sshHomePaths[sshPickerHostAlias])}
                  </span>
                </button>
              ))}
            </div>
          ) : null}
          {/* Manual path input */}
          <div className="ssh-workspace-picker-input-row">
            <input
              type="text"
              className="ssh-workspace-picker-input"
              value={sshPickerPath}
              onChange={(event) => setSshPickerPath(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && sshPickerPath.trim()) {
                  setShow(false);
                  void submitWorkspaceChange(sshPickerPath);
                }
              }}
              placeholder="Enter remote path…"
              autoComplete="off"
              spellCheck={false}
              autoFocus={
                (remoteRecentWorkspaces[sshPickerHostAlias] || []).length === 0 &&
                (sshFavoriteWorkspaces[sshPickerHostAlias] || []).length === 0
              }
            />
            <button
              type="button"
              className="location-switcher-session-btn primary"
              onClick={() => {
                if (sshPickerPath.trim()) {
                  setShow(false);
                  void submitWorkspaceChange(sshPickerPath);
                }
              }}
              disabled={!sshPickerPath.trim()}
            >
              Go
            </button>
          </div>
        </div>
        <div className="ssh-workspace-picker-footer">
          <button type="button" className="ssh-workspace-picker-dismiss" onClick={() => setShow(false)}>
            Work in home directory
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
