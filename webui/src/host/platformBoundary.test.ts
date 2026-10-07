/**
 * SP-160 §160b acceptance gate (host.6): no module OUTSIDE webui/src/host/ may
 * name the platform's billing/team/runner/account pages or resolve platform
 * URLs for them. Those strings must live only in the host tree, so the web-UI
 * components stay host-driven (they render host data and call host navigation).
 *
 * This scans every .ts/.tsx file under webui/src except the host tree and test
 * files, strips comments (a doc-comment example is prose, not a dependency),
 * and fails on any platform-page token.
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const SRC_ROOT = dirname(dirname(fileURLToPath(import.meta.url)));

/** Platform page tokens that must not appear outside webui/src/host/. */
const PLATFORM_TOKENS = [
  'account/billing', // the platform's usage-and-billing page
  '#/team', // Team
  '#/runners', // Runners
  '#/admin', // Admin
  'billing/status', // the platform's billing-status endpoint
  '/?from=editor', // the dashboard exit hash
  '#/workspaces', // Workspaces
];

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else out.push(full);
  }
  return out;
}

/** Remove line and block comments so prose examples don't count as usage. */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

function isTestFile(path: string): boolean {
  return /\.(test|spec)\.(ts|tsx)$/.test(path);
}

function isInHostTree(path: string): boolean {
  const rel = relative(SRC_ROOT, path);
  return rel === 'host' || rel.startsWith(`host${sep}`);
}

describe('platform boundary (SP-160 §160b)', () => {
  it('keeps platform page tokens inside webui/src/host/', () => {
    const offenders: string[] = [];
    for (const file of walk(SRC_ROOT)) {
      if (!/\.(ts|tsx)$/.test(file)) continue;
      if (isInHostTree(file) || isTestFile(file)) continue;
      const code = stripComments(readFileSync(file, 'utf8'));
      for (const token of PLATFORM_TOKENS) {
        if (code.includes(token)) offenders.push(`${relative(SRC_ROOT, file)}: ${token}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});
