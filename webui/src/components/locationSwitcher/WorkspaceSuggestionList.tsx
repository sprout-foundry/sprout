import React from 'react';
import { Loader2 } from 'lucide-react';
import { WorkspaceDirectory, RemoteWorkspaceContext } from './types';
import { collapseHomePath } from './pathUtils';

export interface WorkspaceSuggestionListProps {
  workspaceRoot: string;
  remoteContext: RemoteWorkspaceContext | null;
  suggestions: WorkspaceDirectory[];
  suggestionsLoading: boolean;
  suggestionsError: string | null;
  selectedIndex: number;
  onSelectSuggestion: (path: string) => Promise<void>;
}

export const WorkspaceSuggestionList: React.FC<WorkspaceSuggestionListProps> = ({
  workspaceRoot,
  remoteContext,
  suggestions,
  suggestionsLoading,
  suggestionsError,
  selectedIndex,
  onSelectSuggestion,
}) => {
  if (suggestionsLoading) {
    return (
      <div className="location-switcher-directory-loading">
        <Loader2 size={14} className="spin" />
        <span>Finding folders...</span>
      </div>
    );
  }

  if (suggestionsError) {
    return (
      <div className="location-switcher-directory-error">
        <span>{suggestionsError}</span>
      </div>
    );
  }

  if (suggestions.length === 0) {
    return null;
  }

  return (
    <>
      <div className="location-switcher-section-header" role="presentation">
        Suggestions
      </div>
      <div className="location-switcher-directory-list">
        {suggestions.map((dir, index) => (
          <button
            key={dir.path}
            type="button"
            className={`location-switcher-item ${
              index === selectedIndex ? 'selected' : ''
            } ${dir.path === workspaceRoot ? 'active' : ''}`}
            onClick={() => onSelectSuggestion(dir.path)}
            role="option"
            aria-selected={dir.path === workspaceRoot}
          >
            <span className="location-switcher-item-text">{dir.name}</span>
            <span className="location-switcher-item-meta">
              {remoteContext
                ? collapseHomePath(dir.path, remoteContext.homePath)
                : dir.path}
            </span>
          </button>
        ))}
      </div>
    </>
  );
};
