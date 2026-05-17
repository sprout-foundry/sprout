/**
 * BackendConnectionBanner.tsx — Non-blocking banner for backend connection status.
 *
 * Shows at the top of the app when backend is unreachable and adapter requires health checks.
 * Dismissible but re-appears if still disconnected after next poll.
 */

import { X } from 'lucide-react';
import { useState, useEffect } from 'react';
import { requiresBackendHealthCheck } from '../services/apiAdapter';
import './BackendConnectionBanner.css';

interface BackendConnectionBannerProps {
  isReachable: boolean;
}

const DISMISSED_KEY = 'backend-banner-dismissed';

function wasBannerDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISSED_KEY) === 'true';
  } catch {
    return false;
  }
}

function markBannerDismissed(): void {
  try {
    localStorage.setItem(DISMISSED_KEY, 'true');
  } catch {
    /* Ignore storage errors */
  }
}

function clearBannerDismissed(): void {
  try {
    localStorage.removeItem(DISMISSED_KEY);
  } catch {
    /* Ignore storage errors */
  }
}

function BackendConnectionBanner({ isReachable }: BackendConnectionBannerProps): JSX.Element | null {
  const [dismissed, setDismissed] = useState<boolean>(false);
  const needsHealthCheck = requiresBackendHealthCheck();

  /* Clear dismissed state when backend becomes reachable */
  useEffect(() => {
    if (isReachable) {
      setDismissed(false);
      clearBannerDismissed();
    }
  }, [isReachable]);

  /* No health check needed — never show */
  if (!needsHealthCheck) {
    return null;
  }

  /* Backend is reachable — don't show banner */
  if (isReachable) {
    return null;
  }

  /* Backend unreachable but user dismissed — respect dismissal */
  if (dismissed && wasBannerDismissed()) {
    return null;
  }

  const handleDismiss = () => {
    setDismissed(true);
    markBannerDismissed();
  };

  return (
    <div className="backend-connection-banner" role="alert" aria-live="polite">
      <div className="backend-connection-banner-content">
        <span className="backend-connection-banner-icon">⚠️</span>
        <span className="backend-connection-banner-message">
          Unable to connect to server. Editor and terminal are available in offline mode. Retrying...
        </span>
        <button
          className="backend-connection-banner-dismiss"
          onClick={handleDismiss}
          aria-label="Dismiss notification"
          title="Dismiss"
        >
          <X size={16} />
        </button>
      </div>
    </div>
  );
}

export default BackendConnectionBanner;
