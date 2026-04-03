const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('leditDesktop', {
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
});
