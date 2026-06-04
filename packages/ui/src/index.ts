// @sprout/ui — Reusable React components for Sprout IDE

// ── Chat / shared data types ──────────────────────────────────────────
export type {
  Message,
  ToolExecution,
  SubagentActivity,
  LogEntry,
  TodoStatus,
  TodoPriority,
  TodoItem,
  FileEdit,
  LiveLogLine,
  SubagentRun,
  ToolRef,
  ChatProps,
} from './types/chat';

export { MAX_ACTIVE_LINES, MAX_COMPLETED_SUMMARIES } from './types/chat';

// ── Message Segment Types ─────────────────────────────────────────────
export type {
  TextSegment,
  ToolCallSegment,
  TodoUpdateSegment,
  ProgressSegment,
  ResultSegment,
  MessageSegment,
} from './types/message-segments';

export { parseMessageSegments } from './utils/messageSegments';

// ── Types ────────────────────────────────────────────────────────────
export type { NotificationType } from './services/notificationBus';
export { notificationBus } from './services/notificationBus';
export type { NotificationEvent, Listener } from './services/notificationBus';
export type { NotificationData } from './types/notification';
export type { Notification } from './contexts/NotificationContext';
export type { CursorPosition, StatusBarProps } from './components/StatusBar';
export type { ContextMenuProps } from './components/ContextMenu';
export type { FileInfo } from './types/file-tree';
export type { FileTreeHandle, FileTreeProps } from './components/FileTree';
export type { GitStatusData, GitFile, FileSection, GitCommitSummary, GitCommitDetail, GitCommitFileEntry, } from './types/git-types';
export type { RevisionFile, Revision, RevisionDetailFile } from './types/revision';
export { normalizeRevision, buildRevisionFileKey } from './types/revision';
export type { ChangelogResponse, ChangesResponse, RevisionDetailResponse, RollbackResponse } from './types/api-responses';
export type { GitSidebarPanelProps, GitBranchesState } from './components/GitPanel';
export type { CommandPaletteProps, PaletteMode, CommandDef, FileResult, SymbolResult } from './components/CommandPalette';
export type { TerminalProps } from './components/Terminal';
export type { ShellInfo } from './types/terminal';
export type { TerminalThemePack, CreateTerminalConnection } from './components/TerminalPane';
export type { TerminalSession, AttachableSession } from './components/TerminalTabBar';
export type { EditorProps, EditorTheme, CursorPosition as EditorCursorPosition } from './components/Editor';
export type { SidebarProps } from './components/Sidebar';
export type { MenuBarProps, MenuDefinition, MenuBarItem } from './components/MenuBar';
export type { APIAdapter, PlatformNavItem } from './types/adapter';
export type { SproutEvent, SproutEventCallback, EventsProvider } from './types/events';
export type {
  EditorBuffer,
  EditorBufferKind,
  EditorFileEntry,
  EditorPane,
  PaneLayout,
  PaneSize,
  EditorState,
} from './types/editor';

// ── Git types and utilities ───────────────────────────────────────────
export { FILE_SECTIONS, selectionKey, parseSelectionKey } from './types/git-types';
export { MAX_FILES_PER_SECTION, MAX_FILES_INITIAL, LOAD_MORE_INCREMENT } from './constants/git-constants';

// ── Contexts ──────────────────────────────────────────────────────────
export { NotificationProvider, useNotifications } from './contexts/NotificationContext';
export { SproutProvider, useSproutAdapter, useSproutFetch } from './contexts/SproutAdapterContext';
export type { SproutProviderProps } from './contexts/SproutAdapterContext';
export { EventsContextProvider, useEvents } from './contexts/EventsContext';
export type { EventsContextProviderProps } from './contexts/EventsContext';

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
// Menu bar
export { default as MenuBar } from './components/MenuBar';
export { default as Sidebar } from './components/Sidebar';
export { default as CommandPalette } from './components/CommandPalette';
export { default as Editor } from './components/Editor';
export { Skeleton, SkeletonText } from './components/Skeleton';
export type { SkeletonProps, SkeletonTextProps } from './components/Skeleton';

// ── Dialogs ───────────────────────────────────────────────────────────
export { showThemedAlert, showThemedConfirm, showThemedPrompt } from './components/ThemedDialog';

// ── Utilities ─────────────────────────────────────────────────────────
export { generateUUID } from './utils/uuid';
export { detectLineEnding, type LineEnding, type LineEndingDetectionResult } from './utils/lineEndingDetect';
export { copyToClipboard } from './utils/clipboard';
export { fuzzyScore, fuzzyFilter, highlightMatches, type FuzzyResult } from './utils/fuzzyMatch';
export { debugLog } from './utils/log';
export { groupSubagentRuns } from './utils/subagentGrouping';
export { formatCost, formatTokens } from './utils/formatResourceUsage';
export { PERSONA_COLORS, getPersonaColor } from './utils/personaColors';
export { stripAnsiCodes, hasAnsiCodes, ansiToHtml } from './utils/ansi';
export { getStatusInfo } from './utils/git';
export type { CommandHistoryState, CommandHistoryApi } from './components/command_input_history';
export {
  createEmptyState,
  dedupeCommands,
  loadCommandHistory,
  persistCommandHistory
} from './components/command_input_history';
export { FONT_SIZE_DEFAULT } from './components/terminalConstants';
