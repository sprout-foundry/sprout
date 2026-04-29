// Components
export { default as ContextMenu } from './components/ContextMenu';
export type { ContextMenuProps } from './components/ContextMenu';

export { default as NotificationStack } from './components/NotificationStack';
export { default as NotificationItem } from './components/NotificationItem';
export type { NotificationItemProps } from './components/NotificationItem';

export { default as StatusBar } from './components/StatusBar';
export type { StatusBarProps } from './components/StatusBar';

export { default as Editor } from './components/Editor';
export type { EditorProps } from './components/Editor';

export { default as Terminal } from './components/Terminal';
export type { TerminalProps, TerminalLine, TerminalLineType } from './components/Terminal';

export { default as FileTree } from './components/FileTree';
export type { FileTreeProps, TreeNode, GitStatus as FileTreeGitStatus } from './components/FileTree';

export { default as GitPanel } from './components/GitPanel';
export type { GitPanelProps, GitStatus } from './components/GitPanel';

export { default as ChatPanel } from './components/ChatPanel';
export type { ChatPanelProps, ChatMessage, ChatMessageType } from './components/ChatPanel';

export { default as Sidebar } from './components/Sidebar';
export type { SidebarProps, SidebarItem, SidebarSection } from './components/Sidebar';

export { default as CommandPalette } from './components/CommandPalette';
export type { CommandPaletteProps, PaletteItem } from './components/CommandPalette';

// Contexts
export { NotificationProvider, useNotifications } from './contexts/NotificationContext';
export type { NotificationType } from './services/notificationBus';
export type { Notification } from './contexts/NotificationContext';

// Services
export { notificationBus } from './services/notificationBus';
export type { NotificationEvent } from './services/notificationBus';

// Utils
export { generateUUID } from './utils/uuid';
export { detectLineEnding } from './utils/lineEndingDetect';
export type { LineEnding, LineEndingDetectionResult } from './utils/lineEndingDetect';
