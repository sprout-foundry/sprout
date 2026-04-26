const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('sproutDesktop', {
  platform: process.platform,
  listRecentWorktrees: () => ipcRenderer.invoke('desktop:listRecentWorktrees'),
  listSshHosts: () => ipcRenderer.invoke('desktop:listSshHosts'),
  listWslDistros: () => ipcRenderer.invoke('desktop:listWslDistros'),
  pickRepository: () => ipcRenderer.invoke('desktop:pickRepository'),
  pickWorkspace: () => ipcRenderer.invoke('desktop:pickWorkspace'),
  pickWorktree: () => ipcRenderer.invoke('desktop:pickWorktree'),
  pickWorktreeParent: () => ipcRenderer.invoke('desktop:pickWorktreeParent'),
  openSshWorkspace: (options) => ipcRenderer.invoke('desktop:openSshWorkspace', options),
  openWorkspace: (options) => ipcRenderer.invoke('desktop:openWorkspace', options),
  openWorktree: (options) => ipcRenderer.invoke('desktop:openWorktree', options),
  createWorktree: (options) => ipcRenderer.invoke('desktop:createWorktree', options),
  installWsl: () => ipcRenderer.invoke('desktop:installWsl'),
  installGitForWindows: () => ipcRenderer.invoke('desktop:installGitForWindows'),
  appVersion: () => ipcRenderer.invoke('desktop:appVersion'),
  onDesktopHotkey: (callback) => {
    const handler = (_event, commandId) => callback(commandId);
    ipcRenderer.on('desktop:hotkey', handler);
    return () => ipcRenderer.removeListener('desktop:hotkey', handler);
  },
  // Auto-update API
  checkForUpdates: () => ipcRenderer.invoke('desktop:checkForUpdates'),
  installUpdate: () => ipcRenderer.invoke('desktop:installUpdate'),
  deferUpdate: () => ipcRenderer.invoke('desktop:deferUpdate'),
  isUpdatePending: () => ipcRenderer.invoke('desktop:isUpdatePending'),
  cancelPendingInstall: () => ipcRenderer.invoke('desktop:cancelPendingInstall'),
  // Auto-update event listeners
  onUpdateError: (callback) => {
    const handler = (_event, data) => callback(data);
    ipcRenderer.on('update:error', handler);
    return () => ipcRenderer.removeListener('update:error', handler);
  },
  onUpdateAvailable: (callback) => {
    const handler = (_event, data) => callback(data);
    ipcRenderer.on('update:available', handler);
    return () => ipcRenderer.removeListener('update:available', handler);
  },
  onUpdateDownloadProgress: (callback) => {
    const handler = (_event, progress) => callback(progress);
    ipcRenderer.on('update:download-progress', handler);
    return () => ipcRenderer.removeListener('update:download-progress', handler);
  },
  onUpdateDownloaded: (callback) => {
    const handler = (_event, info) => callback(info);
    ipcRenderer.on('update:downloaded', handler);
    return () => ipcRenderer.removeListener('update:downloaded', handler);
  },
  // Listen for manual update check trigger from menu
  onTriggerUpdateCheck: (callback) => {
    const handler = (_event) => callback();
    ipcRenderer.on('desktop:trigger-update-check', handler);
    return () => ipcRenderer.removeListener('desktop:trigger-update-check', handler);
  },
});
