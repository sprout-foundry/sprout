import type { ReactNode } from 'react';
import './StatusBar.css';

export interface CursorPosition {
  line: number;
  column: number;
}

export interface StatusBarProps {
  /** Git branch name */
  branch?: string;
  /** Current cursor position in the editor */
  cursorPosition?: CursorPosition;
  /** Programming language name */
  language?: string;
  /** File encoding */
  encoding?: string;
  /** Line ending format */
  lineEnding?: string;
  /** Indentation style */
  indentation?: string;
  /** Custom left section items */
  leftItems?: ReactNode;
  /** Custom right section items */
  rightItems?: ReactNode;
  /** Additional CSS class name */
  className?: string;
  /** Whether to show the right section */
  showRightSection?: boolean;
}

/**
 * A status bar component that displays editor and git information.
 *
 * Can be used in two modes:
 * 1. Default: Shows git branch on left and editor info on right
 * 2. Custom: Pass leftItems and/or rightItems for full control
 */
function StatusBar({
  branch,
  cursorPosition,
  language,
  encoding,
  lineEnding,
  indentation,
  leftItems,
  rightItems,
  className,
  showRightSection = true,
}: StatusBarProps): JSX.Element {
  const hasCursor =
    cursorPosition != null &&
    typeof cursorPosition.line === 'number' &&
    typeof cursorPosition.column === 'number';

  const containerClassName = className ? `statusbar ${className}` : 'statusbar';

  return (
    <footer className={containerClassName} aria-label="Editor status bar">
      {/* Left section */}
      <div className="statusbar-left">
        {leftItems || (
          <span className="statusbar-item statusbar-item-git" title={`Branch: ${branch || 'unknown'}`}>
            <GitBranchIcon />
            <span className="statusbar-text">{branch || 'No Git'}</span>
          </span>
        )}
      </div>

      {/* Right section */}
      {showRightSection && (rightItems || cursorPosition || language || encoding || lineEnding || indentation) && (
        <div className="statusbar-right">
          {rightItems || (
            <>
              {/* Cursor position — aria-hidden to prevent screen reader spam on every keystroke */}
              {hasCursor && (
                <span className="statusbar-item statusbar-item-cursor" title="Cursor position" aria-hidden="true">
                  Ln {cursorPosition!.line + 1}, Col {cursorPosition!.column + 1}
                </span>
              )}

              {/* Language */}
              {language && (
                <span className="statusbar-item statusbar-item-language" title={`Language: ${language}`}>
                  {language}
                </span>
              )}

              {/* Encoding */}
              <span className="statusbar-item statusbar-item-encoding" title="File encoding">
                {encoding || 'UTF-8'}
              </span>

              {/* Line endings */}
              <span className="statusbar-item statusbar-item-line-ending" title="Line ending format">
                {lineEnding || 'LF'}
              </span>

              {/* Indentation */}
              <span className="statusbar-item statusbar-item-indentation" title="Indentation">
                {indentation || 'Spaces: 2'}
              </span>
            </>
          )}
        </div>
      )}
    </footer>
  );
}

// Simple SVG icon for Git branch (replaces lucide-react dependency)
function GitBranchIcon(): JSX.Element {
  return (
    <svg
      width={12}
      height={12}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <line x1="6" y1="3" x2="6" y2="15" />
      <circle cx="18" cy="6" r="3" />
      <circle cx="6" cy="18" r="3" />
      <path d="M18 9a9 9 0 0 1-9 9" />
    </svg>
  );
}

export default StatusBar;
