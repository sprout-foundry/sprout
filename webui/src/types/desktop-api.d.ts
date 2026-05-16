/**
 * TypeScript type declarations for the Sprout Desktop API.
 * This file extends the global Window interface to include
 * the desktop-specific IPC methods exposed via preload.js.
 */

export interface DesktopApiResponse<T = unknown> {
  ok: boolean;
  result?: T;
  error?: string;
}

export interface SproutDesktopAPI {
  // Platform detection
  platform: NodeJS.Platform;

  // Workspace management
  listRecentWorktrees: () => Promise<unknown[]>;
  listSshHosts: () => Promise<unknown[]>;
  listWslDistros: () => Promise<unknown[]>;
  pickRepository: () => Promise<unknown>;
  pickWorkspace: () => Promise<unknown>;
  pickWorktree: () => Promise<unknown>;
  pickWorktreeParent: () => Promise<unknown>;
  openSshWorkspace: (options: unknown) => Promise<unknown>;
  openWorkspace: (options: unknown) => Promise<unknown>;
  openWorktree: (options: unknown) => Promise<unknown>;
  createWorktree: (options: unknown) => Promise<unknown>;
  installWsl: () => Promise<unknown>;
  installGitForWindows: () => Promise<unknown>;
  appVersion: () => Promise<string>;

  // Desktop hotkey handling
  onDesktopHotkey: (callback: (commandId: string) => void) => () => void;

  // Auto-update API
  checkForUpdates: () => Promise<DesktopApiResponse<{ hasUpdate: boolean; version?: string }>>;
  installUpdate: () => Promise<DesktopApiResponse<{ pending?: boolean; willInstallOnQuit?: boolean }>>;
  deferUpdate: () => Promise<DesktopApiResponse<{ pending?: boolean; willInstallOnQuit?: boolean }>>;
  isUpdatePending: () => Promise<{ pending: boolean }>;
  cancelPendingInstall: () => Promise<DesktopApiResponse<{ pending?: boolean; willInstallOnQuit?: boolean }>>;

  // Auto-update event listeners
  onUpdateError: (callback: (data: { title: string; message: string; version?: string; duration?: number }) => void) => () => void;
  onUpdateAvailable: (callback: (data: { title: string; message: string; version?: string; duration?: number }) => void) => () => void;
  onUpdateDownloadProgress: (callback: (progress: { percent?: number }) => void) => () => void;
  onUpdateDownloaded: (callback: (info: { version?: string }) => void) => () => void;
  onTriggerUpdateCheck: (callback: () => void) => () => void;
}

declare global {
  interface Window {
    sproutDesktop?: SproutDesktopAPI;
  }
}

export {};
