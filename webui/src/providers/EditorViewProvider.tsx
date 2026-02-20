/**
 * Editor View Provider
 *
 * Data-driven provider for Editor view sidebar content
 */

import React from 'react';
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
            <div className="files-list">
              {files.slice(0, 20).map((file: any, index: number) => {
                const fileName = file.path.split('/').pop() || file.path;
                const extension = fileName.split('.').pop()?.toLowerCase() || '';
                const isDirectory = file.path.endsWith('/') || !fileName.includes('.');

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
                  <div
                    key={index}
                    className="file-item clickable"
                    title={file.path}
                    role="button"
                    tabIndex={0}
                    onClick={() => ctx.onFileClick?.(file.path)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        ctx.onFileClick?.(file.path);
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
              })}
            </div>
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
