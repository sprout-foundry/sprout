/**
 * Resolve a path against the host's outward platform surface.
 *
 * The transport's `platformURL` is the base of everything outward — the
 * platform's SPA pages, account pages and its account API (`/user/…`,
 * `/notifications`, `/billing/status`). Sprout builds an absolute URL when a
 * host supplies a base, and returns the path verbatim otherwise (same-origin,
 * today's behavior) — so a service holds no hardcoded platform origin.
 *
 * Host-agnostic (it only prepends the host's own declared base), so it lives on
 * the public host entry and services may import it directly.
 */
import { getActiveHost } from './accessor';
import type { HostTransport } from './types';

export function outwardURL(path: string, transport?: HostTransport): string {
  const base = transport ? transport.platformURL : getActiveHost()?.transport.platformURL;
  if (!base) return path;
  const cleanBase = base.replace(/\/+$/, '');
  const normalized = path.startsWith('/') ? path : `/${path}`;
  return cleanBase + normalized;
}
