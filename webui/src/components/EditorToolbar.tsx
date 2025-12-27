import React, { useState } from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useTheme } from '../contexts/ThemeContext';
import './EditorToolbar.css';

interface EditorToolbarProps {
  paneId: string;
  showLineNumbers: boolean;
  onToggleLineNumbers: () => void;
  onGoToLine: (line: number) => void;
}

const EditorToolbar: React.FC<EditorToolbarProps> = ({
  paneId,
  showLineNumbers,
  onToggleLineNumbers,
  onGoToLine
}) => {
  const { splitPane, closeSplit, activePaneId, paneLayout } = useEditorManager();
  const { theme, toggleTheme } = useTheme();
  const [showGoToLine, setShowGoToLine] = useState(false);
  const [lineInput, setLineInput] = useState('');

  const handleSplitVertical = () => {
    splitPane(paneId, 'vertical');
  };

  const handleSplitHorizontal = () => {
    splitPane(paneId, 'horizontal');
  };

  const handleCloseSplit = () => {
    closeSplit();
  };

  const handleGoToLineSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const lineNum = parseInt(lineInput, 10);
    if (!isNaN(lineNum) && lineNum > 0) {
      onGoToLine(lineNum);
      setShowGoToLine(false);
      setLineInput('');
    }
  };

  const canSplit = activePaneId === paneId && paneLayout !== 'split-grid';
  const canCloseSplit = paneLayout !== 'single';

  return (
    <div className="editor-toolbar">
      <div className="toolbar-group">
        {/* Line Numbers Toggle */}
        <button
          className={`toolbar-button ${showLineNumbers ? 'active' : ''}`}
          onClick={onToggleLineNumbers}
          title="Toggle line numbers (Ctrl+L)"
        >
          <span className="toolbar-icon">🔢</span>
        </button>

        {/* Go to Line */}
        {showGoToLine ? (
          <form className="go-to-line-form" onSubmit={handleGoToLineSubmit}>
            <input
              type="number"
              className="go-to-line-input"
              placeholder="Line #"
              value={lineInput}
              onChange={(e) => setLineInput(e.target.value)}
              autoFocus
              min="1"
            />
            <button type="button" className="go-to-line-cancel" onClick={() => { setShowGoToLine(false); setLineInput(''); }}>
              ✕
            </button>
          </form>
        ) : (
          <button
            className="toolbar-button"
            onClick={() => setShowGoToLine(true)}
            title="Go to line (Ctrl+G)"
          >
            <span className="toolbar-icon">↣</span>
          </button>
        )}
      </div>

      <div className="toolbar-group">
        {/* Split View Buttons */}
        {canSplit && (
          <>
            <button
              className="toolbar-button"
              onClick={handleSplitVertical}
              title="Split vertically"
            >
              <span className="toolbar-icon">⬌</span>
            </button>
            <button
              className="toolbar-button"
              onClick={handleSplitHorizontal}
              title="Split horizontally"
            >
              <span className="toolbar-icon">⬍</span>
            </button>
          </>
        )}

        {canCloseSplit && (
          <button
            className="toolbar-button"
            onClick={handleCloseSplit}
            title="Close split view"
          >
            <span className="toolbar-icon">✕</span>
          </button>
        )}

        {/* Theme Toggle */}
        <button
          className="toolbar-button"
          onClick={toggleTheme}
          title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
        >
          <span className="toolbar-icon">{theme === 'dark' ? '☀️' : '🌙'}</span>
        </button>
      </div>
    </div>
  );
};

export default EditorToolbar;
