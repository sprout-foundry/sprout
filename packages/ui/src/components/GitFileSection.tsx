import { Plus, Minus, FileText, FilePlus, FileX2, ArrowRightLeft } from 'lucide-react';

/**
 * Get icon component for file status.
 */
export function getStatusIcon(type: 'modified' | 'untracked' | 'staged' | 'deleted' | 'renamed') {
  switch (type) {
    case 'staged':
      return <FilePlus size={14} />;
    case 'modified':
      return <FileText size={14} />;
    case 'untracked':
      return <Plus size={14} />;
    case 'deleted':
      return <FileX2 size={14} />;
    case 'renamed':
      return <ArrowRightLeft size={14} />;
  }
}

/**
 * File list section component.
 */
export function GitFileSection({
  type,
  title,
  files,
  renamedFiles,
  isStaged,
  onFileClick,
}: {
  type: 'modified' | 'untracked' | 'staged' | 'deleted' | 'renamed';
  title: string;
  files: string[];
  renamedFiles?: Array<{ from: string; to: string }>;
  isStaged: (path: string) => boolean;
  onFileClick: (path: string) => void;
}) {
  if (files.length === 0 && (!renamedFiles || renamedFiles.length === 0)) return null;

  return (
    <div className="gitpanel-section">
      <div className="gitpanel-section-title">
        {getStatusIcon(type)}
        {title}
      </div>
      {files.map((file) => (
        <div
          key={file}
          className={`gitpanel-file ${isStaged(file) ? 'gitpanel-file-staged' : ''}`}
          onClick={() => onFileClick(file)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onFileClick(file);
            }
          }}
          role="button"
          tabIndex={0}
        >
          <span className="gitpanel-file-icon" aria-hidden="true">
            {isStaged(file) ? <Minus size={12} /> : <Plus size={12} />}
          </span>
          <span className="gitpanel-file-name">{file}</span>
        </div>
      ))}
      {renamedFiles?.map((item, idx) => (
        <div
          key={idx}
          className={`gitpanel-file ${isStaged(item.to) ? 'gitpanel-file-staged' : ''}`}
          onClick={() => onFileClick(item.to)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onFileClick(item.to);
            }
          }}
          role="button"
          tabIndex={0}
        >
          <span className="gitpanel-file-icon" aria-hidden="true">
            {isStaged(item.to) ? <Minus size={12} /> : <Plus size={12} />}
          </span>
          <span className="gitpanel-file-name">
            {item.from} → {item.to}
          </span>
        </div>
      ))}
    </div>
  );
}
