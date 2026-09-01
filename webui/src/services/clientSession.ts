import { debugLog } from '../utils/log';
import { getAdapter } from './apiAdapter';

export const WEBUI_CLIENT_ID_HEADER = 'X-Sprout-Client-ID';
export const WEBUI_CLIENT_ID_QUERY_PARAM = 'client_id';
const WEBUI_CLIENT_ID_STORAGE_KEY = 'sprout.webuiClientId';
const WEBUI_WORKSPACE_PATH_STORAGE_KEY = 'sprout.workspaceTabPath';
const WINDOW_NAME_PREFIX = 'sproutClientId:';

/**
 * Read the per-tab client ID from window.name.
 *
 * window.name is scoped to the browsing context (tab/window): it survives
 * page reloads and Chrome tab-discards (which clear sessionStorage), but is
 * NOT shared between windows. That makes it the ideal middle tier between
 * sessionStorage (per-tab, lost on discard) and the sprout_client_id cookie
 * (survives everything, but shared by all windows of the origin).
 *
 * Returns null when window.name is unset or holds a value we did not write
 * (some applications stash cross-navigation data in window.name — we never
 * clobber it).
 */
function readClientIdFromWindowName(): string | null {
  if (typeof window === 'undefined' || !window.name) return null;
  if (!window.name.startsWith(WINDOW_NAME_PREFIX)) return null;
  const id = window.name.slice(WINDOW_NAME_PREFIX.length).trim();
  return id || null;
}

/**
 * Persist the client ID into window.name so this tab can recover it after a
 * sessionStorage loss (reload in a discarded tab). Skips writing when
 * window.name holds a foreign value.
 */
function writeClientIdToWindowName(id: string): void {
  if (typeof window === 'undefined' || !id) return;
  if (window.name && !window.name.startsWith(WINDOW_NAME_PREFIX)) return;
  try {
    window.name = WINDOW_NAME_PREFIX + id;
  } catch (err) {
    debugLog('[writeClientIdToWindowName] failed:', err);
  }
}

/**
 * When the app is loaded via the SSH proxy path (e.g. /ssh/{key}/) the server
 * injects `window.SPROUT_PROXY_BASE` so that API and WebSocket calls are routed
 * through the local server's reverse proxy instead of hitting a different port.
 */
export function getProxyBase(): string {
  if (typeof window === 'undefined') return '';
  return window.SPROUT_PROXY_BASE || '';
}

/**
 * Returns the localStorage key to use for persisting the workspace path.
 * For SSH proxy pages the key is scoped to the proxy base so that different
 * SSH host/path sessions do not bleed into each other or into the local UI.
 *
 * The key is ALSO scoped per browsing context (window): the un-suffixed
 * `sprout.workspaceTabPath` is shared by every window of this origin, so two
 * windows pointed at different workspaces kept overwriting each other's
 * path — on next startup, whichever window booted last would silently
 * restore the other's workspace. We suffix with the browsing-context token
 * from window.name when available (stable across reloads/discards, private
 * to the window), falling back to the shared legacy key only when window
 * has no usable marker (first-party popups that reuse window.name for their
 * own data, SSR, tests).
 */
