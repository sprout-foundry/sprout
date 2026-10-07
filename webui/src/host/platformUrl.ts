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
 * The repository path for display — "owner/name", or "group/sub/name" on
 * hosts that nest repos (GitLab) — from an https or scp-style git URL on any
 * host, otherwise null. Links into the platform's GitHub-only repo pages use
 * githubRepoSlug instead.
 */
export function repoSlug(repoURL: string | null | undefined): string | null {
  if (!repoURL) return null;
  const raw = repoURL.trim();
  let path: string;
  const scp = /^git@[^:]+:(.+)$/.exec(raw);
  if (scp) {
    path = scp[1];
  } else {
    try {
      path = new URL(raw).pathname;
    } catch {
      return null;
    }
  }
  path = path.replace(/^\/+|\/+$/g, '');
  const viewAt = path.indexOf('/-/');
  if (viewAt >= 0) path = path.slice(0, viewAt);
  path = path.replace(/\.git$/, '');
  const segments = path.split('/').filter(Boolean);
  return segments.length >= 2 ? segments.join('/') : null;
}

/** The repository's own name (last path segment), or null. */
export function repoName(repoURL: string | null | undefined): string | null {
  const slug = repoSlug(repoURL);
  return slug ? slug.slice(slug.lastIndexOf('/') + 1) : null;
}

/** "owner/name" for a github.com repository URL, otherwise null. */
export function githubRepoSlug(repoURL: string | null | undefined): string | null {
  if (!repoURL) return null;
  const match = /^(?:https?:\/\/|git@)github\.com[/:]([\w.-]+)\/([\w.-]+?)(?:\.git)?\/?$/i.exec(repoURL.trim());
  return match ? `${match[1]}/${match[2]}` : null;
}
