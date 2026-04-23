import './StatusBar.css';

import { useMemo } from 'react';
import { GitBranch } from 'lucide-react';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';
import { detectLineEnding } from '../extensions/lineEndingDetect';

interface StatusBarProps {
  branch?: string;
  buffer?: {
    kind: string;
    file?: { name: string; ext?: string };
    content?: string;
    cursorPosition?: { line: number; column: number };
    languageOverride?: string | null;
  } | null;
  encoding?: string;
  indentation?: string;
}

function StatusBar({ branch, buffer, encoding, indentation }: StatusBarProps): JSX.Element {
  const showRightSection = buffer != null;
  const cursorPosition = buffer?.cursorPosition;
  const hasCursor =
    cursorPosition != null &&
    typeof cursorPosition.line === 'number' &&
    typeof cursorPosition.column === 'number';

  // Language name
  const languageName = useMemo(() => {
    let name = 'Plain Text';
    if (buffer) {
      if (buffer.kind === 'file' && buffer.file) {
        const { languageId } = resolveLanguageId(
          buffer.languageOverride,
          buffer.file.ext?.replace(/^\./, ''),
          buffer.file.name,
        );
        if (languageId) {
          const entry = allLanguageEntries.find((e) => e.id === languageId);
          if (entry) {
            name = entry.name;
          }
        }
      } else {
        name = buffer.kind.charAt(0).toUpperCase() + buffer.kind.slice(1);
      }
    }
    return name;
  }, [buffer]);

  // Line endings detection using shared utility (consistent with editor footer indicator).
  const lineEnding = useMemo(() => {
    const result = detectLineEnding(buffer?.content || '');
    return result.lineEnding;
  }, [buffer?.content]);

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
      {showRightSection && (
        <div className="statusbar-right">
          {/* Cursor position — aria-hidden to prevent screen reader spam on every keystroke */}
          {hasCursor && (
            <span className="statusbar-item statusbar-item-cursor" title="Cursor position" aria-hidden="true">
              Ln {cursorPosition.line + 1}, Col {cursorPosition.column + 1}
            </span>
          )}

          {/* Language */}
          <span className="statusbar-item statusbar-item-language" title={`Language: ${languageName}`}>
            {languageName}
          </span>

          {/* Encoding */}
          <span className="statusbar-item statusbar-item-encoding" title="File encoding">
            {encoding || 'UTF-8'}
          </span>

          {/* Line endings */}
          <span className="statusbar-item statusbar-item-line-ending" title="Line ending format">
            {lineEnding}
          </span>

          {/* Indentation */}
          <span className="statusbar-item statusbar-item-indentation" title="Indentation">
            {indentation || 'Spaces: 2'}
          </span>
        </div>
      )}
    </footer>
  );
}

export default StatusBar;
