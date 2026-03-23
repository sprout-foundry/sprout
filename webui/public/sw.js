// Service Worker - Network Only (no caching for development)

// Install event
self.addEventListener('install', (event) => {
  console.log('[SW] Installing service worker...');
  // Skip waiting to activate immediately
  self.skipWaiting();
});

// Activate event - clean up old caches
self.addEventListener('activate', (event) => {
  console.log('[SW] Activating service worker...');
  // Clean up all caches
  event.waitUntil(
    caches.keys().then((cacheNames) => {
      return Promise.all(
        cacheNames.map((cacheName) => {
          console.log('[SW] Deleting cache:', cacheName);
          return caches.delete(cacheName);
        })
      );
    }).then(() => self.clients.claim())
  );
});

// Fetch event - Network Only (never use cache)
self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // Skip non-GET requests and external requests
  if (request.method !== 'GET' || url.origin !== self.location.origin) {
    return;
  }

  // Chromium can emit non-same-origin only-if-cached requests that throw if intercepted
  if (request.cache === 'only-if-cached' && request.mode !== 'same-origin') {
    return;
  }

  // Always use network only - no caching, but avoid unhandled promise rejection noise
  event.respondWith(
    fetch(request).catch(() => {
      if (request.mode === 'navigate') {
        return new Response('Offline', {
          status: 503,
          statusText: 'Service Unavailable',
          headers: { 'Content-Type': 'text/plain; charset=utf-8' }
        });
      }
      return Response.error();
    })
  );
});
