import React, { useMemo, useCallback } from 'react';
import { ChevronRight } from 'lucide-react';
import './EditorBreadcrumb.css';

interface EditorBreadcrumbProps {
  filePath: string;
  onNavigate?: (path: string) => void;
}

const EditorBreadcrumb: React.FC<EditorBreadcrumbProps> = ({
  filePath,
  onNavigate,
}) => {
  const segments = useMemo(() => {
    // Don't show breadcrumbs for virtual workspace paths
    if (filePath.startsWith('__workspace/')) return null;
    // Don't show breadcrumbs for empty or plain filenames without directory parts
    if (!filePath || !filePath.includes('/')) return null;

    const parts = filePath.split('/').filter(Boolean);
    if (parts.length < 2) return null;
    return parts;
  }, [filePath]);

  const handleClick = useCallback((index: number) => {
    if (!segments || !onNavigate || index === segments.length - 1) return;
    const path = segments.slice(0, index + 1).join('/');
    onNavigate(path);
  }, [segments, onNavigate]);

  // Allow keyboard activation (Enter/Space) on breadcrumb buttons
  const handleKeyDown = useCallback((e: React.KeyboardEvent, index: number) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleClick(index);
    }
  }, [handleClick]);

  if (!segments) return null;

  return (
    <nav className="editor-breadcrumb" aria-label="Breadcrumb">
      <ol className="breadcrumb-list">
        {segments.map((segment, index) => {
          const isCurrent = index === segments.length - 1;
          const path = segments.slice(0, index + 1).join('/');
          return (
            <li key={index} className="breadcrumb-item">
              {index > 0 && (
                <span className="breadcrumb-separator" aria-hidden="true">
                  <ChevronRight size={12} />
                </span>
              )}
              {isCurrent ? (
                <span className="breadcrumb-segment breadcrumb-segment-current" aria-current="page">
                  {segment}
                </span>
              ) : (
                <button
                  className="breadcrumb-segment"
                  onClick={() => handleClick(index)}
                  onKeyDown={(e) => handleKeyDown(e, index)}
                  title={path}
                  type="button"
                >
                  {segment}
                </button>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
};

export default EditorBreadcrumb;
