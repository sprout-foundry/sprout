import React from 'react';
import { collapseHomePath, getPathDisplayName } from './pathUtils';
import type { RemoteProjectFolderContext } from './types';

export interface ProjectFolderRecentListProps {
  remoteContext: RemoteProjectFolderContext | null;
  recentProjectFolders: string[];
  selectedIndex: number;
  suggestionCount: number;
  onSelectProjectFolder: (path: string) => Promise<void>;
}

export const ProjectFolderRecentList: React.FC<ProjectFolderRecentListProps> = ({
  remoteContext,
  recentProjectFolders,
  selectedIndex,
  suggestionCount,
  onSelectProjectFolder,
}) => {
  return (
    <>
      <div className="location-switcher-section-header" role="presentation">
        {remoteContext ? `Recent Paths on ${remoteContext.hostAlias}` : 'Recent Workspaces'}
      </div>

      <div className="location-switcher-recent-list">
        {recentProjectFolders.length === 0 ? (
          <div className="location-switcher-item location-switcher-item-empty" role="option" aria-selected={false}>
            <span className="location-switcher-item-text">No recent workspaces yet</span>
          </div>
        ) : (
          recentProjectFolders.map((path, index) => {
            const rowIndex = suggestionCount + index;
            return (
              <button
                key={path}
                type="button"
                className={`location-switcher-item ${rowIndex === selectedIndex ? 'selected' : ''}`}
                onClick={() => onSelectProjectFolder(path)}
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">{getPathDisplayName(path)}</span>
                <span className="location-switcher-item-meta">
                  {remoteContext ? collapseHomePath(path, remoteContext.homePath) : path}
                </span>
              </button>
            );
          })
        )}
      </div>
    </>
  );
};
