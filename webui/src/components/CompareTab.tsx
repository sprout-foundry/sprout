import { useEffect, useState } from 'react';
import { MergeViewWrapper } from './MergeViewWrapper';
import './CompareTab.css';

interface CompareTabProps {
  fileName: string;
  originalContent: string;
  modifiedContent: string;
  aLabel?: string;
  bLabel?: string;
  title?: string;
}

function CompareTab({
  fileName,
  originalContent,
  modifiedContent,
  aLabel = 'Before',
  bLabel = 'After',
  title = 'Compare',
}: CompareTabProps): JSX.Element {
  const [viewMode, setViewMode] = useState<'side-by-side' | 'unified'>('side-by-side');
  // Lift pane-B merge edits (reverts) into state so they survive mode
  // toggles and re-renders instead of living only inside CodeMirror.
  const [editedModified, setEditedModified] = useState<string | null>(null);

  // Reset edits when the parent supplies different content (new compare).
  useEffect(() => {
    setEditedModified(null);
  }, [modifiedContent]);

  const effectiveModified = editedModified ?? modifiedContent;

  return (
    <div className="workspace-tab compare-tab">
      <div className="workspace-tab-header">
        <div>
          <div className="workspace-tab-eyebrow">{title}</div>
          <h2>{fileName}</h2>
        </div>
        <div className="compare-tab-mode-tabs">
          <button
            className={`workspace-diff-mode-tab ${viewMode === 'side-by-side' ? 'active' : ''}`}
            onClick={() => setViewMode('side-by-side')}
          >
            Side by Side
          </button>
          <button
            className={`workspace-diff-mode-tab ${viewMode === 'unified' ? 'active' : ''}`}
            onClick={() => setViewMode('unified')}
          >
            Unified
          </button>
        </div>
      </div>
      <div className="compare-tab-content">
        <MergeViewWrapper
          originalContent={originalContent}
          modifiedContent={effectiveModified}
          mode={viewMode}
          fileName={fileName}
          aLabel={aLabel}
          bLabel={bLabel}
          onModifiedChange={setEditedModified}
        />
      </div>
    </div>
  );
}

export default CompareTab;
