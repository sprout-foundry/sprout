import React, { useState } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import {
  Save,
  X,
  ArrowDownToLine,
  Sun,
  Moon,
  Loader2,
} from 'lucide-react';
import './EditorToolbar.css';

interface EditorToolbarProps {
  paneId: string;
  onGoToLine: (line: number) => void;
  onSave: () => void;
  saving?: boolean;
  showGoToLine?: boolean;
  showSave?: boolean;
  actions?: Array<{
    id: string;
    title: string;
    icon: React.ReactNode;
    onClick: () => void;
    active?: boolean;
    disabled?: boolean;
  }>;
}

const EditorToolbar: React.FC<EditorToolbarProps> = ({
  paneId: _paneId,
  onGoToLine,
  onSave,
  saving = false,
  showGoToLine = true,
  showSave = true,
  actions = [],
}) => {
  const { theme, themePack, toggleTheme } = useTheme();
  const [showGoToLineInput, setShowGoToLineInput] = useState(false);
  const [lineInput, setLineInput] = useState('');

  const handleGoToLineSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const lineNum = parseInt(lineInput, 10);
    if (!isNaN(lineNum) && lineNum > 0) {
      onGoToLine(lineNum);
      setShowGoToLineInput(false);
      setLineInput('');
    }
  };

  return (
    <div className="editor-toolbar">
      <div className="toolbar-group">
        {/* Go to Line */}
        {showGoToLine ? (
          showGoToLineInput ? (
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
              <button type="button" className="go-to-line-cancel" onClick={() => { setShowGoToLineInput(false); setLineInput(''); }}>
                <X size={12} />
              </button>
            </form>
          ) : (
            <button
              className="toolbar-button"
              onClick={() => setShowGoToLineInput(true)}
              title="Go to line (Ctrl+G)"
            >
              <span className="toolbar-icon"><ArrowDownToLine size={16} /></span>
            </button>
          )
        ) : null}

        {actions.map((action) => (
          <button
            key={action.id}
            className={`toolbar-button ${action.active ? 'active' : ''}`}
            onClick={action.onClick}
            title={action.title}
            disabled={action.disabled}
          >
            <span className="toolbar-icon">{action.icon}</span>
          </button>
        ))}
      </div>

      <div className="toolbar-group">
        {/* Save */}
        {showSave ? (
          <button
            className="toolbar-button"
            onClick={onSave}
            title="Save file (Ctrl+S)"
            disabled={saving}
          >
            {saving ? (
              <span className="toolbar-icon"><Loader2 size={16} className="spinner" /></span>
            ) : (
              <span className="toolbar-icon"><Save size={16} /></span>
            )}
          </button>
        ) : null}

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
