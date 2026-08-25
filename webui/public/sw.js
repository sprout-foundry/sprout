// Sprout Editor Service Worker
// App-shell caching for fast loads + offline support.
// WASM binary (42MB) is cached separately as a versioned asset.

const CACHE_VERSION = 'sprout-v2';
const SHELL_CACHE = `${CACHE_VERSION}-shell`;
const ASSET_CACHE = `${CACHE_VERSION}-assets`;

// Assets to pre-cache on install (app shell only — WASM is too large for
// precache; it gets cached on first fetch via the cache-first strategy).
const APP_SHELL = [
  './',
  './index.html',
  './manifest.json',
];

self.addEventListener('install', (event) => {
  self.skipWaiting();
  event.waitUntil(
    caches.open(SHELL_CACHE).then((cache) =>
      cache.addAll(APP_SHELL).catch(() => {})
    )
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => !k.startsWith(CACHE_VERSION))
          .map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('message', (event) => {
  if (event?.data?.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);

  // Only handle GET for same-origin
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (request.cache === 'only-if-cached' && request.mode !== 'same-origin') return;

  // Network-first for navigations (HTML) — always get fresh HTML, fall back to cache when offline.
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((resp) => {
          const copy = resp.clone();
          caches.open(SHELL_CACHE).then((cache) => cache.put(request, copy));
          return resp;
        })
        .catch(() =>
          caches.match(request).then((r) => r || caches.match('./index.html'))
        )
    );
    return;
  }

  // Cache-first for static assets (JS, CSS, fonts, WASM, images).
  // This includes the 42MB sprout.wasm — once fetched, it's served from cache.
  const path = url.pathname;
  if (
    path.startsWith('/webui/assets/') ||
    path.startsWith('/assets/') ||
    path.startsWith('/webui/wasm/') ||
    path.endsWith('.wasm') ||
    path.endsWith('.js') ||
    path.endsWith('.css') ||
    path.endsWith('.png') ||
    path.endsWith('.svg') ||
    path.endsWith('.ico') ||
    path.endsWith('.woff2')
  ) {
    event.respondWith(
      caches.match(request).then((cached) => {
        if (cached) return cached;
        return fetch(request).then((resp) => {
          if (resp.ok) {
            const copy = resp.clone();
            caches.open(ASSET_CACHE).then((cache) => cache.put(request, copy));
          }
          return resp;
        });
      })
    );
    return;
  }

  // Everything else (API calls, etc.) — network only, no interception.
});
