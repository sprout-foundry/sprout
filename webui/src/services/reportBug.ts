/**
 * Report a bug — the environment collection half, outside the host tree.
 *
 * The public repository URL and the pure URL builder live in the host tree
 * (`host/reportBugURL.ts`, so the host's `reportBug` intent can build the URL
 * without importing the bootstrap adapter). This module collects the running
 * environment — version from the build/bootstrap (never a literal), mode from
 * the host contract, OS and browser from the browser — and delegates to that
 * builder, then re-exports the constants so components and tests have one
 * import site.
 *
 * The prefilled issue body carries only the short template and the environment
 * — never a file path, workspace name, session id, provider key, prompt or
 * model output (this is a public repo).
 */

import { getBootstrapConfig } from '../bootstrapAdapter';
import { isStudioShellSync } from '../config/shell';
import { getActiveHost } from '../host/accessor';
import {
  BUG_REPORT_TITLE,
  buildBugReportURLFromEnv,
  type BugReportEnvironment,
  type SproutMode,
} from '../host/reportBugURL';

export {
  BUG_REPORT_TITLE,
  BUG_REPORT_TEMPLATE,
  MAX_ISSUE_URL_LENGTH,
  SPROUT_ISSUES_NEW_URL,
  SPROUT_REPO_URL,
  formatBugReportBody,
} from '../host/reportBugURL';
export type { BugReportEnvironment, SproutMode } from '../host/reportBugURL';

/**
 * The running mode, derived from the host contract (never a build flag):
 *  - a host whose transport authenticates against a platform is `hosted`;
 *  - otherwise a studio shell is `desktop/studio`;
 *  - otherwise a folder-picker-capable host is a `local daemon`;
 *  - otherwise the browser-only build is `in-browser`.
 */
export function detectSproutMode(): SproutMode {
  const host = getActiveHost();
  if (host?.transport.authMode === 'bearer') return 'hosted';
  if (isStudioShellSync()) return 'desktop/studio';
  if (host?.capabilities.folderPicker) return 'local daemon';
  return 'in-browser';
}

/** The running version from the build/bootstrap (falls back to the dev default). */
export function detectSproutVersion(): string {
  return getBootstrapConfig().buildVersion || 'dev';
}

/** The OS the browser reports, trimmed to a stable first segment. */
export function detectOS(): string {
  if (typeof navigator === 'undefined') return 'unknown';
  const uaData = (navigator as unknown as { userAgentData?: { platform?: string } }).userAgentData;
  const platform = uaData?.platform || navigator.platform || '';
  return platform || 'unknown';
}

/** The browser user agent (never includes the page URL or any user data). */
export function detectBrowser(): string {
  if (typeof navigator === 'undefined') return 'unknown';
  return navigator.userAgent || 'unknown';
}

/** Collect the environment block for a report. Nothing sensitive is read. */
export function collectBugReportEnvironment(): BugReportEnvironment {
  return {
    version: detectSproutVersion(),
    mode: detectSproutMode(),
    os: detectOS(),
    browser: detectBrowser(),
  };
}

export interface BugReportOptions {
  /** Override the environment (tests, and the CLI passing its own values). */
  environment?: BugReportEnvironment;
  /** Override the title (defaults to `Bug: `). */
  title?: string;
}

/**
 * Build the prefilled GitHub new-issue URL:
 * `/issues/new?title=…&body=…&labels=bug`. The body is truncated so the whole
 * URL stays under `MAX_ISSUE_URL_LENGTH`.
 */
export function buildBugReportURL(options: BugReportOptions = {}): string {
  const env = options.environment ?? collectBugReportEnvironment();
  return buildBugReportURLFromEnv(env, options.title ?? BUG_REPORT_TITLE);
}
