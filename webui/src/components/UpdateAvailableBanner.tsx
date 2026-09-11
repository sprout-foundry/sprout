import { X } from 'lucide-react';
import React, { useState, useEffect } from 'react';
import { fetchRuntimeConfig } from '../bootstrapAdapter';
import type { RuntimeConfig } from '../types/runtimeConfig';
import './UpdateAvailableBanner.css';

const DISMISSAL_KEY = 'sprout:update-banner-dismissed';

/**
 * Dismissible "new release available" banner, fed by the daemon's cached
 * (at-most-daily) GitHub release check via the bootstrap payload. Shows
 * at most once per browser profile per newer version; dismissal persists
 * in localStorage keyed by the latest version so a subsequent release
 * notifies again.
 */
const UpdateAvailableBanner: React.FC = () => {
  const [update, setUpdate] = useState<NonNullable<RuntimeConfig['update']>>();

  useEffect(() => {
    let cancelled = false;
    fetchRuntimeConfig()
      .then((config) => {
        if (cancelled || !config.update) return;
        if (localStorage.getItem(`${DISMISSAL_KEY}:${config.update.latest}`)) return;
        setUpdate(config.update);
      })
      .catch(() => {
        // Bootstrap failures leave the banner hidden; the app-level
        // bootstrap path already surfaces connectivity problems.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleDismiss = () => {
    if (update) {
      localStorage.setItem(`${DISMISSAL_KEY}:${update.latest}`, Date.now().toString());
    }
    setUpdate(undefined);
  };

  if (!update) return null;

  return (
    <div className="update-banner" role="status">
      <span className="update-banner-text">
        Sprout {update.latest} is available (running {update.current}).
      </span>
      <button
        className="update-banner-dismiss"
        onClick={handleDismiss}
        aria-label="Dismiss update notification"
      >
        <X size={14} />
      </button>
    </div>
  );
};

export default UpdateAvailableBanner;
