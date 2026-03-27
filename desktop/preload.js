const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('leditDesktop', {
  platform: process.platform,
  listRecentWorktrees: () => ipcRenderer.invoke('desktop:listRecentWorktrees'),
  pickRepository: () => ipcRenderer.invoke('desktop:pickRepository'),
  pickWorkspace: () => ipcRenderer.invoke('desktop:pickWorkspace'),
  pickWorktree: () => ipcRenderer.invoke('desktop:pickWorktree'),
  pickWorktreeParent: () => ipcRenderer.invoke('desktop:pickWorktreeParent'),
  openWorkspace: (options) => ipcRenderer.invoke('desktop:openWorkspace', options),
  openWorktree: (options) => ipcRenderer.invoke('desktop:openWorktree', options),
  createWorktree: (options) => ipcRenderer.invoke('desktop:createWorktree', options),
  appVersion: () => ipcRenderer.invoke('desktop:appVersion'),
});
