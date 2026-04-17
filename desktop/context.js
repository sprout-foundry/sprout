/**
 * Shared mutable application state. Required by all modules that need
 * access to cross-window registries or lifecycle flags.
 */

const ctx = {
  /** Maps BrowserWindow.id → { child, port, workspacePath, backendMode, wslDistro, reloadCallback } */
  instanceRegistry: new Map(),
  /** Maps workspaceKey → BrowserWindow.id for local workspaces */
  workspaceWindowMap: new Map(),
  /** Maps "ssh:<alias>:<remotePath>" → BrowserWindow.id */
  sshWindowMap: new Map(),
  /** Maps worktreePath → setTimeout handle for debounced bounds writes */
  windowStateWriteTimers: new Map(),
  /** Targets received before app.whenReady fired */
  pendingOpenTargets: [],
  /** Set to true once app.whenReady resolves */
  isAppReady: false,
  /** The shared launcher BrowserWindow, if open */
  launcherWindow: null,
};

module.exports = ctx;
