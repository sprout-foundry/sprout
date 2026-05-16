import { debugLog } from '../utils/log';
import { getAdapter } from './apiAdapter';

export const WEBUI_CLIENT_ID_HEADER = 'X-Sprout-Client-ID';
export const WEBUI_CLIENT_ID_QUERY_PARAM = 'client_id';
const WEBUI_CLIENT_ID_STORAGE_KEY = 'sprout.webuiClientId';
const WEBUI_WORKSPACE_PATH_STORAGE_KEY = 'sprout.workspaceTabPath';

/**
 * When the app is loaded via the SSH proxy path (e.g. /ssh/{key}/) the server
 * injects `window.SPROUT_PROXY_BASE` so that API and WebSocket calls are routed
 * through the local server's reverse proxy instead of hitting a different port.
 */
export function getProxyBase(): string {
  if (typeof window === 'undefined') return '';
  return (window as unknown as Record<string, string>).SPROUT_PROXY_BASE || '';
}

/**
 * Returns the localStorage key to use for persisting the workspace path.
 * For SSH proxy pages the key is scoped to the proxy base so that different
 * SSH host/path sessions do not bleed into each other or into the local UI.
 */
function workspacePathStorageKey(): string {
  const proxyBase = getProxyBase();
  if (proxyBase) {
    return `${WEBUI_WORKSPACE_PATH_STORAGE_KEY}:${proxyBase}`;
  }
  return WEBUI_WORKSPACE_PATH_STORAGE_KEY;
}

/**
 * Returns the per-tab client ID used to isolate server-side state (workspace,
 * agent session, terminal sessions, WebSocket events) between browser tabs.
 *
 * Uses sessionStorage exclusively so that each tab gets a unique client_id.
 * sessionStorage survives normal page reloads (F5) within the same tab but
 * is isolated across tabs — fixing the bug where all tabs shared one context.
 *
 * Cross-origin cookie persistence:
 * When the WebUI (Cloudflare Pages) and API (tunnel) are on different domains,
 * the server sets a `sprout_client_id` cookie. On page reload, this function
 * reads the cookie as a fallback so the client resumes the same server-side
 * session instead of generating a new client_id and losing all state.
 * Without this, every reload would create a fresh session.
 *
 * For Chrome tab-discard recovery:
 * - The workspace path is persisted separately in localStorage so the tab
 *   can restore the correct workspace after discard (chat history is lost but
 *   workspace is correct).
 * - The client_id is regenerated (fresh server context) because the old one
 *   may have been cleaned up by the server's idle-context gc.
 */
export function getWebUIClientId(): string {
  if (typeof window === 'undefined') {
    return 'default';
  }

  const existing = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
  if (existing) {
    return existing;
  }

  // Cross-origin fallback: read client ID from the server-set cookie.
  // This preserves the session across page reloads when the WebUI and API
  // are on different origins (Cloudflare Pages + tunnel).
  const cookieValue = readCookie(clientIDCookieName);
  if (cookieValue && cookieValue !== 'default') {
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, cookieValue);
    return cookieValue;
  }

  // Generate a new ID — each tab gets its own unique client_id.
  const generated =
    typeof window.crypto?.randomUUID === 'function'
      ? window.crypto.randomUUID()
      : `webui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);

  // Clean up any stale client_id from localStorage to avoid future confusion.
  window.localStorage.removeItem(WEBUI_CLIENT_ID_STORAGE_KEY);

  return generated;
}

// Cookie name used by the server for cross-origin session persistence.
// Must match the server's clientIDCookieName constant.
const clientIDCookieName = 'sprout_client_id';

/**
 * Read a cookie value by name from document.cookie.
 * Returns the decoded value or null if not found.
 */
function readCookie(name: string): string | null {
  if (typeof document === 'undefined') return null;
  const cookies = document.cookie.split(';');
  for (const cookie of cookies) {
    const [key, ...rest] = cookie.trim().split('=');
    if (key.trim() === name) {
      const value = rest.join('=').trim();
      if (!value) return null;
      try {
        return decodeURIComponent(value);
      } catch {
        return value;
      }
    }
  }
  return null;
}

/**
 * Persist the workspace path for Chrome tab-discard recovery.
 * Called whenever the workspace changes (via the workspace-changed listener).
 * Stored in localStorage (per-origin) so it survives sessionStorage clearing
 * when Chrome discards a background tab.
 */
export function persistTabWorkspacePath(workspacePath: string): void {
  if (typeof window === 'undefined' || !workspacePath) {
    return;
  }
  try {
    window.localStorage.setItem(workspacePathStorageKey(), workspacePath);
  } catch (err) {
    debugLog('[persistTabWorkspacePath] failed to persist workspace path:', err);
  }
}

/**
 * Retrieve the last-known workspace path for this origin.
 * Used after a tab discard to auto-restore the correct workspace
 * even though the client_id (and thus server context) is new.
 */
export function getTabWorkspacePath(): string {
  if (typeof window === 'undefined') {
    return '';
  }
  try {
    return window.localStorage.getItem(workspacePathStorageKey()) || '';
  } catch (err) {
    debugLog('[getTabWorkspacePath] failed to read workspace path:', err);
    return '';
  }
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
 * When running via the SSH proxy, parse the host alias from SPROUT_PROXY_BASE.
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
  // If a cloud adapter is installed, delegate all requests through it.
  // The adapter handles URL rewriting, synthetic responses, and credentials.
  // clientFetch sets the client ID header; the adapter also sets it internally
  // (double-set is intentional for safety — same value, Headers.set overwrites).
  const adapter = getAdapter();
  if (adapter) {
    debugLog('[clientFetch] routing through adapter:', adapter.name);
    const headers = new Headers(init?.headers || {});
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
    return adapter.fetch(input, { ...init, headers });
  }

  // Local mode: existing behavior unchanged
  const headers = new Headers(init?.headers || {});
  headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
  // If we're running behind the SSH proxy, prefix relative API paths so they
  // route through the local server's reverse proxy to the remote backend.
  const proxyBase = getProxyBase();
  let url: RequestInfo | URL = input;
  if (proxyBase && typeof url === 'string' && url.startsWith('/')) {
    url = proxyBase + url;
  }
  return fetch(url, { ...init, headers, credentials: 'include' });
}
