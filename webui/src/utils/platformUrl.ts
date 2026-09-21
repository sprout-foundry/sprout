/**
 * SP-016 P0.3 — platform URL builder.
 *
 * The editor's account-surface exits ("← Dashboard" back-link, escalation
 * "View task on platform" links, avatar-menu exits) must not self-loop
 * into this daemon's own SPA on a Fly workspace (Mode B): relative hrefs
 * resolve against the workspace origin and land back in the editor.
 * When the host knows the platform base URL (bootstrap `platformURL`,
 * from the daemon's SPROUT_PLATFORM_URL env or the platform's cloud
 * bootstrap), exits build absolute URLs against it. When the host does
 * not know it, the relative path is returned verbatim — today's behavior.
 */

import { getPlatformURL } from '../bootstrapAdapter';

/**
 * Resolve the exit URL for a platform SPA path (e.g. '/tasks/abc').
 *
 * @param platformPath  A platform-SPA path, with or without leading slash.
 * @returns The absolute URL `platformURL + path` when a platform base was
 *          resolved at bootstrap; otherwise the path verbatim (existing
 *          relative-exit behavior). Trailing slashes on the base are
 *          stripped so paths append cleanly.
 */
export function platformHref(platformPath: string): string {
  const base = getPlatformURL();
  if (!base) {
    return platformPath;
  }
  const cleanBase = base.replace(/\/+$/, '');
  const normalizedPath = platformPath.startsWith('/') ? platformPath : `/${platformPath}`;
  return cleanBase + normalizedPath;
}
