/**
 * Service Worker registration extracted from App.tsx
 *
 * Handles PWA service worker registration, update detection,
 * and automatic activation/reload on new versions.
 */

import { debugLog } from '../utils/log';
import { isCloud } from '../config/mode';

export const registerServiceWorker = async (): Promise<ServiceWorkerRegistration | null> => {
  // In cloud mode, register the SW for app-shell caching + installability.
  // The SW caches navigations (network-first) and static assets (cache-first)
  // so the app loads fast and works offline (IndexedDB backs the VFS).
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
    // In cloud mode the SW is served at /webui/sw.js (relative to the app
    // scope). In local mode it's at the root.
    const swUrl = isCloud ? '/webui/sw.js' : `${import.meta.env.PUBLIC_URL || ''}/sw.js`;
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