function workspacePathStorageKey(): string {
  const proxyBase = getProxyBase();
  const suffixes: string[] = [];
  if (proxyBase) {
    suffixes.push(proxyBase);
  }
  const browsingContextToken = readClientIdFromWindowName();
  if (browsingContextToken) {
    suffixes.push(browsingContextToken);
  }
  if (suffixes.length === 0) {
    return WEBUI_WORKSPACE_PATH_STORAGE_KEY;
  }
  return `${WEBUI_WORKSPACE_PATH_STORAGE_KEY}:${suffixes.join(':')}`;
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
    writeClientIdToWindowName(existing);
    return existing;
  }

  // Browsing-context recovery (window.name). This must run BEFORE the cookie
  // fallback: the sprout_client_id cookie is shared by every window of this
  // origin, so a second window would otherwise adopt the first window's
  // client ID and both would share one server-side context (workspace,
  // terminal, chats) — the "two windows pointing at different folders break
  // each other" bug. window.name is per-window, so it can only ever point
  // back at this window's own previous identity.
  const windowNameId = readClientIdFromWindowName();
  if (windowNameId) {
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, windowNameId);
    return windowNameId;
  }

  // Cross-origin fallback: read client ID from the server-set cookie.
  // This preserves the session across page reloads when the WebUI and API
  // are on different origins (Cloudflare Pages + tunnel) AND no other
  // window has claimed that ID for a different workspace. See
  // claimOrGenerateClientId for why a naive adoption is unsafe.
  const cookieValue = readCookie(clientIDCookieName);
  if (cookieValue && cookieValue !== 'default') {
    const claimed = claimOrGenerateClientId(cookieValue);
    if (claimed !== cookieValue) {
      // Another live window owns the cookie ID — use the freshly generated
      // one so this window gets its own server context.
      window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, claimed);
      writeClientIdToWindowName(claimed);
      return claimed;
    }
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, cookieValue);
    writeClientIdToWindowName(cookieValue);
    return cookieValue;
  }

  // Generate a new ID — each tab gets its own unique client_id. Registering
  // the claim matters even here: once this window starts sending the ID, the
  // server sets the shared cookie to it, and without a registry entry a
  // second window would later adopt it from the cookie.
  const generated = generateClientId();
  window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);
  writeClientIdToWindowName(generated);
  claimOrGenerateClientId(generated);

  // Clean up any stale client_id from localStorage to avoid future confusion.
  window.localStorage.removeItem(WEBUI_CLIENT_ID_STORAGE_KEY);

  return generated;
}

