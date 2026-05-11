// Thin shell: wraps @sprout/ui StatusBar with local webui-specific prop computation
import { useMemo, useRef } from 'react';
import { StatusBar as SproutStatusBar, detectLineEnding } from '@sprout/ui';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';
import './StatusBar.css';

interface StatusBarBufferInfo {
  kind: string;
  file?: { name: string; ext?: string };
  content?: string;
  cursorPosition?: { line: number; column: number };
  languageOverride?: string | null;
}

interface WebuiStatusBarProps {
  branch?: string;
  buffer?: StatusBarBufferInfo | null;
  encoding?: string;
  indentation?: string;
  notificationCount?: number;
  onToggleNotificationCenter?: () => void;
  notificationCenterRef?: React.RefObject<HTMLDivElement>;
}

// Simple SVG icon for notification bell (similar to existing inline SVGs)
function BellIcon(): JSX.Element {
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
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  );
}

/**
 * Webui-specific StatusBar that derives language name and line ending
 * from the buffer prop, then delegates rendering to @sprout/ui StatusBar.
 * Also adds a notification bell icon with badge count.
 */
function StatusBar({
  branch,
  buffer,
  encoding,
  indentation,
  notificationCount = 0,
  onToggleNotificationCenter,
  notificationCenterRef,
}: WebuiStatusBarProps): JSX.Element {
  // Language name — derived from buffer metadata using local language registry
  const language = useMemo(() => {
    if (!buffer) return undefined;
    if (buffer.kind === 'file' && buffer.file) {
      const { languageId } = resolveLanguageId(
        buffer.languageOverride,
        buffer.file.ext?.replace(/^\./, ''),
        buffer.file.name,
      );
      if (languageId) {
        const entry = allLanguageEntries.find((e) => e.id === languageId);
        if (entry) return entry.name;
      }
    }
    return buffer.kind.charAt(0).toUpperCase() + buffer.kind.slice(1);
  }, [buffer]);

  // Line ending — detected from buffer content.
  // Depends on `buffer?.file` reference (stable across keystrokes, changes on file
  // switch or external reload) rather than `buffer?.content` (changes every keystroke).
  const lineEnding = useMemo(() => {
    const result = detectLineEnding(buffer?.content || '');
    return result.lineEnding;
  }, [buffer?.file, buffer?.kind]);

  const bellIconRef = useRef<HTMLDivElement>(null);

  return (
    <div className="statusbar-wrapper">
      <SproutStatusBar
        branch={branch}
        cursorPosition={buffer?.cursorPosition}
        language={language}
        encoding={encoding}
        lineEnding={lineEnding}
        indentation={indentation}
        showRightSection={buffer != null}
      />
      {onToggleNotificationCenter && (
        <div
          ref={notificationCenterRef ?? bellIconRef}
          className="statusbar-item statusbar-item-notification"
          onClick={onToggleNotificationCenter}
          role="button"
          tabIndex={0}
          aria-label={`Notifications${notificationCount > 0 ? ` (${notificationCount})` : ''}`}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onToggleNotificationCenter();
            }
          }}
        >
          <BellIcon />
          {notificationCount > 0 && (
            <span className="statusbar-notification-badge">{notificationCount > 99 ? '99+' : notificationCount}</span>
          )}
        </div>
      )}
    </div>
  );
}

export default StatusBar;
