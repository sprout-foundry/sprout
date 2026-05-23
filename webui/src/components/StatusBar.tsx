// Thin shell: wraps @sprout/ui StatusBar with local webui-specific prop computation
import { StatusBar as SproutStatusBar, detectLineEnding } from '@sprout/ui';
import { useMemo, useRef } from 'react';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';
import { ChatStatusBarItems } from './chat/ChatStatusBarItems';
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
  /**
   * SP-053-3b: chat stats blob (provider/model/tokens/cost). When non-empty,
   * the right section renders ChatStatusBarItems instead of editor metadata
   * so the user always sees what they're spending while a chat is active.
   */
  chatStats?: Record<string, unknown> | null;
  /**
   * Fired when the user clicks the model name in the chat status segment.
   * Passes the active provider name so the caller can open the model
   * picker scoped to that provider. Without this, the model name renders
   * as plain text (no click affordance).
   */
  onModelClick?: (provider: string) => void;
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
  chatStats,
  onModelClick,
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

  // SP-053-3b: chat stats outrank editor metadata in the right section.
  // When a chat is active and has any stats payload, render the chat
  // segments (provider · model · ctx · cost); otherwise fall through to
  // the shared StatusBar's editor metadata defaults.
  const hasChatStats = chatStats != null && Object.keys(chatStats).length > 0;
  const chatRightItems = hasChatStats ? (
    <ChatStatusBarItems stats={chatStats} onModelClick={onModelClick} />
  ) : undefined;

  return (
    <div className="statusbar-wrapper">
      <SproutStatusBar
        branch={branch}
        cursorPosition={buffer?.cursorPosition}
        language={language}
        encoding={encoding}
        lineEnding={lineEnding}
        indentation={indentation}
        showRightSection={buffer != null || hasChatStats}
        rightItems={chatRightItems}
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
