/**
 * SP-016 P0.3 — platform URL builder.
 *
 * The editor's account-surface exits ("← Dashboard" back-link, escalation
 * "View task on platform" links, avatar-menu exits) must not self-loop
 * into this daemon's own SPA on a Fly workspace (Mode B): relative hrefs
 * resolve against the workspace origin and land back in the editor.
 * When the host's transport carries a platform base URL (its outward
 * platform surface), exits build absolute URLs against it. When the host
 * has no platform surface — the local build, or a hosted build that did not
 * provide one — the relative path is returned verbatim (today's behavior).
 *
 * The base is host-provided data read from the active host's transport, not
 * fetched: this module is part of the package's import graph, so it must not
 * reach the bootstrap fetch. A caller may pass the host whose transport to
 * read (the React path, where the host is at hand); otherwise the active
 * host's transport is consulted. The entry point records the same host
 * instance on the accessor and the provider, so both channels agree.
 *
 * This module is part of the platform implementation (the internal host
 * module): `platformHref` resolves a platform page, so it must not be imported
 * from outside the host tree. Repository NAMING (repoSlug/repoName/
 * githubRepoSlug) is host-agnostic and lives in `host/repoName.ts`, exported
 * from the public entry.
 */

import { outwardURL } from './outwardURL';
import { githubRepoSlug } from './repoName';
import type { HostTransport } from './types';

/**
 * Resolve the exit URL for a platform SPA path (e.g. '/tasks/abc').
 *
 * @param platformPath  A platform-SPA path, with or without leading slash.
 * @param transport     The host transport to read the platform base from;
 *                      defaults to the active host's transport.
 * @returns The absolute URL `platformURL + path` when the host supplies a
 *          platform base; otherwise the path verbatim (existing
 *          relative-exit behavior).
 */
export function platformHref(platformPath: string, transport?: HostTransport): string {
  return outwardURL(platformPath, transport);
}

/**
 * Platform path of a GitHub repository's hub page, or the dashboard when the
 * repo is unknown or not on GitHub. The route lives in the SPA's hash so the
 * platform's bare-"/" bounce back into the editor never fires; ?from=editor
 * stays in the query where the platform counts editor exits.
 */
export function repoHubPath(repoURL: string | null | undefined): string {
  const slug = githubRepoSlug(repoURL);
  return slug ? `/?from=editor#/repos/${slug}` : '/?from=editor';
}

/**
 * The platform's own full page URL for a route the editor embeds or opens: the
 * platform SPA path with the platform's embed decoration (`?embed=1` plus the
 * route in the hash) applied. Keeps the embed query literal on the host side.
 */
export function platformEmbedPath(route: string): string {
  return platformHref(`/?embed=1#${route}`);
}
