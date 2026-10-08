/**
 * The bug-report URL builder: prefill, public-repo hygiene (no sensitive
 * fields), and the length cap.
 */

import {
  BUG_REPORT_TITLE,
  MAX_ISSUE_URL_LENGTH,
  SPROUT_ISSUES_NEW_URL,
  SPROUT_REPO_URL,
  buildBugReportURL,
  formatBugReportBody,
} from './reportBug';

const ENV = {
  version: 'v9.9.9',
  mode: 'local daemon' as const,
  os: 'macOS',
  browser: 'Mozilla/5.0 (Macintosh) TestBrowser/1.0',
};

describe('SPROUT_REPO_URL', () => {
  it('is the public repository, not the old repo', () => {
    expect(SPROUT_REPO_URL).toBe('https://github.com/sprout-foundry/sprout');
    expect(SPROUT_ISSUES_NEW_URL).toBe('https://github.com/sprout-foundry/sprout/issues/new');
    expect(SPROUT_ISSUES_NEW_URL).not.toContain('alantheprice');
  });
});

describe('buildBugReportURL', () => {
  it('prefills title, body and the bug label on /issues/new', () => {
    const url = buildBugReportURL({ environment: ENV });
    expect(url.startsWith(`${SPROUT_ISSUES_NEW_URL}?`)).toBe(true);

    const params = new URL(url).searchParams;
    expect(params.get('title')).toBe(BUG_REPORT_TITLE);
    expect(params.get('labels')).toBe('bug');
    const body = params.get('body')!;
    expect(body).toContain('### What happened');
    expect(body).toContain('### What you expected');
    expect(body).toContain('### Steps to reproduce');
  });

  it('fills the environment from the supplied values (version is never a literal)', () => {
    const body = formatBugReportBody(ENV);
    expect(body).toContain('sprout version: v9.9.9');
    expect(body).toContain('mode: local daemon');
    expect(body).toContain('OS: macOS');
    expect(body).toContain('browser: Mozilla/5.0 (Macintosh) TestBrowser/1.0');
  });

  it('never includes sensitive fields (paths, workspace, session, keys, prompts, model output)', () => {
    const url = buildBugReportURL({ environment: ENV });
    const decoded = decodeURIComponent(url);
    for (const needle of ['/Users/', '/home/', 'workspace', 'session', 'sk-', 'api_key', 'token', 'prompt']) {
      expect(decoded.toLowerCase()).not.toContain(needle.toLowerCase());
    }
  });

  it('caps the URL length by truncating the body', () => {
    const huge = { ...ENV, browser: 'X'.repeat(20_000) };
    const url = buildBugReportURL({ environment: huge });
    expect(url.length).toBeLessThanOrEqual(MAX_ISSUE_URL_LENGTH);
    // The title and label survive; only the body is trimmed.
    const params = new URL(url).searchParams;
    expect(params.get('title')).toBe(BUG_REPORT_TITLE);
    expect(params.get('labels')).toBe('bug');
    expect(params.get('body')).toContain('…(truncated)');
  });

  it('caps the URL length even when the body percent-encodes heavily', () => {
    // Newlines encode to %0A (3 bytes each), so trimming against the raw
    // length would overshoot the cap; the builder must trim against the
    // encoded length.
    for (const n of [9000, 20_000, 50_000, 200_000]) {
      const url = buildBugReportURL({ environment: { ...ENV, browser: '\n'.repeat(n) } });
      expect(url.length).toBeLessThanOrEqual(MAX_ISSUE_URL_LENGTH);
      const params = new URL(url).searchParams;
      expect(params.get('labels')).toBe('bug');
      expect(params.get('body')).toContain('…(truncated)');
    }
  });
});
