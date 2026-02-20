/**
 * File List Component
 *
 * Displays a list of files with icons and click handlers
 */

import React from 'react';

interface FileListProps {
  files: Array<{ path: string; modified: boolean }>;
  onFileClick?: (filePath: string) => void;
  maxFiles?: number;
  showIcons?: boolean;
}

export const FileList: React.FC<FileListProps> = ({
  files,
  onFileClick,
  maxFiles = 20,
  showIcons = true
}) => {
  const getFileIcon = (fileName: string) => {
    if (!showIcons) return null;

    const extension = fileName.split('.').pop()?.toLowerCase() || '';
    const isDirectory = fileName.endsWith('/') || !fileName.includes('.');

    if (isDirectory) return '📁';

    const iconMap: { [key: string]: string } = {
      'js': '📜',
      'jsx': '⚛️',
      'ts': '📘',
      'tsx': '⚛️',
      'go': '🐹',
      'py': '🐍',
      'rs': '🦀',
      'java': '☕',
      'md': '📝',
      'json': '📋',
      'yaml': '⚙️',
      'yml': '⚙️',
      'txt': '📄',
      'css': '🎨',
      'html': '🌐',
      'sh': '💻',
      'mod': '📦',
      'sum': '🔒'
    };

    return iconMap[extension] || '📄';
  };

  if (files.length === 0) {
    return <span className="empty">No files</span>;
  }

  return (
    <div className="files-list">
      {files.slice(0, maxFiles).map((file, index) => {
        const fileName = file.path.split('/').pop() || file.path;
        const icon = getFileIcon(fileName);

        return (
          <div
            key={index}
            className={`file-item ${onFileClick ? 'clickable' : ''}`}
            title={file.path}
            onClick={() => onFileClick?.(file.path)}
          >
            {icon && <span className="file-icon">{icon}</span>}
            <span className={`file-path ${file.modified ? 'modified' : ''}`}>
              {fileName}
            </span>
            {file.modified && <span className="badge">✓</span>}
          </div>
        );
      })}
    </div>
  );
};
