/**
 * TypeScript type declarations for the Sprout Desktop API.
 * This file extends the global Window interface to include
 * the desktop-specific IPC methods exposed via preload.js.
 */

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
  checkForUpdates: () => Promise<{ ok: boolean; result?: { hasUpdate: boolean; version?: string }; error?: string }>;
  installUpdate: () => Promise<{ ok: boolean }>;
  deferUpdate: () => Promise<{ ok: boolean; willInstallOnQuit?: boolean }>;
  isUpdatePending: () => Promise<{ pending: boolean }>;
  cancelPendingInstall: () => Promise<{ ok: boolean }>;
}

declare global {
  interface Window {
    sproutDesktop?: SproutDesktopAPI;
  }
}

export {};