function generateClientId(): string {
  return typeof window.crypto?.randomUUID === 'function'
    ? window.crypto.randomUUID()
    : `webui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

// ── Cross-window ownership registry ─────────────────────────────
//
// The sprout_client_id cookie is per-origin (all windows share it), but the
// server-side context it addresses is per-window. claimOrGenerateClientId
// answers: "is the cookie's ID safe for THIS window to use, or does another
// live window own it?" A registry entry is written whenever a window adopts
// or generates an ID, and refreshed on a 15s heartbeat so a closed window's
// claim expires quickly. sessionStorage is never trusted for ownership —
// the registry lives in localStorage so every window can see every claim.

const CLAIM_REGISTRY_KEY = 'sprout.webuiClientId.claims';
const CLAIM_TTL_MS = 30_000;

interface ClaimEntry {
  /** Owner window's client ID. */
  id: string;
  /** Last heartbeat timestamp (ms). */
  t: number;
}

/** Parse and GC the claim registry. Returns the live entries keyed by ID. */
function readClaimRegistry(): Map<string, ClaimEntry> {
  const live = new Map<string, ClaimEntry>();
  try {
    const raw = window.localStorage.getItem(CLAIM_REGISTRY_KEY);
    if (!raw) return live;
    const parsed = JSON.parse(raw) as Record<string, ClaimEntry>;
    const now = Date.now();
    for (const [id, entry] of Object.entries(parsed)) {
      if (!entry || typeof entry.t !== 'number') continue;
      if (now - entry.t > CLAIM_TTL_MS) continue;
      live.set(id, entry);
    }
  } catch {
    // Malformed registry — treat as empty.
  }
  return live;
}

/**
 * Attempt to claim `cookieId` for this window. Returns the cookie ID when
 * free (or owned by this window from an earlier visit), or a freshly
 * generated ID when another live window has claimed it. As a side effect,
 * registers/refreshes this window's claim on the returned ID.
 */
function claimOrGenerateClientId(cookieId: string): string {
  let claimedId = cookieId;
  const registry = readClaimRegistry();
  const entry = registry.get(cookieId);
  if (entry && entry.id !== '') {
    // The ID is owned by some live window. sessionStorage/window.name for
    // this window is empty (we would have returned earlier), so that owner
    // is a different window — mint a new ID instead of sharing.
    claimedId = generateClientId();
  }

  registry.set(claimedId, { id: claimedId, t: Date.now() });
  writeClaimRegistry(registry);
  startClaimHeartbeat();
  return claimedId;
}

function writeClaimRegistry(registry: Map<string, ClaimEntry>): void {
  try {
    const obj: Record<string, ClaimEntry> = {};
    for (const [id, entry] of registry) {
      if (id === entry.id) obj[id] = entry;
    }
    window.localStorage.setItem(CLAIM_REGISTRY_KEY, JSON.stringify(obj));
  } catch (err) {
    debugLog('[writeClaimRegistry] failed:', err);
  }
}

let claimHeartbeatStarted = false;
/**
 * Refresh this window's claim every 10s so other windows see it as live.
 * The registry GC (CLAIM_TTL_MS) then reaps entries whose window closed.
 */
function startClaimHeartbeat(): void {
  if (claimHeartbeatStarted || typeof window === 'undefined') return;
  claimHeartbeatStarted = true;
  const beat = () => {
    const id = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    if (!id) return;
    const registry = readClaimRegistry();
    registry.set(id, { id, t: Date.now() });
    writeClaimRegistry(registry);
  };
  window.setInterval(beat, 10_000);
  window.addEventListener('pagehide', (event) => {
    // Only release the claim on a true unload. When persisted=true the page
    // may return from bfcache (or a tab discard) — releasing would let
    // another window adopt this ID while the tab is parked. The TTL
    // backstop reaps the claim if the tab never comes back.
    if (event.persisted) return;
    const id = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    if (!id) return;
    try {
      const registry = readClaimRegistry();
      registry.delete(id);
      writeClaimRegistry(registry);
    } catch {
      // Non-fatal.
    }
  });
}

// Cookie name used by the server for cross-origin session persistence.
// Must match the server's clientIDCookieName constant.
const clientIDCookieName = 'sprout_client_id';

/**
 * Read a cookie value by name from document.cookie.
 * Returns the decoded value or null if not found.
 */
function readCookie(name: string): string | null {
  // Read via window.document (not the ambient document) so the cookie source
  // matches the window whose storage this module is scoping — relevant in
  // tests and non-DOM embeddings where the two can differ.
  if (typeof window === 'undefined' || !window.document) return null;
  const cookies = window.document.cookie.split(';');
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

/**
 * Sync the client ID from an API response header into sessionStorage.
 *
 * In a cross-origin deployment (WebUI on Cloudflare Pages, API on a tunnel),
 * JavaScript cannot read cookies from a different origin (document.cookie is
 * origin-scoped). The server echoes the resolved client ID in the
 * X-Sprout-Client-ID response header (exposed via Access-Control-Expose-Headers).
 *
 * This function reads that header and writes it to sessionStorage so that
 * subsequent page loads / reloads can pick up the same client ID without
 * needing to read the cross-origin cookie directly.
 *
 * This is the "header round-trip" pattern:
 *   1. Browser sends X-Sprout-Client-ID header (from sessionStorage) or cookie
 *   2. Server resolves the client ID, sets cookie + X-Sprout-Client-ID response header
 *   3. Client reads X-Sprout-Client-ID from response and writes it to sessionStorage
 *   4. On page reload, sessionStorage has the client ID (or cookie fallback for same-origin)
 */
function syncClientIdFromResponse(response: Response): void {
  const headerValue = response.headers.get(WEBUI_CLIENT_ID_HEADER);
  if (!headerValue) return;

  const existing = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
  if (existing !== headerValue) {
    // Only overwrite sessionStorage if the response header has a non-default value.
    // This prevents the server's default from overwriting a user-generated UUID.
    if (headerValue !== 'default') {
      window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, headerValue);
    }
  }
}

/**
 * Resolve the client ID for this session.
 *
 * In cross-origin mode (Cloudflare Pages + tunnel), this may make an initial
 * request to the server to recover a previously-stored client ID from the
 * response header, since document.cookie is inaccessible cross-origin.
 *
 * Returns a promise that resolves to the client ID string. The resolved value
 * is always cached into sessionStorage, so subsequent calls to
 * getWebUIClientId() (synchronous) will find it there.
 */
let _resolvedClientId: Promise<string> | null = null;
export function resolveWebUIClientId(): Promise<string> {
  if (_resolvedClientId) return _resolvedClientId;

  _resolvedClientId = (async (): Promise<string> => {
    // Fast path: sessionStorage already has a value (e.g. from a prior page
    // visit or from a previous syncClientIdFromResponse call during this session).
    const existing = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    if (existing && existing !== 'default') {
      writeClientIdToWindowName(existing);
      return existing;
    }

    // Browsing-context recovery via window.name — per-window, survives tab
    // discard. Checked before the cookie for the same reason as
    // getWebUIClientId: the cookie is shared across windows, window.name is
    // not.
    const windowNameId = readClientIdFromWindowName();
    if (windowNameId && windowNameId !== 'default') {
      window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, windowNameId);
      return windowNameId;
    }

    // Try reading the cookie — works for same-origin deployments and when the
    // page is served from the same origin as the API. The claim registry
    // guards against adopting an ID another live window owns (the shared
    // cookie would otherwise fuse two windows into one server context).
    const cookieValue = readCookie(clientIDCookieName);
    if (cookieValue && cookieValue !== 'default') {
      const claimed = claimOrGenerateClientId(cookieValue);
      window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, claimed);
      writeClientIdToWindowName(claimed);
      return claimed;
    }

    // Cross-origin recovery: make a lightweight request to the server.
    // The server will read the sprout_client_id cookie (which the browser
    // sends automatically with credentials: 'include') and echo the resolved
    // client ID in the X-Sprout-Client-ID response header.
    try {
      const proxyBase = getProxyBase();
      const url = `${proxyBase}/api/query/status`;
      const resp = await fetch(url, {
        method: 'GET',
        credentials: 'include',
        headers: { 'Cache-Control': 'no-store' },
      });
      const echoedId = resp.headers.get(WEBUI_CLIENT_ID_HEADER);
      if (echoedId && echoedId !== 'default') {
        const claimed = claimOrGenerateClientId(echoedId);
        window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, claimed);
        writeClientIdToWindowName(claimed);
        return claimed;
      }
    } catch {
      // Network error — fall through to generate a new ID below.
      debugLog('[resolveWebUIClientId] cross-origin recovery fetch failed');
    }

    // No existing session — generate a new client ID.
    const generated = generateClientId();
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);
    writeClientIdToWindowName(generated);
    claimOrGenerateClientId(generated);
    // Clean up any stale client_id from localStorage to avoid future confusion.
    window.localStorage.removeItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    return generated;
  })();

  return _resolvedClientId;
}

export async function clientFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  // Ensure the client ID is resolved before making any API calls.
  // In cross-origin mode, this performs an initial recovery fetch to get
  // the server's cached client ID from the response header (since
  // document.cookie is inaccessible cross-origin).
  const clientId = await resolveWebUIClientId();

  // If a cloud adapter is installed, delegate all requests through it.
  // The adapter handles URL rewriting, synthetic responses, and credentials.
  // clientFetch sets the client ID header; the adapter also sets it internally
  // (double-set is intentional for safety — same value, Headers.set overwrites).
  const adapter = getAdapter();
  if (adapter) {
    debugLog('[clientFetch] routing through adapter:', adapter.name);
    const headers = new Headers(init?.headers || {});
    headers.set(WEBUI_CLIENT_ID_HEADER, clientId);
    const response = await adapter.fetch(input, { ...init, headers });
    syncClientIdFromResponse(response);
    return response;
  }

  // Local mode: existing behavior unchanged
  const headers = new Headers(init?.headers || {});
  headers.set(WEBUI_CLIENT_ID_HEADER, clientId);
  // If we're running behind the SSH proxy, prefix relative API paths so they
  // route through the local server's reverse proxy to the remote backend.
  const proxyBase = getProxyBase();
  let url: RequestInfo | URL = input;
  if (proxyBase && typeof url === 'string' && url.startsWith('/')) {
    url = proxyBase + url;
  }
  const response = await fetch(url, { ...init, headers, credentials: 'include' });
  syncClientIdFromResponse(response);
  return response;
}
