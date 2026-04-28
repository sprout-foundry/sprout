import './StatusBar.css';

import { useMemo } from 'react';
import { GitBranch } from 'lucide-react';
import { detectLineEnding } from '../utils/lineEndingDetect';

export interface StatusBarProps {
  branch?: string;
  cursorPosition?: { line: number; column: number } | null;
  languageName?: string;
  content?: string;
  encoding?: string;
  lineEnding?: string;
  indentation?: string;
}

function StatusBar({
  branch,
  cursorPosition,
  languageName,
  content,
  encoding,
  lineEnding: lineEndingProp,
  indentation,
}: StatusBarProps): JSX.Element {
  const hasCursor =
    cursorPosition != null &&
    typeof cursorPosition.line === 'number' &&
    typeof cursorPosition.column === 'number';

  // Default language name
  const displayLanguageName = languageName || 'Plain Text';

  // Line endings detection using shared utility (consistent with editor footer indicator).
  // Use prop if provided, otherwise compute from content.
  const displayLineEnding = useMemo(() => {
    if (lineEndingProp !== undefined) {
      return lineEndingProp;
    }
    const result = detectLineEnding(content || '');
    return result.lineEnding;
  }, [content, lineEndingProp]);

  return (
    <footer className="statusbar" aria-label="Editor status bar">
      {/* Left section: Git branch */}
      <div className="statusbar-left">
        <span className="statusbar-item statusbar-item-git" title={`Branch: ${branch || 'unknown'}`}>
          <GitBranch size={12} />
          <span className="statusbar-text">
            {branch || 'No Git'}
          </span>
        </span>
      </div>

      {/* Right section: Buffer info */}
      <div className="statusbar-right">
        {/* Cursor position — aria-hidden to prevent screen reader spam on every keystroke */}
        {hasCursor && (
          <span className="statusbar-item statusbar-item-cursor" title="Cursor position" aria-hidden="true">
            Ln {cursorPosition.line + 1}, Col {cursorPosition.column + 1}
          </span>
        )}

        {/* Language */}
        {languageName && (
          <span className="statusbar-item statusbar-item-language" title={`Language: ${displayLanguageName}`}>
            {displayLanguageName}
          </span>
        )}

        {/* Encoding */}
        {encoding && (
          <span className="statusbar-item statusbar-item-encoding" title="File encoding">
            {encoding}
          </span>
        )}

        {/* Line endings */}
        <span className="statusbar-item statusbar-item-line-ending" title="Line ending format">
          {displayLineEnding}
        </span>

        {/* Indentation */}
        <span className="statusbar-item statusbar-item-indentation" title="Indentation">
          {indentation || 'Spaces: 2'}
        </span>
      </div>
    </footer>
  );
}

export default StatusBar;
