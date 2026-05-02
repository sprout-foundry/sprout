import React from 'react';
import { RemoteWorkspaceContext } from './types';
import { collapseHomePath, getPathDisplayName } from './pathUtils';

export interface WorkspaceRecentListProps {
  remoteContext: RemoteWorkspaceContext | null;
  recentWorkspaces: string[];
  selectedIndex: number;
  suggestionCount: number;
  onSelectWorkspace: (path: string) => Promise<void>;
}

export const WorkspaceRecentList: React.FC<WorkspaceRecentListProps> = ({
  remoteContext,
  recentWorkspaces,
  selectedIndex,
  suggestionCount,
  onSelectWorkspace,
}) => {
  return (
    <>
      <div className="location-switcher-section-header" role="presentation">
        {remoteContext
          ? `Recent Paths on ${remoteContext.hostAlias}`
          : 'Recent Workspaces'}
      </div>

      <div className="location-switcher-recent-list">
        {recentWorkspaces.length === 0 ? (
          <div
            className="location-switcher-item location-switcher-item-empty"
            role="option"
            aria-selected={false}
          >
            <span className="location-switcher-item-text">
              No recent workspaces yet
            </span>
          </div>
        ) : (
          recentWorkspaces.map((path, index) => {
            const rowIndex = suggestionCount + index;
            return (
              <button
                key={path}
                type="button"
                className={`location-switcher-item ${
                  rowIndex === selectedIndex ? 'selected' : ''
                }`}
                onClick={() => onSelectWorkspace(path)}
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">
                  {getPathDisplayName(path)}
                </span>
                <span className="location-switcher-item-meta">
                  {remoteContext
                    ? collapseHomePath(path, remoteContext.homePath)
                    : path}
                </span>
              </button>
            );
          })
        )}
      </div>
    </>
  );
};
