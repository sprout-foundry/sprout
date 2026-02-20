/**
 * Editor View Provider
 *
 * Data-driven provider for Editor view sidebar content
 */

import React, { useState, useMemo } from 'react';
import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class EditorViewProvider implements ContentProvider {
  readonly id = 'editor-view';
  readonly viewType = 'editor';
  readonly name = 'Editor View Provider';

  getSections(context: ProviderContext): SidebarSection[] {
    return [
      {
        id: 'files',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => data.recentFiles
        },
        renderItem: (files: any[], ctx: ProviderContext) => {
          if (files.length === 0) {
            return <span className="empty">No files</span>;
          }

          return (
            <FilesListWithSearch 
              files={files} 
              onFileClick={ctx.onFileClick} 
            />
          );
        },
        title: (files: any[]) => `📁 Files (${files.length})`,
        order: 1
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'open-file':
        if (context.onFileClick && action.payload?.filePath) {
          context.onFileClick(action.payload.filePath);
          return { success: true };
        }
        return { success: false, error: 'No onFileClick handler' };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {}
}

interface FilesListWithSearchProps {
  files: any[];
  onFileClick?: (filePath: string) => void;
}

const FilesListWithSearch: React.FC<FilesListWithSearchProps> = ({ files, onFileClick }) => {
  const [searchQuery, setSearchQuery] = useState('');

  const filteredFiles = useMemo(() => {
    if (!searchQuery.trim()) return files;
    const query = searchQuery.toLowerCase();
    return files.filter((file: any) => {
      const fileName = file.path.split('/').pop() || file.path;
      return fileName.toLowerCase().includes(query) || file.path.toLowerCase().includes(query);
    });
  }, [files, searchQuery]);

  const getFileIcon = (ext: string, isDir: boolean) => {
    if (isDir) return '📁';
    const iconMap: { [key: string]: string } = {
      'js': '📜', 'jsx': '⚛️', 'ts': '📘', 'tsx': '⚛️',
      'go': '🐹', 'py': '🐍', 'rs': '🦀', 'java': '☕',
      'md': '📝', 'json': '📋', 'yaml': '⚙️', 'yml': '⚙️',
      'txt': '📄', 'css': '🎨', 'html': '🌐', 'sh': '💻',
      'mod': '📦', 'sum': '🔒'
    };
    return iconMap[ext] || '📄';
  };

  return (
    <div className="files-list-container">
      <div className="files-search">
        <input
          type="text"
          placeholder="Search files..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="files-search-input"
        />
        {searchQuery && (
          <button 
            className="files-search-clear"
            onClick={() => setSearchQuery('')}
            title="Clear search"
          >
            ✕
          </button>
        )}
      </div>
      <div className="files-list">
        {filteredFiles.length === 0 ? (
          <div className="files-search-empty">
            {searchQuery ? `No files match "${searchQuery}"` : 'No files'}
          </div>
        ) : (
          filteredFiles.slice(0, 50).map((file: any, index: number) => {
            const fileName = file.path.split('/').pop() || file.path;
            const extension = fileName.split('.').pop()?.toLowerCase() || '';
            const isDirectory = file.path.endsWith('/') || !fileName.includes('.');

            return (
              <div
                key={index}
                className="file-item clickable"
                title={file.path}
                role="button"
                tabIndex={0}
                onClick={() => onFileClick?.(file.path)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onFileClick?.(file.path);
                  }
                }}
              >
                <span className="file-icon">{getFileIcon(extension, isDirectory)}</span>
                <span className={`file-path ${file.modified ? 'modified' : ''}`}>
                  {fileName}
                </span>
                {file.modified && <span className="badge">✓</span>}
              </div>
            );
          })
        )}
        {filteredFiles.length > 50 && (
          <div className="files-search-more">
            Showing 50 of {filteredFiles.length} files
          </div>
        )}
      </div>
    </div>
  );
};
