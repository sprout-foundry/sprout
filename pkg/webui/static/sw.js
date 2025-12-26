const CACHE_NAME = 'ledit-v1';
const STATIC_CACHE = 'ledit-static-v1';
const RUNTIME_CACHE = 'ledit-runtime-v1';

// Static assets to cache immediately
const STATIC_ASSETS = [
  '/',
  '/index.html',
  '/manifest.json',
  '/icon-192.png',
  '/icon-512.png',
  // CSS and JS files will be added during build
];

// Install event - cache static assets
self.addEventListener('install', (event) => {
  console.log('[SW] Installing service worker...');

  event.waitUntil(
    caches.open(STATIC_CACHE)
      .then((cache) => {
        console.log('[SW] Caching static assets');
        return cache.addAll(STATIC_ASSETS);
      })
      .then(() => self.skipWaiting())
  );
});

// Activate event - clean up old caches
self.addEventListener('activate', (event) => {
  console.log('[SW] Activating service worker...');

  event.waitUntil(
    caches.keys()
      .then((cacheNames) => {
        return Promise.all(
          cacheNames.map((cacheName) => {
            if (cacheName !== STATIC_CACHE && cacheName !== RUNTIME_CACHE) {
              console.log('[SW] Deleting old cache:', cacheName);
              return caches.delete(cacheName);
            }
          })
        );
      })
      .then(() => self.clients.claim())
  );
});

// Fetch event - implement caching strategies
self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // Skip non-GET requests and external requests
  if (request.method !== 'GET' || !url.origin.includes(self.location.origin)) {
    return;
  }

  // Handle different types of requests with different strategies
  if (isStaticAsset(request.url)) {
    // Cache First for static assets
    event.respondWith(handleCacheFirst(request));
  } else if (isAPIRequest(request.url)) {
    // Network First for API requests
    event.respondWith(handleNetworkFirst(request));
  } else {
    // Network First for other requests
    event.respondWith(handleNetworkFirst(request));
  }
});

// Cache First strategy for static assets
async function handleCacheFirst(request) {
  const cachedResponse = await caches.match(request);

  if (cachedResponse) {
    return cachedResponse;
  }

  try {
    const networkResponse = await fetch(request);

    if (networkResponse.ok) {
      const cache = await caches.open(STATIC_CACHE);
      cache.put(request, networkResponse.clone());
    }

    return networkResponse;
  } catch (error) {
    console.log('[SW] Network request failed, serving from cache if available');
    return caches.match(request) || new Response('Offline', {
      status: 503,
      statusText: 'Service Unavailable'
    });
  }
}

// Network First strategy for API requests and navigation
async function handleNetworkFirst(request) {
  try {
    const networkResponse = await fetch(request);

    if (networkResponse.ok && request.method === 'GET') {
      const cache = await caches.open(RUNTIME_CACHE);
      cache.put(request, networkResponse.clone());
    }

    return networkResponse;
  } catch (error) {
    console.log('[SW] Network request failed, serving from cache if available');
    const cachedResponse = await caches.match(request);

    if (cachedResponse) {
      return cachedResponse;
    }

    // Return a basic offline page for navigation requests
    if (request.mode === 'navigate') {
      return new Response(`
        <!DOCTYPE html>
        <html>
          <head>
            <title>ledit - Offline</title>
            <style>
              body {
                font-family: system-ui, -apple-system, sans-serif;
                display: flex;
                align-items: center;
                justify-content: center;
                min-height: 100vh;
                margin: 0;
                background: #1a1a2a;
                color: #e0e0e0;
                text-align: center;
              }
              .offline-container {
                max-width: 400px;
                padding: 2rem;
              }
              .offline-icon {
                font-size: 4rem;
                margin-bottom: 1rem;
              }
              h1 {
                margin: 0 0 1rem 0;
                color: #6366f1;
              }
              p {
                margin: 0 0 2rem 0;
                line-height: 1.5;
              }
              button {
                background: #6366f1;
                color: white;
                border: none;
                padding: 0.75rem 1.5rem;
                border-radius: 6px;
                cursor: pointer;
                font-size: 1rem;
              }
              button:hover {
                background: #5558e3;
              }
            </style>
          </head>
          <body>
            <div class="offline-container">
              <div class="offline-icon">📡</div>
              <h1>You're offline</h1>
              <p>ledit requires an internet connection to function. Please check your connection and try again.</p>
              <button onclick="window.location.reload()">Retry</button>
            </div>
          </body>
        </html>
      `, {
        headers: { 'Content-Type': 'text/html' }
      });
    }

    return new Response('Offline', {
      status: 503,
      statusText: 'Service Unavailable'
    });
  }
}

// Helper functions
function isStaticAsset(url) {
  return url.includes('/static/') ||
         url.includes('.css') ||
         url.includes('.js') ||
         url.includes('.png') ||
         url.includes('.jpg') ||
         url.includes('.svg') ||
         url.includes('.ico') ||
         url.includes('/manifest.json');
}

function isAPIRequest(url) {
  return url.includes('/api/');
}

// Handle background sync for when the app comes back online
self.addEventListener('sync', (event) => {
  console.log('[SW] Background sync event:', event.tag);

  if (event.tag === 'background-sync') {
    event.waitUntil(doBackgroundSync());
  }
});

async function doBackgroundSync() {
  try {
    // Wake up any sleeping clients
    const clients = await self.clients.matchAll({ type: 'window' });
    clients.forEach(client => {
      client.postMessage({ type: 'BACKGROUND_SYNC' });
    });
  } catch (error) {
    console.log('[SW] Background sync failed:', error);
  }
}

// Handle push notifications (future enhancement)
self.addEventListener('push', (event) => {
  if (!event.data) {
    return;
  }

  const data = event.data.json();
  const options = {
    body: data.body,
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    vibrate: [100, 50, 100],
    data: data.data
  };

  event.waitUntil(
    self.registration.showNotification(data.title, options)
  );
});

// Handle notification clicks
self.addEventListener('notificationclick', (event) => {
  event.notification.close();

  event.waitUntil(
    clients.matchAll({ type: 'window' }).then((clientList) => {
      for (const client of clientList) {
        if (client.url === '/' && 'focus' in client) {
          return client.focus();
        }
      }
      if (clients.openWindow) {
        return clients.openWindow('/');
      }
    })
  );
});