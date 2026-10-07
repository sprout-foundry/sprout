/**
 * Service Worker registration extracted from App.tsx
 *
 * Handles PWA service worker registration, update detection,
 * and automatic activation/reload on new versions.
 */

import { capability } from '../config/mode';
import { getActiveHost } from '../host/accessor';
import { debugLog } from '../utils/log';

export const registerServiceWorker = async (): Promise<ServiceWorkerRegistration | null> => {
  // A shell with a local terminal ships its own service worker; a hosted shell
  // (WASM terminal, no local terminal) ships none: the platform's vendored dist
  // strips sw.js (see platform/scripts/update-sprout-webui.sh) and the SPA
  // catch-all would serve index.html (text/html) for /webui/sw.js, turning
  // the registration into a "script has an unsupported MIME type" console
  // error. The CloudAdapter proxies the platform API directly; offline
  // state is backed by IndexedDB, not a network cache.
  const localTerminal = getActiveHost()?.capabilities.localTerminal ?? capability('supportsLocalTerminal', true, false);
  if (!localTerminal) {
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
    // The service worker is served at the app scope for a hosted shell
    // (no local terminal); a local-terminal shell serves it at the root.
    const swUrl = !localTerminal ? '/webui/sw.js' : `${import.meta.env.PUBLIC_URL || ''}/sw.js`;
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
