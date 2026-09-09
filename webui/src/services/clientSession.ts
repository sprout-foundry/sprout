import { debugLog } from '../utils/log';
import { getAdapter } from './apiAdapter';

export const WEBUI_CLIENT_ID_HEADER = 'X-Sprout-Client-ID';
export const WEBUI_CLIENT_ID_QUERY_PARAM = 'client_id';
const WEBUI_CLIENT_ID_STORAGE_KEY = 'sprout.webuiClientId';
const WEBUI_WORKSPACE_PATH_STORAGE_KEY = 'sprout.workspaceTabPath';
const WINDOW_NAME_PREFIX = 'sproutClientId:';

// Cookie name used by the server for cross-origin session persistence.
// Must match the server's clientIDCookieName constant.
const clientIDCookieName = 'sprout_client_id';

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

// ── Live-window ownership oracle (BroadcastChannel) ────────────────
//
// sessionStorage AND window.name are CLONED into popups opened via
// window.open() (Chromium behavior), so a second window boots with the
// first window's client ID already in every storage tier — the claim
// registry in localStorage cannot distinguish "reloaded tab" (keep the
// ID) from "cloned popup" (must mint its own): both see a live claim.
//
// The only reliable oracle is liveness itself: broadcast "who owns id X?"
// and wait briefly for a response. A window mid-reload has no live page,
// so nobody answers and the reloading tab keeps its ID. A popup's opener
// IS live and answers, so the popup mints a fresh ID.

const OWNERSHIP_CHANNEL = 'sprout.webuiClientId.ownership';
const OWNERSHIP_AWAIT_MS = 120;

interface OwnershipReply {
  type: 'who-owns';
  id: string;
  nonce: string;
}
interface OwnershipAnswer {
  type: 'i-own';
  id: string;
  nonce: string;
}

let ownershipChannel: BroadcastChannel | null = null;
let ownershipResponderStarted = false;

function getOwnershipChannel(): BroadcastChannel | null {
  if (typeof window === 'undefined') return null;
  const Ctor = (window as unknown as { BroadcastChannel?: typeof BroadcastChannel }).BroadcastChannel;
  if (!Ctor) return null;
  if (!ownershipChannel) {
    try {
      ownershipChannel = new Ctor(OWNERSHIP_CHANNEL);
    } catch {
      return null;
    }
  }
  return ownershipChannel;
}

/** Answer who-owns probes for OUR id. Idempotent; one listener per window. */
function startOwnershipResponder(): void {
  if (ownershipResponderStarted) return;
  const ch = getOwnershipChannel();
  if (!ch) return;
  ownershipResponderStarted = true;
  ch.addEventListener('message', (ev) => {
    const msg = ev.data as OwnershipReply;
    if (!msg || msg.type !== 'who-owns') return;
    const mine = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    if (mine && mine === msg.id) {
      const answer: OwnershipAnswer = { type: 'i-own', id: mine, nonce: msg.nonce };
      ch.postMessage(answer);
    }
  });
}

/**
 * Returns true when a live window answers for `id` within the await window.
 * Must be called BEFORE this window adopts/announces the id itself.
 */
function isIdOwnedByLiveWindow(id: string): Promise<boolean> {
  return new Promise((resolve) => {
    const ch = getOwnershipChannel();
    if (!ch) {
      resolve(false);
      return;
    }
    let settled = false;
    const finish = (owned: boolean): void => {
      if (settled) return;
      settled = true;
      ch.removeEventListener('message', onAnswerRef.current!);
      resolve(owned);
    };
    const onAnswer = (ev: MessageEvent): void => {
      const msg = ev.data as OwnershipAnswer;
      if (msg && msg.type === 'i-own' && msg.id === id) finish(true);
    };
    const onAnswerRef = { current: onAnswer as ((ev: MessageEvent) => void) | null };
    ch.addEventListener('message', onAnswer);
    const nonce = generateClientId();
    const probe: OwnershipReply = { type: 'who-owns', id, nonce };
    try {
      ch.postMessage(probe);
    } catch {
      finish(false);
      return;
    }
    window.setTimeout(() => finish(false), OWNERSHIP_AWAIT_MS);
  });
}

/**
 * One-shot boot-time identity resolution. Guards against popups opened via
 * window.open(), which inherit the OPENER's sessionStorage and window.name
 * (Chromium clones both into the new browsing context). Storage tiers
 * alone cannot distinguish that popup from a legit reload of the same tab
 * — the claim registry also looks identical (both see a live heartbeat).
 *
 * The BroadcastChannel oracle resolves it: ask "who owns this id?" and a
 * LIVE window answers. A reloading tab has no live page → keeps its id.
 * A cloned popup's opener answers → the popup mints its own id before
 * the app renders.
 *
 * Idempotent: the first caller runs the oracle; later calls are free.
 */
let identityResolved: Promise<void> | null = null;

export function resolveClientIdentity(): Promise<void> {
  if (identityResolved) return identityResolved;
  identityResolved = (async () => {
    if (typeof window === 'undefined') return;
    // NOTE: the ownership responder starts AFTER the probe below — starting
    // it first would make this window answer its own who-owns question
    // (our stored id matches the probe target), breaking reload persistence.

    const stored = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
    if (!stored) {
      // Nothing adopted yet; getWebUIClientId's normal minting path runs
      // later (first real caller). Bring the responder online for it.
      startOwnershipResponder();
      startClaimHeartbeat();
      return;
    }

    // The oracle: does a live window answer for our stored id? Only the
    // opener of a cloned popup does — a reloading tab has no live page.
    const owned = await isIdOwnedByLiveWindow(stored);
    startOwnershipResponder();
    startClaimHeartbeat();

    if (!owned) {
      // Ours alone (reload/discard recovery). Reassert the claim.
      try {
        const registry = readClaimRegistry();
        registry.set(stored, { id: stored, t: Date.now() });
        writeClaimRegistry(registry);
      } catch {
        // best-effort
      }
      return;
    }

    // A live window owns this id — we are a cloned popup. Mint our own.
    const fresh = generateClientId();
    window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, fresh);
    writeClientIdToWindowName(fresh);
    claimOrGenerateClientId(fresh);
    debugLog('[clientSession] inherited client id from opener; minted fresh id', fresh);
  })();
  return identityResolved;
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

  // Browsing-context recovery (window.name). Ownership is NOT re-checked
  // here: a synchronous registry lookup cannot distinguish "my previous
  // page's claim" from "the opener's claim" (window.open popups inherit
  // BOTH sessionStorage and window.name from the opener in Chromium).
  // The async ownership oracle at boot (resolveClientIdentity, wired in
  // index.tsx) is the layer that detects a live owner and mints a fresh
  // id for cloned popups before the app renders.
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
    // best-effort: malformed registry — treat as empty.
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
      // best-effort: pagehide cleanup; the claim TTL reaps this entry anyway.
    }
  });
}

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
        // best-effort: undecodable cookie value used verbatim.
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
