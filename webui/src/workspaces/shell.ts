/**
 * Workspace-mode shell contract.
 *
 * A mode's shell owns the shell around its surface: the `<main>` column and
 * the chrome that belongs to that mode. Code gets the menubar, context panel,
 * and status bar; Design gets none of it. AppContent renders the active
 * mode's shell with this one props object, so adding a mode is a registry
 * entry plus a shell component — not new suppression flags poked into shared
 * components.
 *
 * Plain data and callbacks only: shells read everything they render from
 * here and nothing else.
 */

import type { EditorBuffer, LogEntry, SubagentActivity, ToolExecution } from '@sprout/ui';
import type { ComponentProps, RefObject } from 'react';
import type { ContextPanelHandle } from '../components/contextPanel/types';
import type { DesignTab } from '../components/design/DesignView';
import type { default as WorkspacePane } from '../components/WorkspacePane';
import type { ChatSession } from '../services/chatSessions';
import type { PerChatState, QueryProgress, ViewType } from '../types/app';
import type { GitBranchesState, GitStatusData } from '../types/git-types';

/** Chat/editor surface payload, consumed by the Code shell. */
export interface WorkspaceShellChat {
  perChatCache?: Record<string, PerChatState>;
  activeChatId?: string | null;
  chatSessions?: ChatSession[];
  onActiveChatChange?: (id: string) => void;
  onCreateChat?: () => Promise<string | null>;
  /** Opens the host's New-Chat-in-Worktree dialog (AppContent owns it). */
  onCreateChatInWorktree?: () => void;
  onDeleteChat?: (id: string, options?: { removeWorktree?: boolean }) => Promise<void> | void;
  onDeleteAllChats?: () => void;
  onRenameChat?: (id: string, name: string) => void;
  chatProps: ComponentProps<typeof WorkspacePane>['chatProps'];
  reviewProps: ComponentProps<typeof WorkspacePane>['reviewProps'];
  diffState: ComponentProps<typeof WorkspacePane>['diffState'];
}

/** Design surface payload, consumed by the Design shell. */
export interface WorkspaceShellDesign {
  /** True while the design-presence probe is in flight. */
  loading: boolean;
  /** True when the workspace has a design/ tree. */
  present: boolean;
  /** The active section, driven by the mode's rail. */
  tab: DesignTab;
  onTabChange: (tab: DesignTab) => void;
  /** Leave Design (back to Code mode). */
  onBack?: () => void;
  onOpenFile?: (path: string, lineNumber?: number) => void;
}

/** Git state the Code shell's status bar shows. */
export interface WorkspaceShellGit {
  gitBranches: GitBranchesState;
  gitStatus: GitStatusData | null;
  workspaceRoot: string;
}

/**
 * Everything a mode shell may need from the app, in one object. Shells take
 * the whole object and read only what their mode renders.
 */
export interface WorkspaceShellProps {
  // Viewport and shell chrome (both shells).
  isMobile: boolean;
  isTablet: boolean;
  isSidebarOpen: boolean;
  isConnected: boolean;
  /** Intra-mode route (chat/editor/git inside Code; the design surface inside Design). */
  currentView: ViewType;
  onViewChange: (view: ViewType) => void;
  onToggleSidebar: () => void;
  onToggleContextPanel: () => void;
  supportsLocalTerminal: boolean;
  isTerminalExpanded: boolean;
  onTerminalExpandedChange: (expanded: boolean) => void;

  // Right-hand context panel (chat context; the Code shell renders it).
  showContextSidebar: boolean;
  contextPanelRef: RefObject<ContextPanelHandle>;
  toolExecutions: ToolExecution[];
  logs: LogEntry[];
  subagentActivities: SubagentActivity[];
  messages: Array<{ type: string; timestamp: Date }>;
  isProcessing: boolean;
  lastError: string | null;
  queryProgress: QueryProgress | null;

  // Active editor buffer (the Code shell's status bar).
  currentBuffer: EditorBuffer | null;

  // Editor chrome handlers (Code shell).
  handleOutlineNavigateToSymbol: (line: number) => void;

  // Per-mode payloads; each shell reads only its own.
  chat: WorkspaceShellChat;
  design: WorkspaceShellDesign;
  git: WorkspaceShellGit;
}
