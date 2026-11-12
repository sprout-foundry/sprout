/**
 * Workspace-mode shell contract.
 *
 * A mode's shell owns the shell around its surface: the `<main>` column and
 * the chrome that belongs to that mode. Code gets the menubar and status bar.
 * AppContent renders the active mode's shell with this one
 * props object, so adding a mode is a registry entry plus a shell component —
 * not new suppression flags poked into shared components.
 *
 * Plain data and callbacks only: shells read everything they render from
 * here and nothing else.
 */

import type { EditorBuffer } from '@sprout/ui';
import type { ComponentProps } from 'react';
import type { default as WorkspacePane } from '../components/WorkspacePane';
import type { ChatSession } from '../services/chatSessions';
import type { PerChatState, ViewType } from '../types/app';
import type { GitBranchesState, GitStatusData } from '../types/git-types';
import type { ShipPayload } from './ship';

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

/** Mockup payload, consumed by the Code shell's preview panel. */
export interface WorkspaceShellMockup {
  /** True while the mockup-presence probe is in flight. */
  loading: boolean;
  /** True when the workspace has a `design/mockup.html` to show. */
  present: boolean;
}
/** Git state the Code shell's status bar shows. */
export interface WorkspaceShellGit {
  gitBranches: GitBranchesState;
  gitStatus: GitStatusData | null;
  workspaceRoot: string;
  /**
   * The name the status bar labels the workspace with. An embedding host
   * passes its project's name, since its workspace root (a container path such
   * as `/workspace`) names nothing the user recognises. Unset, the label comes
   * from the root's basename or the open repository.
   */
  projectName?: string;
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
  /** Intra-mode route (chat/editor/git inside Code). */
  currentView: ViewType;
  onViewChange: (view: ViewType) => void;
  onToggleSidebar: () => void;
  supportsLocalTerminal: boolean;
  /**
   * Whether the Code shell mounts its in-browser (WASM) terminal when there is
   * no local terminal. Defaults to true; a composition whose host has no
   * terminal at all passes false, so no hidden terminal or shell is started.
   */
  inBrowserTerminal?: boolean;
  /**
   * Whether the Code shell offers its preview panel (and the header toggle and
   * `toggle_preview` command that open it). Defaults to true; a composition
   * whose host's backend does not serve `/api/preview/*` passes false, so the
   * panel is never mounted and nothing polls the preview status.
   */
  previewAvailable?: boolean;
  isTerminalExpanded: boolean;
  onTerminalExpandedChange: (expanded: boolean) => void;

  // Active editor buffer (the Code shell's status bar).
  currentBuffer: EditorBuffer | null;

  // Editor chrome handlers (Code shell).
  handleOutlineNavigateToSymbol: (line: number) => void;

  // Per-surface payloads; each shell reads only its own.
  chat: WorkspaceShellChat;
  mockup: WorkspaceShellMockup;
  git: WorkspaceShellGit;
  ship: ShipPayload;
}
