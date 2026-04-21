import { useState, useEffect } from 'react';
import { useNotifications } from '../contexts/NotificationContext';
import { AlertTriangle, Download, RefreshCw, X } from 'lucide-react';

interface UpdateInfo {
  version: string;
  releaseNotes?: string;
}

interface CheckResult {
  hasUpdate: boolean;
  version?: string;
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

  // Check for pending install on mount
  useEffect(() => {
    checkPendingInstall();
  }, []);

  const checkPendingInstall = async () => {
    if (!window.leditDesktop?.isUpdatePending) {
      return;
    }
    try {
      const result = await window.leditDesktop.isUpdatePending();
      setPendingInstall(result.pending || false);
    } catch (error) {
      console.error('Failed to check pending install:', error);
    }
  };

  const checkForUpdates = async () => {
    if (!window.leditDesktop?.checkForUpdates) {
      addNotification('info', 'Update Check', 'Auto-update is not available in this environment.');
      return;
    }

    setChecking(true);
    try {
      const result = await window.leditDesktop.checkForUpdates();
      if (result.ok && result.result) {
        setUpdateAvailable(result.result);
        if (result.result.hasUpdate) {
          addNotification(
            'success',
            'Update Available',
            `Version ${result.result.version} is ready to install.`,
            0 // Don't auto-dismiss
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

  const installNow = async () => {
    if (!window.leditDesktop?.installUpdate) {
      addNotification('error', 'Update Failed', 'Update installation is not available');
      return;
    }

    try {
      const result = await window.leditDesktop.installUpdate();
      if (result.ok) {
        // The app will quit and reinstall
        addNotification('success', 'Installing Update', 'Ledit will restart to apply the update.');
      } else {
        addNotification('error', 'Update Failed', 'Failed to install update');
      }
    } catch (error) {
      addNotification('error', 'Update Failed', 'An error occurred while installing the update');
      console.error('Install update error:', error);
    }
  };

  const deferInstall = async () => {
    if (!window.leditDesktop?.deferUpdate) {
      addNotification('info', 'Update Deferred', 'Update will be installed on next quit.');
      setPendingInstall(true);
      return;
    }

    try {
      const result = await window.leditDesktop.deferUpdate();
      if (result.ok) {
        setPendingInstall(true);
        addNotification('info', 'Update Deferred', 'Update will be installed when you quit Ledit.');
      } else {
        addNotification('error', 'Update Failed', 'Failed to defer update');
      }
    } catch (error) {
      addNotification('error', 'Update Failed', 'An error occurred while deferring the update');
      console.error('Defer update error:', error);
    }
  };

  const cancelPendingInstall = async () => {
    if (!window.leditDesktop?.cancelPendingInstall) {
      setPendingInstall(false);
      return;
    }

    try {
      const result = await window.leditDesktop.cancelPendingInstall();
      if (result.ok) {
        setPendingInstall(false);
        addNotification('info', 'Update Cancelled', 'Update installation has been cancelled.');
      }
    } catch (error) {
      console.error('Cancel pending install error:', error);
    }
  };

  // Don't render if there's no desktop API available
  if (!window.leditDesktop?.checkForUpdates) {
    return null;
  }

  // Show pending install banner
  if (pendingInstall) {
    return (
      <div
        className="update-pending-banner"
        role="alert"
        aria-live="polite"
      >
        <div className="update-pending-content">
          <Download size={16} className="update-pending-icon" />
          <span className="update-pending-text">
            An update is queued and will be installed when you quit Ledit.
          </span>
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
      <div
        className="update-available-banner"
        role="alert"
        aria-live="polite"
      >
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
      <div
        className="update-checking-banner"
        role="status"
        aria-live="polite"
      >
        <div className="update-checking-content">
          <RefreshCw size={14} className="spin" />
          <span>Checking for updates...</span>
        </div>
      </div>
    );
  }

  return null;
}

export default UpdateNotification;
