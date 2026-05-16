import { useState, useEffect, useCallback, useRef } from 'react';
import { useNotifications } from '../contexts/NotificationContext';
import { AlertTriangle, Download, RefreshCw, X, Loader } from 'lucide-react';

interface UpdateInfo {
  version: string;
  releaseNotes?: string;
}

interface CheckResult {
  hasUpdate: boolean;
  version?: string;
}

interface NotificationEvent {
  title: string;
  message: string;
  version?: string;
  duration?: number;
}

// Update-specific types are declared in types/desktop-api.d.ts alongside SproutDesktopAPI.

interface UpdateApiResponse {
  pending?: boolean;
  willInstallOnQuit?: boolean;
}

/**
 * UpdateNotification component that displays update status and provides
 * controls for installing or deferring updates.
 *
 * This component is rendered conditionally when an update is available
 * or when checking for updates. It provides a non-intrusive UI for
 * users to manage updates.
 */
function UpdateNotification(): JSX.Element | null {
  const { addNotification } = useNotifications();
  const [checking, setChecking] = useState(false);
  const [updateAvailable, setUpdateAvailable] = useState<CheckResult | null>(null);
  const [pendingInstall, setPendingInstall] = useState(false);
  const [downloadProgress, setDownloadProgress] = useState<number | null>(null);

  // Ref to store the latest checkForUpdates function for event listeners
  const checkForUpdatesRef = useRef<(() => Promise<void>) | null>(null);

  // Check for pending install on mount
  useEffect(() => {
    checkPendingInstall();
  }, []);

  const checkForUpdates = async () => {
    if (!window.sproutDesktop?.checkForUpdates) {
      addNotification('info', 'Update Check', 'Auto-update is not available in this environment.');
      return;
    }

    setChecking(true);
    setDownloadProgress(null);
    try {
      const result = await window.sproutDesktop.checkForUpdates();
      if (result.ok && result.result) {
        setUpdateAvailable(result.result);
        if (result.result.hasUpdate) {
          addNotification(
            'success',
            'Update Available',
            `Version ${result.result.version} is ready to install.`,
            0, // Don't auto-dismiss
          );
        }
      } else {
        addNotification('warning', 'Update Check', result.error || 'Failed to check for updates');
      }
    } catch (error) {
      addNotification('error', 'Update Check', 'An error occurred while checking for updates');
      console.error('Check for updates error:', error);
    } finally {
      setChecking(false);
    }
  };

  // Update the ref whenever checkForUpdates changes
  useEffect(() => {
    checkForUpdatesRef.current = checkForUpdates;
  }, [checkForUpdates]);

  // Listen for IPC events from the main process
  useEffect(() => {
    if (!window.sproutDesktop?.onUpdateError || !window.sproutDesktop?.onUpdateAvailable) {
      return;
    }

    // Listen for update error notifications
    const unsubscribeError = window.sproutDesktop.onUpdateError((data: NotificationEvent) => {
      addNotification('warning', data.title, data.message, data.duration);
    });

    // Listen for update available notifications
    const unsubscribeAvailable = window.sproutDesktop.onUpdateAvailable((data: NotificationEvent) => {
      setUpdateAvailable({ hasUpdate: true, version: data.version });
      setDownloadProgress(null); // Reset progress when download completes
      addNotification('success', data.title, data.message, data.duration);
    });

    // Listen for manual update check trigger from menu
    let unsubscribeTriggerUpdateCheck: (() => void) | null = null;
    if (window.sproutDesktop.onTriggerUpdateCheck) {
      unsubscribeTriggerUpdateCheck = window.sproutDesktop.onTriggerUpdateCheck(() => {
        // Call checkForUpdates when triggered from the menu
        if (checkForUpdatesRef.current) {
          void checkForUpdatesRef.current();
        }
      });
    }

    // Listen for download progress
    let unsubscribeDownloadProgress: (() => void) | null = null;
    if (window.sproutDesktop.onUpdateDownloadProgress) {
      unsubscribeDownloadProgress = window.sproutDesktop.onUpdateDownloadProgress((progress) => {
        console.warn(`Update download progress: ${progress.percent}%`);
        setDownloadProgress(progress.percent ?? null);
      });
    }

    // Listen for update downloaded event
    let unsubscribeDownloaded: (() => void) | null = null;
    if (window.sproutDesktop.onUpdateDownloaded) {
      unsubscribeDownloaded = window.sproutDesktop.onUpdateDownloaded((info) => {
        console.warn(`Update downloaded: ${info.version}`);
        setDownloadProgress(null); // Reset progress when download completes
        setUpdateAvailable({ hasUpdate: true, version: info.version });
        addNotification(
          'success',
          'Update Available',
          `Version ${info.version} is ready to install.`,
          0, // Don't auto-dismiss
        );
      });
    }

    // Cleanup event listeners
    return () => {
      unsubscribeError();
      unsubscribeAvailable();
      if (unsubscribeTriggerUpdateCheck) {
        unsubscribeTriggerUpdateCheck();
      }
      if (unsubscribeDownloadProgress) {
        unsubscribeDownloadProgress();
      }
      if (unsubscribeDownloaded) {
        unsubscribeDownloaded();
      }
    };
  }, [addNotification]);

  const checkPendingInstall = async () => {
    if (!window.sproutDesktop?.isUpdatePending) {
      return;
    }
    try {
      const result = await window.sproutDesktop.isUpdatePending();
      setPendingInstall(result.pending || false);
    } catch (error) {
      console.error('Failed to check pending install:', error);
    }
  };

  const installNow = async () => {
    if (!window.sproutDesktop?.installUpdate) {
      addNotification('error', 'Update Failed', 'Update installation is not available');
      return;
    }

    try {
      const result = await window.sproutDesktop.installUpdate();
      if (result.ok) {
        // The app will quit and reinstall
        addNotification('success', 'Installing Update', 'Sprout will restart to apply the update.');
      } else {
        addNotification('error', 'Update Failed', result.error || 'Failed to install update');
      }
    } catch (error) {
      addNotification('error', 'Update Failed', 'An error occurred while installing the update');
      console.error('Install update error:', error);
    }
  };

  const deferInstall = async () => {
    if (!window.sproutDesktop?.deferUpdate) {
      addNotification('info', 'Update Deferred', 'Update will be installed on next quit.');
      setPendingInstall(true);
      return;
    }

    try {
      const result = await window.sproutDesktop.deferUpdate();
      if (result.ok) {
        setPendingInstall(true);
        addNotification('info', 'Update Deferred', 'Update will be installed when you quit Sprout.');
      } else {
        addNotification('error', 'Update Failed', 'Failed to defer update');
      }
    } catch (error) {
      addNotification('error', 'Update Failed', 'An error occurred while deferring the update');
      console.error('Defer update error:', error);
    }
  };

  const cancelPendingInstall = async () => {
    if (!window.sproutDesktop?.cancelPendingInstall) {
      setPendingInstall(false);
      return;
    }

    try {
      const result = await window.sproutDesktop.cancelPendingInstall();
      if (result.ok) {
        setPendingInstall(false);
        addNotification('info', 'Update Cancelled', 'Update installation has been cancelled.');
      }
    } catch (error) {
      console.error('Cancel pending install error:', error);
    }
  };

  // Don't render if there's no desktop API available
  if (!window.sproutDesktop?.checkForUpdates) {
    return null;
  }

  // Show pending install banner
  if (pendingInstall) {
    return (
      <div className="update-pending-banner" role="alert" aria-live="polite">
        <div className="update-pending-content">
          <Download size={16} className="update-pending-icon" />
          <span className="update-pending-text">An update is queued and will be installed when you quit Sprout.</span>
          <button
            className="update-pending-dismiss"
            onClick={cancelPendingInstall}
            aria-label="Cancel update installation"
            type="button"
          >
            <X size={14} />
          </button>
        </div>
      </div>
    );
  }

  // Show update available banner
  if (updateAvailable?.hasUpdate && updateAvailable.version) {
    return (
      <div className="update-available-banner" role="alert" aria-live="polite">
        <div className="update-available-content">
          <div className="update-available-info">
            <AlertTriangle size={16} className="update-available-icon" />
            <div className="update-available-text">
              <strong>Update available:</strong> Version {updateAvailable.version}
            </div>
          </div>
          <div className="update-available-actions">
            <button
              className="update-button secondary"
              onClick={checkForUpdates}
              disabled={checking}
              aria-label="Check for updates"
              type="button"
            >
              <RefreshCw size={14} className={checking ? 'spin' : ''} />
              {checking ? 'Checking...' : 'Check Again'}
            </button>
            <button
              className="update-button primary"
              onClick={installNow}
              aria-label="Install update now"
              type="button"
            >
              Install Now
            </button>
            <button
              className="update-button"
              onClick={deferInstall}
              aria-label="Defer update installation"
              type="button"
            >
              Later
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Show checking state
  if (checking) {
    return (
      <div className="update-checking-banner" role="status" aria-live="polite">
        <div className="update-checking-content">
          <RefreshCw size={14} className="spin" />
          <span>Checking for updates...</span>
        </div>
      </div>
    );
  }

  // Show download progress
  if (downloadProgress !== null) {
    return (
      <div className="update-downloading-banner" role="status" aria-live="polite">
        <div className="update-downloading-content">
          <Loader size={16} className="update-downloading-icon" />
          <span className="update-downloading-text">Downloading update: {Math.round(downloadProgress)}%</span>
        </div>
      </div>
    );
  }

  return null;
}

export default UpdateNotification;
