export const WEBUI_CLIENT_ID_HEADER = 'X-Ledit-Client-ID';
export const WEBUI_CLIENT_ID_QUERY_PARAM = 'client_id';
const WEBUI_CLIENT_ID_STORAGE_KEY = 'ledit.webuiClientId';

/**
 * When the app is loaded via the SSH proxy path (e.g. /ssh/{key}/) the server
 * injects `window.LEDIT_PROXY_BASE` so that API and WebSocket calls are routed
 * through the local server's reverse proxy instead of hitting a different port.
 */
export function getProxyBase(): string {
  if (typeof window === 'undefined') return '';
  return (window as any).LEDIT_PROXY_BASE || '';
}

export function getWebUIClientId(): string {
  if (typeof window === 'undefined') {
    return 'default';
  }

  // Check sessionStorage first (survives refresh/reload within same tab).
  // Always sync to localStorage so the client_id survives tab discard,
  // which clears sessionStorage but preserves localStorage.
  let existing = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
  if (existing) {
    window.localStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, existing);
    return existing;
  }

  // SessionStorage is empty — fall back to localStorage (survives tab discard).
  // When Chrome discards/freezes a tab, sessionStorage is cleared but
  // localStorage is preserved. This allows the same client_id to be
  // recovered so server-side context (chat history, terminal sessions)
  // can be reattached.
  // NOTE: This means if the user opens a second tab at the same origin,
  // both tabs will share the same client_id (and server context). This is
  // acceptable because ledit is typically used in a single tab, and sharing
  // context after tab discard is better than losing it entirely.
  const persisted = window.localStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
  if (persisted) {
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, persisted);
    return persisted;
  }

  // First ever load — generate new ID and save to both storage locations
  const generated =
    typeof window.crypto?.randomUUID === 'function'
      ? window.crypto.randomUUID()
      : `webui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);
  window.localStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);
  return generated;
}

export function appendClientIdToUrl(input: string): string {
  if (typeof window === 'undefined') {
    return input;
  }

  const url = new URL(input, window.location.origin);
  url.searchParams.set(WEBUI_CLIENT_ID_QUERY_PARAM, getWebUIClientId());
  if (url.origin === window.location.origin) {
    return `${url.pathname}${url.search}${url.hash}`;
  }
  return url.toString();
}

/**
 * When running via the SSH proxy, parse the host alias from LEDIT_PROXY_BASE.
 * The session key embedded in the proxy base has the form "{hostAlias}::{remotePath}".
 * Returns null when not in a proxy session.
 */
export function getSSHProxyContext(): { hostAlias: string; remotePath: string } | null {
  const proxyBase = getProxyBase(); // e.g. "/ssh/mac-mini%3A%3A%24HOME"
  if (!proxyBase) return null;
  const match = proxyBase.match(/^\/ssh\/([^/]+)/);
  if (!match) return null;
  const sessionKey = decodeURIComponent(match[1]); // "mac-mini::$HOME"
  const idx = sessionKey.indexOf('::');
  if (idx < 0) return null;
  return {
    hostAlias: sessionKey.slice(0, idx),
    remotePath: sessionKey.slice(idx + 2),
  };
}

export async function clientFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers || {});
  headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
  // If we're running behind the SSH proxy, prefix relative API paths so they
  // route through the local server's reverse proxy to the remote backend.
  const proxyBase = getProxyBase();
  let url: RequestInfo | URL = input;
  if (proxyBase && typeof url === 'string' && url.startsWith('/')) {
    url = proxyBase + url;
  }
  return fetch(url, { ...init, headers });
}

