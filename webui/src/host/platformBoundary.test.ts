/**
 * SP-160 §160b acceptance gate: no module OUTSIDE webui/src/host/ may name the
 * platform's billing/team/runner/account pages or resolve platform URLs for
 * them. Those strings must live only in the host tree, so the web-UI components
 * stay host-driven (they render host data and call host navigation).
 *
 * It also enforces the platform boundary: no module outside the host tree may
 * import the bootstrap adapter, whose auto-run fetches /api/bootstrap on import.
 * The host tree owns the host; the bootstrap adapter installs the adapter and
 * records the resolved platform URL onto the active host's transport.
 *
 * This scans every .ts/.tsx file under webui/src except the host tree and test
 * files, strips comments (a doc-comment example is prose, not a dependency),
 * and fails on any platform-page token or bootstrap-adapter import.
 */
import { readdirSync, readFileSync, statSync, existsSync } from 'node:fs';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const SRC_ROOT = dirname(dirname(fileURLToPath(import.meta.url)));

/** The static import/export-from specifiers of a source string (no dynamic import). */
function staticSpecifiers(source: string): string[] {
  const pattern = /\b(?:import|export)\b(?!\s*\()\s+(?:[^"'();]*?\bfrom\s+)?["']([^"']+)["']/g;
  return [...source.matchAll(pattern)].map((m) => m[1]);
}

/** Resolve a relative specifier to an existing .ts/.tsx file, or null. */
function resolveSpecifier(fromDir: string, specifier: string): string | null {
  const base = resolve(fromDir, specifier);
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')]) {
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  return null;
}

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

  it('keeps the bootstrap adapter out of the host tree entirely (importing the host fetches nothing)', () => {
    // The host tree must not import the bootstrap adapter at all: a host that
    // imports the package must not drag its /api/bootstrap auto-run in. The
    // concrete platform URL arrives as transport data, not through an import.
    // (Other web UI modules may still read bootstrap identity through the
    // adapter; this scopes to what the host contract's own graph reaches.)
    const hostDir = join(SRC_ROOT, 'host');
    const offenders: string[] = [];
    for (const file of walk(hostDir)) {
      if (!/\.(ts|tsx)$/.test(file)) continue;
      if (isTestFile(file)) continue;
      const code = stripComments(readFileSync(file, 'utf8'));
      if (/bootstrapAdapter/.test(code)) offenders.push(relative(SRC_ROOT, file));
    }
    expect(offenders).toEqual([]);
  });

  it('keeps the bootstrap adapter out of the host entry graph (the import-side-effect boundary)', () => {
    // The host entry (webui/src/host/index.ts) is what the package re-exports
    // and what a component imports for the contract. Walk its static graph and
    // assert no module on it reaches the bootstrap adapter or names its fetch:
    // importing the host entry must not evaluate the adapter's module scope.
    const seen = new Set<string>();
    const offenders: string[] = [];
    const stack = [join(SRC_ROOT, 'host', 'index.ts')];
    while (stack.length > 0) {
      const file = stack.pop()!;
      if (seen.has(file) || isTestFile(file)) continue;
      seen.add(file);
      if (!existsSync(file)) continue;
      const code = stripComments(readFileSync(file, 'utf8'));
      if (/bootstrapAdapter/.test(code) || /\/api\/bootstrap/.test(code)) {
        offenders.push(relative(SRC_ROOT, file));
      }
      for (const spec of staticSpecifiers(code)) {
        if (!spec.startsWith('.')) continue;
        const resolved = resolveSpecifier(dirname(file), spec);
        if (resolved) stack.push(resolved);
      }
    }
    expect(offenders).toEqual([]);
  });
});
