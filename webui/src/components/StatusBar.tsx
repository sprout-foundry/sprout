// Thin shell: wraps @sprout/ui StatusBar with local webui-specific prop computation
import { StatusBar as SproutStatusBar, detectLineEnding } from '@sprout/ui';
import { FolderOpen } from 'lucide-react';
import { useMemo, useRef, useState, useCallback } from 'react';
import { supportsGit } from '../config/mode';
import { useNotifications } from '../contexts/NotificationContext';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';
import { ChatStatusBarItems } from './chat/ChatStatusBarItems';
import NotificationCenter from './NotificationCenter';
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
  /**
   * SP-022-W2.3: Full workspace directory path. When provided, the
   * workspace basename is shown as a clickable indicator on the left
   * side of the status bar (before the SproutStatusBar content).
   */
  workspacePath?: string;
  /**
   * SP-022-W2.3: Callback fired when the workspace indicator is clicked.
   * Typically used to open a workspace picker or focus the sidebar.
   */
  onWorkspaceClick?: () => void;
  /**
   * SP-053-3b: chat stats blob (provider/model/tokens/cost). When non-empty,
   * the right section renders ChatStatusBarItems instead of editor metadata
   * so the user always sees what they're spending while a chat is active.
   */
  chatStats?: Record<string, unknown> | null;
  /**
   * WebSocket connection state — forwarded to ChatStatusBarItems so the
   * status bar shows a "disconnected" pill when events have stopped
   * flowing. Without this the user has no feedback about the drop.
   */
  isConnected?: boolean;
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
  workspacePath,
  onWorkspaceClick,
  chatStats,
  isConnected,
  onModelClick,
}: WebuiStatusBarProps): JSX.Element {
  // Notification context — derive unread count for the bell badge
  const { notifications } = useNotifications();
  const unreadCount = useMemo(() => notifications.filter((n) => !n.read).length, [notifications]);

  // Internal notification panel state
  const [isNotificationCenterOpen, setIsNotificationCenterOpen] = useState(false);
  const bellIconRef = useRef<HTMLDivElement>(null);

  const toggleNotificationCenter = useCallback(() => {
    setIsNotificationCenterOpen((prev) => !prev);
  }, []);

  const closeNotificationCenter = useCallback(() => {
    setIsNotificationCenterOpen(false);
  }, []);

  // SP-022-W2.3: derive workspace basename from the full path
  const workspaceName = useMemo(() => {
    if (!workspacePath || workspacePath.trim() === '') return '';
    // Handle trailing slashes and extract last non-empty segment
    const trimmed = workspacePath.replace(/\/+$/, '');
    const segments = trimmed.split('/');
    return segments[segments.length - 1] || '';
  }, [workspacePath]);

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

  // SP-053-3b: chat stats outrank editor metadata in the right section.
  // When a chat is active and has any stats payload, render the chat
  // segments (provider · model · ctx · cost); otherwise fall through to
  // the shared StatusBar's editor metadata defaults.
  // Render the chat status segment whenever stats are non-empty OR the
  // user is explicitly disconnected — the disconnected pill is the only
  // feedback that events have stopped flowing, so we render it even
  // when there's no other chat metadata to show.
  const hasChatStats = chatStats != null && Object.keys(chatStats).length > 0;
  const showChatSegment = hasChatStats || isConnected === false;
  const chatRightItems = showChatSegment ? (
    <ChatStatusBarItems stats={chatStats} isConnected={isConnected} onModelClick={onModelClick} />
  ) : undefined;

  return (
    <div className="statusbar-wrapper" data-testid="status-bar">
      {workspaceName && (
        <div
          className="statusbar-item statusbar-item-workspace"
          onClick={onWorkspaceClick}
          role="button"
          tabIndex={0}
          title={`Workspace: ${workspacePath}`}
          aria-label={`Workspace: ${workspaceName}`}
          data-testid="status-bar-workspace"
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onWorkspaceClick?.();
            }
          }}
        >
          <FolderOpen size={12} />
          <span className="statusbar-text">{workspaceName}</span>
        </div>
      )}
      <SproutStatusBar
        branch={supportsGit ? branch : 'Browser IDE'}
        cursorPosition={buffer?.cursorPosition}
        language={language}
        encoding={encoding}
        lineEnding={lineEnding}
        indentation={indentation}
        showRightSection={buffer != null || showChatSegment}
        rightItems={chatRightItems}
      />
      <div
        ref={bellIconRef}
        className="statusbar-item statusbar-item-notification"
        onClick={toggleNotificationCenter}
        role="button"
        tabIndex={0}
        aria-label={`Notifications${unreadCount > 0 ? ` (${unreadCount})` : ''}`}
        data-testid="status-bar-notification"
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            toggleNotificationCenter();
          }
        }}
      >
        <BellIcon />
        {unreadCount > 0 && (
          <span className="statusbar-notification-badge">{unreadCount > 99 ? '99+' : unreadCount}</span>
        )}
      </div>
      <NotificationCenter
        isOpen={isNotificationCenterOpen}
        onClose={closeNotificationCenter}
        positionRef={bellIconRef}
      />
    </div>
  );
}

export default StatusBar;
