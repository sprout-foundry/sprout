/**
 * clientIdCookie — persist and recover the per-tab WebUI client ID via cookie.
 *
 * The WebUI uses a per-tab `client_id` (UUID) to isolate server-side context
 * (workspace, agent session, terminal sessions, WebSocket events) between
 * browser tabs. The server already sets a `sprout_client_id` cookie on every
 * response (see pkg/webui/client_context.go:24, `clientIDCookieName`); this
 * module centralises cookie read/write on the client side so:
 *
 *   1. On WebUI startup we read the cookie. If it carries a UUID we adopt
 *      it as our client_id, which makes the same browser tab resume the
 *      same server-side context across reloads — fixing the bug where
 *      hard-reloading after a worktree switch dropped the user back into
 *      `~`.
 *   2. When we mint a fresh client_id (first visit, no cookie yet) we
 *      immediately mirror it into the cookie so a subsequent reload still
 *      finds it via path 1.
 *   3. Tests / logout can call `clearClientIdCookie()` to start clean.
 *
 * This module is deliberately SSR-safe and tolerant of missing `document`
 * (so it can be imported from shared utilities that may be evaluated in
 * node-side tests).
 */

const CLIENT_ID_COOKIE_NAME = 'sprout_client_id';

// `path=/` makes the cookie readable on every route in the app. SameSite=Lax
// is the right default for a tab-scoped session identifier — we want the
// cookie to be sent on top-level navigations from the same site but not on
// cross-site subresource requests. We do NOT set Secure because the WebUI
// may be served over plain http in local dev; the server's Set-Cookie
// already upgrades Secure when the connection is https.
const COOKIE_DEFAULTS = 'path=/; SameSite=Lax; max-age=2592000'; // 30 days

/**
 * UUIDv4 pattern exported for tests. isLikelyClientId() is deliberately
 * more permissive (accepts any non-empty non-"default" value) so it
 * round-trips whatever ID scheme the server uses. Tests can use this
 * regex to verify that generated IDs are well-formed UUIDs without
 * coupling to the exact validation path.
 */
const UUID_LIKE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function isLikelyClientId(value: string | null | undefined): value is string {
  if (typeof value !== 'string' || value.length === 0) return false;
  if (value === 'default') return false;
  // Accept any non-empty non-"default" value the server might have
  // written. The server may use UUIDs, timestamps, or its own ID
  // scheme — we deliberately don't enforce a shape here so we always
  // round-trip whatever the server set.
  return true;
}

function hasDocument(): boolean {
  return typeof document !== 'undefined' && typeof document.cookie === 'string';
}

/**
 * Read the current `sprout_client_id` cookie value. Returns null when
 * the cookie is absent, malformed, or we're running outside a browser.
 *
 * Note: this only works for same-origin deployments. In cross-origin
 * setups (Cloudflare Pages + tunnel) the browser blocks `document.cookie`
 * access and the server's Set-Cookie header is the only source of truth;
 * the request-side response-header path in `clientSession.ts` covers that.
 */
export function readClientIdCookie(): string | null {
  if (!hasDocument()) return null;

  const cookies = document.cookie.split(';');
  for (const cookie of cookies) {
    const trimmed = cookie.trim();
    if (!trimmed.startsWith(`${CLIENT_ID_COOKIE_NAME}=`)) continue;
    const raw = trimmed.slice(CLIENT_ID_COOKIE_NAME.length + 1);
    if (!raw) return null;
    try {
      const decoded = decodeURIComponent(raw);
      return isLikelyClientId(decoded) ? decoded : null;
    } catch {
      return isLikelyClientId(raw) ? raw : null;
    }
  }
  return null;
}

/**
 * Write a client_id to the cookie. If `value` is null/empty, the cookie
 * is removed instead. Safe to call during SSR (no-op outside a browser).
 *
 * `Secure` is added opportunistically: it's a no-op on http localhost but
 * the browser enforces it on https. We do NOT rely on it for the local
 * dev case where the WebUI is served over plain http.
 */
export function writeClientIdCookie(value: string | null): void {
  if (!hasDocument()) return;
  if (value == null || value === '') {
    clearClientIdCookie();
    return;
  }
  if (!isLikelyClientId(value)) return;
  const secure = typeof window !== 'undefined' && window.location?.protocol === 'https:' ? '; Secure' : '';
  document.cookie = `${CLIENT_ID_COOKIE_NAME}=${encodeURIComponent(value)}; ${COOKIE_DEFAULTS}${secure}`;
}

export function clearClientIdCookie(): void {
  if (!hasDocument()) return;
  // Use a past date to delete. Path/domain must match the original write.
  document.cookie = `${CLIENT_ID_COOKIE_NAME}=; path=/; max-age=0; SameSite=Lax`;
}

/**
 * Returns a freshly generated client_id. Uses `crypto.randomUUID()` when
 * available, falls back to a 128-bit-secure-ish string built from
 * `crypto.getRandomValues` and finally a Math.random() fallback for very
 * old environments (vitest's jsdom, ancient test runners).
 */
export function generateClientId(): string {
  if (typeof crypto !== 'undefined') {
    if (typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }
    if (typeof crypto.getRandomValues === 'function') {
      const bytes = new Uint8Array(16);
      crypto.getRandomValues(bytes);
      // Format as UUIDv4 (set version + variant bits) for friendliness in
      // logs; the exact version doesn't matter for our use case.
      bytes[6] = (bytes[6] & 0x0f) | 0x40;
      bytes[8] = (bytes[8] & 0x3f) | 0x80;
      const hex: string[] = [];
      for (let i = 0; i < bytes.length; i++) {
        hex.push((bytes[i] + 0x100).toString(16).slice(1));
      }
      return `${hex.slice(0, 4).join('')}-${hex.slice(4, 6).join('')}-${hex
        .slice(6, 8)
        .join('')}-${hex.slice(8, 10).join('')}-${hex.slice(10, 16).join('')}`;
    }
  }
  return `webui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

/**
 * Get an existing client_id from the cookie or mint a new one and persist
 * it. This is the primary entry point for "what's my client_id?" at app
 * startup — the cookie is the source of truth so a reload resumes the
 * same server-side session.
 *
 * Optional `existing` argument: if the caller already has a client_id
 * (e.g. recovered from sessionStorage or a response header), pass it
 * here. The cookie will be reconciled with that value (write the cookie
 * if it doesn't match) without minting a new one.
 */
export function getOrCreateClientIdCookie(existing?: string | null): string {
  const fromCookie = readClientIdCookie();
  if (fromCookie) {
    // Cookie is the source of truth — if the caller has a different
    // ID (e.g. stale sessionStorage), trust the cookie and let the
    // caller reconcile via the return value.
    return fromCookie;
  }

  if (existing && isLikelyClientId(existing)) {
    writeClientIdCookie(existing);
    return existing;
  }

  const generated = generateClientId();
  writeClientIdCookie(generated);
  return generated;
}

/**
 * Re-export of the cookie name for tests / debugging.
 */
export const CLIENT_ID_COOKIE_NAME_FOR_TESTS = CLIENT_ID_COOKIE_NAME;
export const UUID_LIKE_PATTERN_FOR_TESTS = UUID_LIKE;
