/**
 * Auto-update integration using electron-updater.
 * Checks GitHub Releases and installs updates silently in the background.
 * Only active in packaged builds — no-ops during development.
 */

const { app, ipcMain, BrowserWindow } = require('electron');
const { autoUpdater } = require('electron-updater');
const fs = require('node:fs');
const path = require('node:path');

/**
 * Path to persist update state across app restarts.
 */
const UPDATE_STATE_FILE = path.join(app.getPath('userData'), 'update-state.json');

/**
 * Track if user has deferred update installation (will install on quit).
 */
let installOnQuit = false;

/**
 * Track if an update has been downloaded and ready to install.
 */
let updateDownloaded = false;

/**
 * Track the version of the downloaded update.
 */
let downloadedVersion = null;

/**
 * Load persisted update state from disk.
 */
function loadUpdateState() {
  try {
    if (fs.existsSync(UPDATE_STATE_FILE)) {
      const data = fs.readFileSync(UPDATE_STATE_FILE, 'utf8');
      const state = JSON.parse(data);
      if (state.installOnQuit) {
        installOnQuit = true;
      }
      if (state.updateDownloaded) {
        updateDownloaded = true;
        downloadedVersion = state.downloadedVersion || null;
      }
    }
  } catch (error) {
    console.error('[updater] Failed to load update state:', error);
  }
}

/**
 * Persist update state to disk.
 * @param {Object} stateOverride - Optional override for the state object
 */
function persistUpdateState(stateOverride = null) {
  try {
    const state = stateOverride || {
      installOnQuit,
      updateDownloaded,
      downloadedVersion,
    };
    const data = JSON.stringify(state);
    fs.writeFileSync(UPDATE_STATE_FILE, data, 'utf8');
  } catch (error) {
    console.error('[updater] Failed to persist update state:', error);
  }
}

/**
 * Initialize auto-updater with event handlers and IPC registration.
 */
