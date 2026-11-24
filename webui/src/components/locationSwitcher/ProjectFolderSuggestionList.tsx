import { Loader2 } from 'lucide-react';
import React from 'react';
import { collapseHomePath } from './pathUtils';
import type { ProjectFolderDirectory, RemoteProjectFolderContext } from './types';

export interface ProjectFolderSuggestionListProps {
  projectFolder: string;
  remoteContext: RemoteProjectFolderContext | null;
  suggestions: ProjectFolderDirectory[];
  suggestionsLoading: boolean;
  suggestionsError: string | null;
  selectedIndex: number;
  onSelectSuggestion: (path: string) => Promise<void>;
}

export const ProjectFolderSuggestionList: React.FC<ProjectFolderSuggestionListProps> = ({
  projectFolder: projectFolder,
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
            } ${dir.path === projectFolder ? 'active' : ''}`}
            onClick={() => onSelectSuggestion(dir.path)}
            role="option"
            aria-selected={dir.path === projectFolder}
          >
            <span className="location-switcher-item-text">{dir.name}</span>
            <span className="location-switcher-item-meta">
              {remoteContext ? collapseHomePath(dir.path, remoteContext.homePath) : dir.path}
            </span>
          </button>
        ))}
      </div>
    </>
  );
};
