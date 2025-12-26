import React, { useState } from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { EditorBuffer } from '../types/editor';
import './EditorTabs.css';

const EditorTabs: React.FC = () => {
  const { buffers, activeBufferId, switchPane, closeBuffer, panes } = useEditorManager();
  const [showConfirm, setShowConfirm] = useState<{ bufferId: string; fileName: string } | null>(null);

  // Convert buffers Map to array and sort by last opened (most recent first)
  const bufferList = Array.from(buffers.values())
    .sort((a, b) => {
      // Active buffer first
      if (a.id === activeBufferId) return -1;
      if (b.id === activeBufferId) return 1;
      // Otherwise by creation order (newer first)
      return b.id.localeCompare(a.id);
    });

  const handleTabClick = (buffer: EditorBuffer) => {
    const pane = panes.find(p => p.bufferId === buffer.id);
    if (pane && pane.id !== panes.find(p => p.isActive)?.id) {
      switchPane(pane.id);
    }
  };

  const handleTabClose = (e: React.MouseEvent, buffer: EditorBuffer) => {
    e.stopPropagation();
    
    if (buffer.isModified) {
      setShowConfirm({ bufferId: buffer.id, fileName: buffer.file.name });
      return;
    }
    
    closeBuffer(buffer.id);
  };

  const handleConfirmClose = () => {
    if (showConfirm) {
      closeBuffer(showConfirm.bufferId);
      setShowConfirm(null);
    }
  };

  const handleCancelClose = () => {
    setShowConfirm(null);
  };

  return (
    <div className="editor-tabs">
      <div className="tabs-container">
        {bufferList.length === 0 ? (
          <div className="no-tabs">
            <span className="no-tabs-icon">📂</span>
            <span className="no-tabs-text">No open files</span>
          </div>
        ) : (
          <div className="tabs-list">
            {bufferList.map((buffer) => (
              <div
                key={buffer.id}
                className={`tab ${buffer.id === activeBufferId ? 'active' : ''}`}
                onClick={() => handleTabClick(buffer)}
              >
                <div className="tab-content">
                  <span className="tab-icon" style={{ color: getFileIconColor(buffer.file.ext) }}>
                    {getFileIcon(buffer.file.ext)}
                  </span>
                  <span className="tab-name">{buffer.file.name}</span>
                  {buffer.isModified && <span className="tab-modified">●</span>}
                  <button
                    className="tab-close"
                    onClick={(e) => handleTabClose(e, buffer)}
                    title={`Close ${buffer.file.name}`}
                  >
                    ✕
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      <div className="tabs-actions">
        <span className="tab-count">
          {bufferList.length} file{bufferList.length !== 1 ? 's' : ''} open
        </span>
      </div>

      {/* Confirmation Dialog */}
      {showConfirm && (
        <div className="close-confirm-overlay">
          <div className="close-confirm-dialog">
            <div className="dialog-header">
              <h3>⚠️ Unsaved Changes</h3>
            </div>
            <div className="dialog-body">
              <p>You have unsaved changes in <strong>"{showConfirm.fileName}"</strong>.</p>
              <p>Are you sure you want to close the file?</p>
            </div>
            <div className="dialog-actions">
              <button onClick={handleConfirmClose} className="dialog-btn danger">
                × Yes, Close
              </button>
              <button onClick={handleCancelClose} className="dialog-btn primary">
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

const getFileIcon = (ext?: string): string => {
  if (!ext) return '📄';

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return '🟨';
    case '.ts':
    case '.tsx':
      return '🔷';
    case '.go':
      return '🐹';
    case '.py':
      return '🐍';
    case '.json':
      return '📋';
    case '.html':
      return '🌐';
    case '.css':
      return '🎨';
    case '.md':
      return '📝';
    case '.txt':
      return '📄';
    case '.yml':
    case '.yaml':
      return '⚙️';
    case '.sh':
      return '🐚';
    default:
      return '📄';
  }
};

const getFileIconColor = (ext?: string): string => {
  if (!ext) return '#9ca3af';

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return '#f7df1e';
    case '.ts':
    case '.tsx':
      return '#3178c6';
    case '.go':
      return '#00add8';
    case '.py':
      return '#3776ab';
    case '.json':
      return '#cbcb41';
    case '.html':
      return '#e34c26';
    case '.css':
      return '#264de4';
    case '.md':
      return '#083fa1';
    default:
      return '#9ca3af';
  }
};

export default EditorTabs;
