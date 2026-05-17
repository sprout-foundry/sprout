import React from 'react';
import { collapseHomePath, getPathDisplayName } from './pathUtils';
import type { RemoteWorkspaceContext } from './types';

export interface WorkspaceSSHFavoritesProps {
  remoteContext: RemoteWorkspaceContext;
  workspaceRoot: string;
  favorites: string[];
  isSwitching: boolean;
  onSelectFavorite: (path: string) => Promise<void>;
  onFillInput: (path: string) => void;
  onSaveFavorite: () => void;
  onRemoveFavorite: (path: string) => void;
}

export const WorkspaceSSHFavorites: React.FC<WorkspaceSSHFavoritesProps> = ({
  remoteContext,
  workspaceRoot,
  favorites,
  isSwitching,
  onSelectFavorite,
  onFillInput,
  onSaveFavorite,
  onRemoveFavorite,
}) => {
  return (
    <>
      <div className="location-switcher-section-header" role="presentation">
        Favorite Paths on {remoteContext.hostAlias}
      </div>
      <div className="location-switcher-ssh-actions">
        <button
          type="button"
          className="location-switcher-session-btn"
          onClick={onSaveFavorite}
          disabled={!workspaceRoot || isSwitching}
        >
          Save Current Path
        </button>
      </div>
      <div className="location-switcher-recent-list">
        {favorites.length === 0 ? (
          <div className="location-switcher-item location-switcher-item-empty" role="option" aria-selected={false}>
            <span className="location-switcher-item-text">No saved paths on this host yet</span>
          </div>
        ) : (
          favorites.map((path) => (
            <div
              key={`remote-favorite-${remoteContext.hostAlias}-${path}`}
              className="location-switcher-item location-switcher-item-session"
            >
              <span className="location-switcher-item-text">{getPathDisplayName(path)}</span>
              <span className="location-switcher-item-meta">{collapseHomePath(path, remoteContext.homePath)}</span>
              <div className="location-switcher-session-actions">
                <button
                  type="button"
                  className="location-switcher-session-btn"
                  onClick={() => onSelectFavorite(path)}
                  disabled={isSwitching}
                >
                  Open
                </button>
                <button
                  type="button"
                  className="location-switcher-session-btn"
                  onClick={() => onFillInput(path)}
                  disabled={isSwitching}
                >
                  Fill
                </button>
                <button
                  type="button"
                  className="location-switcher-session-btn danger"
                  onClick={() => onRemoveFavorite(path)}
                  disabled={isSwitching}
                >
                  Remove
                </button>
              </div>
            </div>
          ))
        )}
      </div>
    </>
  );
};
