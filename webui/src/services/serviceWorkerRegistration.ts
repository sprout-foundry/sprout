/**
 * Service Worker registration extracted from App.tsx
 *
 * Handles PWA service worker registration, update detection,
 * and automatic activation/reload on new versions.
 */

import { debugLog } from '../utils/log';
import { isCloud } from '../config/mode';

export const registerServiceWorker = async (): Promise<ServiceWorkerRegistration | null> => {
  // Skip SW registration in cloud mode (no sw.js served by the platform).
  // Use the build-time isCloud flag instead of adapter presence — the
  // adapter installs asynchronously, so getAdapter() may be null during
  // initial module load even in cloud mode.
  if (isCloud) {
    debugLog('SW registration skipped: cloud mode');
    return null;
  }

  if (!('serviceWorker' in navigator)) {
    return null;
  }

  const env = import.meta.env.PROD ? 'production' : 'development';
  if (env !== 'production') {
    const registrations = await navigator.serviceWorker.getRegistrations();
    await Promise.all(registrations.map((registration) => registration.unregister()));
    return null;
  }

  try {
    const swUrl = `${import.meta.env.PUBLIC_URL || ''}/sw.js`;
    const registration = await navigator.serviceWorker.register(swUrl);
    await registration.update();
    debugLog('SW registered:', registration);

    // If an update is already waiting, activate it immediately.
    if (registration.waiting) {
      registration.waiting.postMessage({ type: 'SKIP_WAITING' });
    }

    // Ensure we pick up new SW/controller as soon as it activates.
    let hasReloadedForController = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (hasReloadedForController) {
        return;
      }
      hasReloadedForController = true;
      window.location.reload();
    });

    registration.addEventListener('updatefound', () => {
      const newWorker = registration.installing;
      if (newWorker) {
        newWorker.addEventListener('statechange', () => {
          if (newWorker.state === 'installed') {
            newWorker.postMessage({ type: 'SKIP_WAITING' });
          }
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            debugLog('New service worker available');
          }
        });
      }
    });

    return registration;
  } catch (error) {
    debugLog('SW registration failed:', error);
  }

  return null;
};
