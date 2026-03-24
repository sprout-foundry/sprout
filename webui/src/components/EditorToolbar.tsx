import React, { useState } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import {
  Hash,
  Save,
  X,
  ArrowDownToLine,
  Sun,
  Moon,
} from 'lucide-react';
import './EditorToolbar.css';

interface EditorToolbarProps {
  paneId: string;
  showLineNumbers: boolean;
  onToggleLineNumbers: () => void;
  onGoToLine: (line: number) => void;
  onSave: () => void;
}

const EditorToolbar: React.FC<EditorToolbarProps> = ({
  paneId: _paneId,
  showLineNumbers,
  onToggleLineNumbers,
  onGoToLine,
  onSave
}) => {
  const { theme, themePack, toggleTheme } = useTheme();
  const [showGoToLine, setShowGoToLine] = useState(false);
  const [lineInput, setLineInput] = useState('');

  const handleGoToLineSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const lineNum = parseInt(lineInput, 10);
    if (!isNaN(lineNum) && lineNum > 0) {
      onGoToLine(lineNum);
      setShowGoToLine(false);
      setLineInput('');
    }
  };

  return (
    <div className="editor-toolbar">
      <div className="toolbar-group">
        {/* Line Numbers Toggle */}
        <button
          className={`toolbar-button ${showLineNumbers ? 'active' : ''}`}
          onClick={onToggleLineNumbers}
          title="Toggle line numbers (Ctrl+L)"
        >
          <span className="toolbar-icon"><Hash size={16} /></span>
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
              <X size={12} />
            </button>
          </form>
        ) : (
          <button
            className="toolbar-button"
            onClick={() => setShowGoToLine(true)}
            title="Go to line (Ctrl+G)"
          >
            <span className="toolbar-icon"><ArrowDownToLine size={16} /></span>
          </button>
        )}
      </div>

      <div className="toolbar-group">
        {/* Save */}
        <button
          className="toolbar-button"
          onClick={onSave}
          title="Save file (Ctrl+S)"
        >
          <span className="toolbar-icon"><Save size={16} /></span>
        </button>

        {/* Theme Toggle */}
        <button
          className="toolbar-button"
          onClick={toggleTheme}
          title={`Switch mode from ${themePack.name} to ${theme === 'dark' ? 'light' : 'dark'}`}
        >
          <span className="toolbar-icon">{theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}</span>
        </button>
      </div>
    </div>
  );
};

export default EditorToolbar;
