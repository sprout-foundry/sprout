/**
 * The bug-report URL builder — the pure half that lives in the host tree.
 *
 * The host tree must not import the bootstrap adapter (importing the host must
 * fetch nothing), so this module is pure: it takes the environment as an
 * argument and never reads bootstrap. The environment collection (version from
 * the build/bootstrap, mode from the host) lives outside the host tree in
 * `services/reportBug.ts`, which passes the collected values in.
 *
 * The public repository URL is a single constant here (`SPROUT_REPO_URL`),
 * consumed by the builder, the host's `reportBug` intent, the buttons and the
 * `sprout bug` CLI (which mirrors it in Go, pkg/repo).
 */

/** The public repository — the single source of truth for its URL. */
export const SPROUT_REPO_URL = 'https://github.com/sprout-foundry/sprout';

/** The new-issue endpoint on the public repository. */
export const SPROUT_ISSUES_NEW_URL = `${SPROUT_REPO_URL}/issues/new`;

/**
 * GitHub caps a URL around 8 KB; keep the whole URL comfortably under that by
 * truncating the body, never the title or the labels.
 */
export const MAX_ISSUE_URL_LENGTH = 7000;

export type SproutMode = 'local daemon' | 'in-browser' | 'desktop/studio' | 'hosted';

export interface BugReportEnvironment {
  /** The running build's version (from the build/bootstrap, never a literal). */
  version: string;
  /** How Sprout is running. */
  mode: SproutMode;
  /** The operating system the browser reports. */
  os: string;
  /** The browser user agent. */
  browser: string;
}

/** The short template the issue body starts from. */
export const BUG_REPORT_TEMPLATE = [
  '### What happened',
  '',
  '',
  '### What you expected',
  '',
  '',
  '### Steps to reproduce',
  '',
  '1. ',
  '',
].join('\n');

/** The default issue title. */
export const BUG_REPORT_TITLE = 'Bug: ';

/** The environment block appended to the template. */
export function formatBugReportBody(env: BugReportEnvironment): string {
  return [
    BUG_REPORT_TEMPLATE,
    '### Environment',
    '',
    `- sprout version: ${env.version}`,
    `- mode: ${env.mode}`,
    `- OS: ${env.os}`,
    `- browser: ${env.browser}`,
    '',
  ].join('\n');
}

/**
 * Build the prefilled GitHub new-issue URL:
 * `/issues/new?title=…&body=…&labels=bug`. The body is truncated so the whole
 * URL stays under `MAX_ISSUE_URL_LENGTH`.
 */
export function buildBugReportURLFromEnv(env: BugReportEnvironment, title: string = BUG_REPORT_TITLE): string {
  const body = formatBugReportBody(env);

  const build = (b: string): string => {
    const params = new URLSearchParams();
    params.set('title', title);
    params.set('body', b);
    params.set('labels', 'bug');
    return `${SPROUT_ISSUES_NEW_URL}?${params.toString()}`;
  };

  let url = build(body);
  if (url.length > MAX_ISSUE_URL_LENGTH) {
    // Truncate only the body; the title/labels/endpoint are fixed. URL
    // encoding inflates the body, so trim against the *encoded* length:
    // shrink the raw body until the encoded URL fits.
    const marker = '\n\n…(truncated)';
    const fixed = build('').length; // endpoint + title + labels + separators
    let keep = Math.max(0, MAX_ISSUE_URL_LENGTH - fixed - marker.length);
    for (;;) {
      url = build(body.slice(0, keep) + marker);
      if (url.length <= MAX_ISSUE_URL_LENGTH || keep === 0) break;
      keep = Math.max(0, keep - Math.max(1, Math.ceil((url.length - MAX_ISSUE_URL_LENGTH) / 3)));
    }
  }
  return url;
}
