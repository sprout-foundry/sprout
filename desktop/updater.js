/**
 * Auto-update integration using electron-updater.
 * Checks GitHub Releases and installs updates silently in the background.
 * Only active in packaged builds — no-ops during development.
 */

const { app, dialog } = require('electron');
const { autoUpdater } = require('electron-updater');

function initAutoUpdater() {
  if (!app.isPackaged) {
    return;
  }

  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = true;
  autoUpdater.allowPrerelease = false;

  autoUpdater.on('error', (error) => {
    // Log silently — update errors are non-critical
    console.error('[updater] Error:', error?.message || error);
  });

  autoUpdater.on('update-downloaded', () => {
    dialog.showMessageBox({
      type: 'info',
      title: 'Update Ready',
      message: 'A new version of Ledit has been downloaded.',
      detail: 'It will be installed automatically when you quit. Click "Install Now" to restart and apply the update immediately.',
      buttons: ['Install Now', 'Later'],
      defaultId: 1,
      cancelId: 1,
    }).then(({ response }) => {
      if (response === 0) {
        autoUpdater.quitAndInstall();
      }
    }).catch((error) => {
      console.error('[updater] Dialog error:', error);
    });
  });

  // Delay first check slightly to avoid blocking startup
  setTimeout(() => {
    autoUpdater.checkForUpdates().catch((error) => {
      console.error('[updater] checkForUpdates error:', error?.message || error);
    });
  }, 10000);
}

module.exports = { initAutoUpdater };
