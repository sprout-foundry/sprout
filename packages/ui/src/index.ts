// @sprout/ui — Reusable React components for Sprout IDE

// ── Types ────────────────────────────────────────────────────────────
export type { NotificationType } from './services/notificationBus';
export type { NotificationData } from './types/notification';
export type { CursorPosition, StatusBarProps } from './components/StatusBar';
export type { ContextMenuProps } from './components/ContextMenu';
export type { FileInfo } from './types/file-tree';
export type { FileTreeHandle, FileTreeProps } from './components/FileTree';
export type {
  GitStatusData,
  GitFile,
  FileSection,
  GitCommitSummary,
  GitCommitDetail,
  GitCommitFileEntry,
} from './types/git-types';
export type { GitSidebarPanelProps, GitBranchesState } from './components/GitPanel';
export type { CommandPaletteProps, PaletteMode, CommandDef, FileResult, SymbolResult } from './components/CommandPalette';
export type { TerminalProps, ShellInfo } from './components/Terminal';
export type { TerminalThemePack, CreateTerminalConnection } from './components/TerminalPane';

export type { EditorProps, EditorTheme, CursorPosition as EditorCursorPosition } from './components/Editor';
export type { SidebarProps } from './components/Sidebar';

export type { APIAdapter, PlatformNavItem } from './types/adapter';

// ── Git types and utilities ───────────────────────────────────────────
export { FILE_SECTIONS, selectionKey, parseSelectionKey } from './types/git-types';
export { MAX_FILES_PER_SECTION, MAX_FILES_INITIAL, LOAD_MORE_INCREMENT } from './constants/git-constants';

// ── Contexts ──────────────────────────────────────────────────────────
export { NotificationProvider, useNotifications } from './contexts/NotificationContext';
export { SproutProvider, useSproutAdapter } from './contexts/SproutAdapterContext';
export type { SproutProviderProps } from './contexts/SproutAdapterContext';

// ── Hooks ─────────────────────────────────────────────────────────────
export { useMultiSelect, flattenVisibleFiles } from './hooks/useMultiSelect';
export type { MultiSelectState, MultiSelectActions, VisibleEntry } from './hooks/useMultiSelect';

// ── Components ────────────────────────────────────────────────────────
export { default as ContextMenu } from './components/ContextMenu';
export { default as NotificationStack } from './components/NotificationStack';
export { default as NotificationItem } from './components/NotificationItem';
export { default as StatusBar } from './components/StatusBar';
export { default as FileTree } from './components/FileTree';
export { default as SelectionActionBar } from './components/SelectionActionBar';
export { default as GitSidebarPanel } from './components/GitPanel';
export { default as ChatPanel } from './components/ChatPanel';
export { default as CommandInput } from './components/CommandInput';
export { default as LiveLog } from './components/LiveLog';
export { default as MessageBubble } from './components/MessageBubble';
export { default as MessageContent } from './components/MessageContent';
export { default as MessageSegments } from './components/MessageSegments';
export { default as ChatMessageContextMenu } from './components/ChatMessageContextMenu';
export { default as QueuedMessagesPanel } from './components/QueuedMessagesPanel';
export { default as Terminal } from './components/Terminal';
export { default as TerminalPane } from './components/TerminalPane';
export { default as TerminalTabBar } from './components/TerminalTabBar';
export { default as Sidebar } from './components/Sidebar';
export { default as CommandPalette } from './components/CommandPalette';
export { default as Editor } from './components/Editor';

// ── Dialogs ───────────────────────────────────────────────────────────
export { showThemedAlert, showThemedConfirm, showThemedPrompt } from './components/ThemedDialog';

// ── Utilities ─────────────────────────────────────────────────────────
export { generateUUID } from './utils/uuid';
export { detectLineEnding } from './utils/lineEndingDetect';
export { copyToClipboard } from './utils/clipboard';
export { fuzzyScore, fuzzyFilter, highlightMatches } from './utils/fuzzyMatch';
export { debugLog } from './utils/log';

// ── Services ──────────────────────────────────────────────────────────
export { notificationBus } from './services/notificationBus';
