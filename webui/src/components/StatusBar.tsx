// Thin shell: wraps @sprout/ui StatusBar with local webui-specific prop computation
import { StatusBar as SproutStatusBar, detectLineEnding } from '@sprout/ui';
import { FolderOpen, Zap } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { supportsGit, isCloud } from '../config/mode';
import { getBootstrapConfig } from '../bootstrapAdapter';
import { useActiveRepoURL } from '../services/activeRepo';
import { githubRepoSlug } from '../utils/platformUrl';
import { useNotifications } from '../contexts/NotificationContext';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';
import NotificationHistoryPanel from './NotificationHistoryPanel';
import { OPEN_NOTIFICATIONS_EVENT } from '../config/layout';
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
}: WebuiStatusBarProps): JSX.Element {
  // Notification context — derive unread count for the bell badge
  const { notifications } = useNotifications();
  const unreadCount = useMemo(() => notifications.filter((n) => !n.read).length, [notifications]);

  // Internal notification panel state
  const [isNotificationCenterOpen, setIsNotificationCenterOpen] = useState(false);
  // The layered layout's rail opens the same notification history.
  const railAnchorRef = useRef<HTMLElement | null>(null);
  const [anchoredToRail, setAnchoredToRail] = useState(false);
  useEffect(() => {
    const open = (e: Event) => {
      railAnchorRef.current = (e as CustomEvent<{ anchor?: HTMLElement }>).detail?.anchor ?? null;
      setAnchoredToRail(!!railAnchorRef.current);
      setIsNotificationCenterOpen(true);
    };
    window.addEventListener(OPEN_NOTIFICATIONS_EVENT, open);
    return () => window.removeEventListener(OPEN_NOTIFICATIONS_EVENT, open);
  }, []);
  const bellIconRef = useRef<HTMLButtonElement>(null);

  const toggleNotificationCenter = useCallback(() => {
    setAnchoredToRail(false);
    setIsNotificationCenterOpen((prev) => !prev);
  }, []);

  const closeNotificationCenter = useCallback(() => {
    setIsNotificationCenterOpen(false);
  }, []);

  // The hosted editor's workspace root is a fixed virtual folder, so the
  // open repository names it instead.
  const repoSlug = githubRepoSlug(useActiveRepoURL());
  const workspaceLabel = isCloud ? repoSlug : workspacePath;

  // SP-022-W2.3: derive workspace basename from the full path
  const workspaceName = useMemo(() => {
    if (isCloud) return repoSlug?.split('/')[1] ?? '';
    if (!workspacePath || workspacePath.trim() === '') return '';
    // Handle trailing slashes and extract last non-empty segment
    const trimmed = workspacePath.replace(/\/+$/, '');
    const segments = trimmed.split('/');
    return segments[segments.length - 1] || '';
  }, [workspacePath, repoSlug]);

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

  return (
    <div className="statusbar-wrapper" data-testid="status-bar">
      {workspaceName && (
        <div
          className="statusbar-item statusbar-item-workspace"
          onClick={onWorkspaceClick}
          role="button"
          tabIndex={0}
          title={isCloud ? `Repository: ${workspaceLabel}` : `Workspace: ${workspacePath}`}
          aria-label={isCloud ? `Repository: ${workspaceName}` : `Workspace: ${workspaceName}`}
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
      {isCloud &&
        (() => {
          const cfg = getBootstrapConfig();
          if (['pro', 'team', 'runner'].includes(cfg.user?.tier ?? '')) return null;
          return (
            <div
              className="statusbar-item statusbar-item-auto-tier"
              title="Auto mode — shared compute. Limited daily requests."
              data-testid="status-bar-auto-tier"
            >
              <Zap size={12} />
              <span className="statusbar-text">Auto</span>
            </div>
          );
        })()}
      <SproutStatusBar
        branch={supportsGit ? branch || (isCloud ? 'No repository' : undefined) : 'Browser IDE'}
        cursorPosition={buffer?.cursorPosition}
        language={language}
        encoding={encoding}
        lineEnding={lineEnding}
        indentation={indentation}
        showRightSection={buffer != null}
      />
      <button
        type="button"
        ref={bellIconRef}
        className="statusbar-item statusbar-item-notification"
        onClick={toggleNotificationCenter}
        aria-label={`Notifications${unreadCount > 0 ? ` (${unreadCount} unread)` : ''}`}
        aria-haspopup="dialog"
        aria-expanded={isNotificationCenterOpen}
        data-testid="status-bar-notification"
      >
        <BellIcon />
        {unreadCount > 0 && (
          <span className="statusbar-notification-badge">{unreadCount > 99 ? '99+' : unreadCount}</span>
        )}
      </button>
      {isNotificationCenterOpen && (
        <NotificationHistoryPanel
          anchorRef={anchoredToRail ? (railAnchorRef as React.RefObject<HTMLElement>) : bellIconRef}
          onClose={closeNotificationCenter}
        />
      )}
    </div>
  );
}

export default StatusBar;
