/**
 * Auto-update integration using electron-updater.
 * Checks GitHub Releases and installs updates silently in the background.
 * Only active in packaged builds — no-ops during development.
 */

const { app, dialog, ipcMain } = require('electron');
const { autoUpdater } = require('electron-updater');

/**
 * Track if an update notification has been shown to avoid duplicate notifications.
 */
let updateNotificationShown = false;

/**
 * Track if user has deferred update installation (will install on quit).
 */
let installOnQuit = false;

/**
 * Initialize auto-updater with event handlers and IPC registration.
 */
function initAutoUpdater() {
  if (!app.isPackaged) {
    console.log('[updater] Running in development mode — auto-update disabled');
    return;
  }

  // Configure updater settings
  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = false; // We'll control this manually
  autoUpdater.allowPrerelease = false;

  // Register IPC handlers for webui communication
  registerIpcHandlers();

  // Error handler — log but don't interrupt user
  autoUpdater.on('error', (error) => {
    console.error('[updater] Error:', error?.message || error);
    // Send error notification to webui
    notifyUpdateError(error);
  });

  // Download progress handler — optional logging
  autoUpdater.on('download-progress', (progress) => {
    console.log(`[updater] Download progress: ${progress.percent}%`);
  });

  // Update downloaded handler — show non-intrusive notification
  autoUpdater.on('update-downloaded', (info) => {
    console.log(`[updater] Update downloaded: ${info.version}`);
    notifyUpdateAvailable(info);
  });

  // Delay first check slightly to avoid blocking startup
  setTimeout(() => {
    checkForUpdates();
  }, 10000);
}

/**
 * Register IPC handlers for webui to communicate with auto-updater.
 */
function registerIpcHandlers() {
  // Check for updates on demand
  ipcMain.handle('desktop:checkForUpdates', async () => {
    try {
      const result = await checkForUpdates();
      return { ok: true, result };
    } catch (error) {
      return { ok: false, error: error?.message || 'Failed to check for updates' };
    }
  });

  // Install update immediately
  ipcMain.handle('desktop:installUpdate', async () => {
    installOnQuit = false;
    autoUpdater.quitAndInstall();
    return { ok: true };
  });

  // Defer update to install on quit
  ipcMain.handle('desktop:deferUpdate', async () => {
    installOnQuit = true;
    return { ok: true, willInstallOnQuit: true };
  });

  // Check if update is pending install on quit
  ipcMain.handle('desktop:isUpdatePending', async () => {
    return { pending: installOnQuit };
  });

  // Cancel pending install on quit
  ipcMain.handle('desktop:cancelPendingInstall', async () => {
    installOnQuit = false;
    return { ok: true };
  });
}

/**
 * Check for updates and return the result.
 * @returns {Promise<Object>} Result with update info or null
 */
async function checkForUpdates() {
  try {
    const result = await autoUpdater.checkForUpdates();
    return result ? { hasUpdate: true, version: result.updateInfo.version } : { hasUpdate: false };
  } catch (error) {
    console.error('[updater] checkForUpdates error:', error?.message || error);
    throw error;
  }
}

/**
 * Send an error notification to the webui via notificationBus.
 * @param {Error} error - The error that occurred
 */
function notifyUpdateError(error) {
  const { notificationBus } = require('../webui/src/services/notificationBus');
  if (notificationBus) {
    notificationBus.notify(
      'warning',
      'Update Check Failed',
      'We were unable to check for updates. Please try again later.',
      5000
    );
  }
}

/**
 * Send a non-intrusive update available notification to the webui.
 * @param {Object} info - Update information from electron-updater
 */
function notifyUpdateAvailable(info) {
  const { notificationBus } = require('../webui/src/services/notificationBus');
  if (notificationBus) {
    notificationBus.notify(
      'success',
      'Update Available',
      `Version ${info.version} is ready to install. You can install now or quit to install automatically.`,
      10000 // Longer duration for update notifications
    );
  }
}

/**
 * Check if an update is pending install on quit.
 * @returns {boolean} True if update will be installed on quit
 */
function isUpdatePending() {
  return installOnQuit;
}

/**
 * Set the install-on-quit flag.
 * @param {boolean} value - Whether to install on quit
 */
function setInstallOnQuit(value) {
  installOnQuit = value;
}

module.exports = {
  initAutoUpdater,
  checkForUpdates,
  isUpdatePending,
  setInstallOnQuit,
};