function initAutoUpdater() {
  if (!app.isPackaged) {
    console.log('[updater] Running in development mode — auto-update disabled');
    // Even in dev mode we still register the IPC handlers so the webui
    // doesn't crash on launch when it polls `desktop:isUpdatePending`
    // (preload.js wires it up unconditionally). The handlers return
    // safe "no update available / no install pending" defaults so the
    // UI's update-banner path stays inert.
    registerIpcHandlers();
    return;
  }

  // Load persisted state
  loadUpdateState();

  // Show notification if update was previously downloaded but not installed
  if (updateDownloaded && downloadedVersion) {
    // Delay notification slightly to allow UI to initialize
    setTimeout(() => {
      notifyUpdateAvailable({ version: downloadedVersion });
    }, 1000);
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

  // Download progress handler — send progress to webui
  autoUpdater.on('download-progress', (progress) => {
    console.log(`[updater] Download progress: ${progress.percent}%`);
    const mainWindow = BrowserWindow.getAllWindows()[0];
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('update:download-progress', {
        percent: progress.percent,
        transferred: progress.transferred,
        total: progress.total,
        bytesPerSecond: progress.bytesPerSecond,
      });
    }
  });

  // Update downloaded handler — show non-intrusive notification
  autoUpdater.on('update-downloaded', (info) => {
    console.log(`[updater] Update downloaded: ${info.version}`);
    updateDownloaded = true;
    downloadedVersion = info.version;

    // Validate update info before using
    if (!info?.version || typeof info.version !== 'string') {
      console.error('[updater] Invalid update info: missing or invalid version');
      return;
    }

    // Persist state with error handling
    try {
      persistUpdateState();
      notifyUpdateAvailable(info);

      // Also send direct IPC event for immediate response
      const mainWindow = BrowserWindow.getAllWindows()[0];
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('update:downloaded', {
          version: info.version,
        });
      }
    } catch (error) {
      console.error('[updater] Failed to persist update state after download:', error);
      // Notify user that state persistence failed
      notifyUpdateError({
        title: 'Update download failed',
        message: 'Update downloaded but state could not be saved. The update may not persist after restart.'
      });
    }
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
    if (!app.isPackaged) {
      // Dev mode: autoUpdater is unconfigured. Calling checkForUpdates
      // would throw a noisy "no update server" error; report "no update"
      // instead so the webui's update-check UI stays quiet.
      return { ok: true, result: { hasUpdate: false } };
    }
    try {
      const result = await checkForUpdates();
      return { ok: true, result };
    } catch (error) {
      return { ok: false, error: error?.message || 'Failed to check for updates' };
    }
  });

  // Install update immediately
  ipcMain.handle('desktop:installUpdate', async () => {
    if (!app.isPackaged) {
      // Dev mode: quitAndInstall would crash the dev electron — refuse
      // cleanly so the renderer can handle the no-op response.
      return { ok: false, error: 'Auto-update is disabled in development mode' };
    }
    try {
      // Clear the install-on-quit flag since we're installing now
      installOnQuit = false;

      // Clear the downloaded state since we're installing now
      // Note: Must do this BEFORE quitAndInstall because app quits immediately
      updateDownloaded = false;
      downloadedVersion = null;
      persistUpdateState();

      // Attempt to quit and install
      // Note: This will quit the app immediately, so code below may not execute
      autoUpdater.quitAndInstall();

      // This return won't execute, but keeps TypeScript happy
      return { ok: true };
    } catch (error) {
      console.error('[updater] quitAndInstall error:', error);
      // Restore state since install failed
      installOnQuit = false;
      updateDownloaded = false;
      downloadedVersion = null;
      persistUpdateState();
      // Return error to renderer so it can show a notification
      return { ok: false, error: error.message || 'Failed to install update' };
    }
  });

  // Defer update to install on quit
  ipcMain.handle('desktop:deferUpdate', async () => {
    try {
      installOnQuit = true;
      persistUpdateState();
      return { ok: true, willInstallOnQuit: true };
    } catch (error) {
      console.error('[updater] Failed to defer update:', error);
      return { ok: false, error: error.message || 'Failed to defer update' };
    }
  });

  // Check if update is pending install on quit
  ipcMain.handle('desktop:isUpdatePending', async () => {
    try {
      return { pending: installOnQuit };
    } catch (error) {
      console.error('[updater] Failed to check pending status:', error);
      return { pending: false, error: error.message };
    }
  });

  // Cancel pending install on quit
  ipcMain.handle('desktop:cancelPendingInstall', async () => {
    try {
      installOnQuit = false;
      updateDownloaded = false;
      downloadedVersion = null;
      persistUpdateState();
      return { ok: true };
    } catch (error) {
      console.error('[updater] Failed to cancel pending install:', error);
      return { ok: false, error: error.message || 'Failed to cancel pending install' };
    }
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
 * Send an error notification to the webui via IPC.
 * @param {Error} error - The error that occurred
 */
function notifyUpdateError(error) {
  // Determine appropriate error message based on error type
  let errorMessage = 'We were unable to check for updates. Please try again later.';
  const errorMsg = error?.message || '';
  const errorCode = error?.code || '';

  if (errorMsg.includes('net')) {
    errorMessage = 'Network error while checking for updates. Please check your connection.';
  } else if (errorCode === 'ERR_UPDATER_CHANNEL_INVALID') {
    errorMessage = 'Update server configuration error. Please reinstall the app.';
  }

  // Send IPC message to renderer to show notification
  const mainWindow = BrowserWindow.getAllWindows()[0];
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send('update:error', {
      title: 'Update Check Failed',
      message: errorMessage,
      duration: 8000,
    });
  }
}

/**
 * Send a non-intrusive update available notification to the webui via IPC.
 * @param {Object} info - Update information from electron-updater
 */
function notifyUpdateAvailable(info) {
  // Send IPC message to renderer to show notification
  const mainWindow = BrowserWindow.getAllWindows()[0];
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send('update:available', {
      title: 'Update Available',
      message: `Version ${info.version} is ready to install. You can install now or quit to install automatically.`,
      version: info.version,
      duration: 10000, // Longer duration for update notifications
    });
  }
}

/**
 * Check if an update is pending install on quit.
 * @returns {boolean} True if update will be installed on quit
 */
function isUpdatePending() {
  return installOnQuit;
}

module.exports = {
  initAutoUpdater,
  checkForUpdates,
  isUpdatePending,
};
