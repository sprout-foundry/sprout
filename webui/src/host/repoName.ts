/**
 * Host-agnostic repository naming: parse an arbitrary git URL (GitHub, GitLab,
 * Bitbucket, scp- or https-style) into a display name. These helpers carry no
 * platform concept — they name a repository, not a platform page — so they are
 * part of the public host entry alongside the contract, and a component may
 * import them from `host/index`.
 */

/**
 * The repository path for display — "owner/name", or "group/sub/name" on
 * hosts that nest repos (GitLab) — from an https or scp-style git URL on any
 * host, otherwise null.
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
