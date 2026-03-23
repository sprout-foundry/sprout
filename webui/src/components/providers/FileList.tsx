/**
 * File List Component
 *
 * Displays a list of files with icons and click handlers
 */

import React from 'react';
import {
  Folder, FileCode, Code2, Code, FileText, Braces,
  File, Palette, Globe, Terminal, Package, Lock, ScrollText, Check
} from 'lucide-react';

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

    if (isDirectory) return <Folder size={14} />;

    const iconMap: { [key: string]: React.ReactNode } = {
      'js': <FileCode size={14} />,
      'jsx': <FileCode size={14} />,
      'ts': <Code2 size={14} />,
      'tsx': <Code2 size={14} />,
      'go': <Code2 size={14} />,
      'py': <Code size={14} />,
      'rs': <FileCode size={14} />,
      'java': <Code size={14} />,
      'md': <FileText size={14} />,
      'json': <Braces size={14} />,
      'yaml': <Palette size={14} />,
      'yml': <Palette size={14} />,
      'txt': <File size={14} />,
      'css': <Palette size={14} />,
      'html': <Globe size={14} />,
      'sh': <Terminal size={14} />,
      'mod': <Package size={14} />,
      'sum': <Lock size={14} />,
      'log': <ScrollText size={14} />,
      'toml': <Braces size={14} />,
      'sql': <Code2 size={14} />,
    };

    return iconMap[extension] || <File size={14} />;
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
            {file.modified && <span className="badge"><Check size={10} /></span>}
          </div>
        );
      })}
    </div>
  );
};
