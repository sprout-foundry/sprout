/**
 * SP-160 §160b acceptance gate: no module OUTSIDE webui/src/host/ may name the
 * platform's billing/team/runner/account pages or resolve platform URLs for
 * them. Those strings must live only in the host tree, so the web-UI components
 * stay host-driven (they render host data and call host navigation).
 *
 * It also enforces two structural boundaries:
 *  - the PLATFORM-IMPLEMENTATION boundary: no module outside the host tree may
 *    import a `host/platform*` module (the concrete platform surface —
 *    `cloudHost`, `platformHref`, `PlatformGitHubAccountCard`,
 *    `platformEntitlements`, …). Those are internal to the host; a component
 *    imports the contract (`host/index`) only. The app entry is the one
 *    carve-out: it selects the host at startup, importing `cloudHost` from the
 *    internal platform module.
 *  - the BOOTSTRAP-ADAPTER boundary: importing the host entry must fetch
 *    nothing, so no module on the host entry graph may reach the bootstrap
 *    adapter (whose auto-run fetches /api/bootstrap).
 *
 * This scans every .ts/.tsx file under webui/src except the host tree and test
 * files, strips comments (a doc-comment example is prose, not a dependency),
 * and fails on any platform-page token or forbidden import. The scanner is a
 * pure function with positive controls, so a broken scanner cannot pass
 * vacuously.
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

/**
 * Platform page tokens that must not appear outside webui/src/host/. Each is a
 * regex matched against the comment-stripped source. Fragile tokens (a bare
 * `/login` collides with `user.login` and `github.com/login/device`, and the
 * platform-auth proxy legitimately redirects to `/login` from outside the host
 * tree) are matched as quoted string literals, so only a hardcoded page path is
 * flagged.
 */
const PLATFORM_TOKENS: { label: string; pattern: RegExp }[] = [
  { label: 'account/billing', pattern: /account\/billing/ }, // the platform's usage-and-billing page
  { label: '#/team', pattern: /#\/team/ }, // Team
  { label: '#/runners', pattern: /#\/runners/ }, // Runners
  { label: '#/admin', pattern: /#\/admin/ }, // Admin
  { label: 'billing/status', pattern: /billing\/status/ }, // the platform's billing-status endpoint
  { label: '/?from=editor', pattern: /\/\?from=editor/ }, // the dashboard exit hash
  { label: '#/workspaces', pattern: /#\/workspaces/ }, // Workspaces
  { label: '/webui/auth/logout', pattern: /\/webui\/auth\/logout/ }, // the platform's sign-out (logout) endpoint
  // The sign-out landing page and the embed decoration, as quoted page paths.
  { label: '/login (page path)', pattern: /['"]\/login['"]/ },
  { label: '/#/tasks/', pattern: /\/#\/tasks\// }, // the platform task deep-link route
  { label: '/?embed=1', pattern: /\/\?embed=1/ }, // the platform's iframe-embed page decoration
];

/**
 * The one module allowed to import the internal platform module: the app entry
 * selects the host once at startup (it is the single place that reads the build
 * flag), and it imports `cloudHost` from the internal platform module rather
 * than the public host barrel.
 */
const PLATFORM_IMPORT_ALLOWLIST = ['index.tsx'];

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

/** Relative path with forward slashes, for allowlist comparison. */
function relPath(path: string): string {
  return relative(SRC_ROOT, path).split(sep).join('/');
}

/**
 * Whether a specifier resolves to a `host/platform*` implementation module:
 * `host/platform.ts`, `host/platform/…`, `host/platformUrl.ts`,
 * `host/platformGitHub.ts`, `host/platformNav.ts`,
 * `host/PlatformGitHubAccountCard.tsx`. `from` and `specifier` are relative.
 * A deep host-internal path is exactly what the boundary forbids.
 */
export function resolvesToPlatformInternal(fromDir: string, specifier: string): boolean {
  if (!specifier.startsWith('.')) return false;
  const resolved = resolveSpecifier(fromDir, specifier);
  if (!resolved) return false;
  const rel = relPath(resolved);
  const inPlatformDir = rel.startsWith('host/platform/');
  const platformFile = /^host\/platform[A-Za-z]*\.tsx?$/.test(rel);
  const card = rel === 'host/PlatformGitHubAccountCard.tsx';
  return inPlatformDir || platformFile || card;
}

/** Every platform-boundary violation in a single source string. `file` is its path. */
export function findPlatformViolations(file: string, source: string): string[] {
  const code = stripComments(source);
  const offenders: string[] = [];
  const rel = relPath(file);

  for (const token of PLATFORM_TOKENS) {
    if (token.pattern.test(code)) offenders.push(`${rel}: platform token '${token.label}'`);
  }

  if (!PLATFORM_IMPORT_ALLOWLIST.includes(rel)) {
    const fromDir = dirname(file);
    for (const spec of staticSpecifiers(code)) {
      if (resolvesToPlatformInternal(fromDir, spec)) {
        offenders.push(`${rel}: imports the internal platform module '${spec}'`);
      }
    }
  }
  return offenders;
}

/** Scan the whole src tree (minus the host tree and tests). */
function scanSourceTree(): string[] {
  const offenders: string[] = [];
  for (const file of walk(SRC_ROOT)) {
    if (!/\.(ts|tsx)$/.test(file)) continue;
    if (isInHostTree(file) || isTestFile(file)) continue;
    offenders.push(...findPlatformViolations(file, readFileSync(file, 'utf8')));
  }
  return offenders;
}

describe('platform boundary (SP-160 §160b)', () => {
  it('keeps platform page tokens and platform imports out of every module outside webui/src/host/', () => {
    expect(scanSourceTree()).toEqual([]);
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
      if (/bootstrapAdapter/.test(code)) offenders.push(relPath(file));
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
        offenders.push(relPath(file));
      }
      for (const spec of staticSpecifiers(code)) {
        if (!spec.startsWith('.')) continue;
        const resolved = resolveSpecifier(dirname(file), spec);
        if (resolved) stack.push(resolved);
      }
    }
    expect(offenders).toEqual([]);
  });

  it('the public host entry does not re-export the platform implementation (non-vacuous)', () => {
    // The split is the point of the item: the public entry carries the contract
    // only. Prove it does not re-export cloudHost or the platform helpers, so a
    // regression that widens the public API fails here.
    const entry = stripComments(readFileSync(join(SRC_ROOT, 'host', 'index.ts'), 'utf8'));
    for (const name of ['cloudHost', 'platformHref', 'platformEntitlements', 'PlatformGitHubAccountCard']) {
      expect(entry.includes(name), `the public host entry must not name '${name}'`).toBe(false);
    }
    // Non-vacuous: it DOES export the contract pieces.
    expect(entry).toContain('localHost');
    expect(entry).toContain('useHost');
  });

  describe('scanner catches seeded violations (positive control)', () => {
    const outside = join(SRC_ROOT, 'components', 'Seeded.tsx');
    const entry = join(SRC_ROOT, 'index.tsx');

    it('flags each platform path literal', () => {
      expect(findPlatformViolations(outside, `fetch('/webui/auth/logout', { method: 'POST' });`)).toContain(
        "components/Seeded.tsx: platform token '/webui/auth/logout'",
      );
      expect(findPlatformViolations(outside, `window.location.href = '/login';`)).toContain(
        "components/Seeded.tsx: platform token '/login (page path)'",
      );
      expect(findPlatformViolations(outside, `href={'/#/tasks/' + id}`)).toContain(
        "components/Seeded.tsx: platform token '/#/tasks/'",
      );
      expect(findPlatformViolations(outside, `src={'/?embed=1#' + route}`)).toContain(
        "components/Seeded.tsx: platform token '/?embed=1'",
      );
    });

    it('flags an import of the internal platform module from outside the host tree', () => {
      expect(findPlatformViolations(outside, `import { platformHref } from '../host/platformUrl';`)).toContain(
        "components/Seeded.tsx: imports the internal platform module '../host/platformUrl'",
      );
      expect(findPlatformViolations(outside, `import { cloudHost } from '../host/cloudHost';`)).toEqual([]);
      expect(findPlatformViolations(outside, `import { intentPath } from '../host/platform';`)).toContain(
        "components/Seeded.tsx: imports the internal platform module '../host/platform'",
      );
      expect(
        findPlatformViolations(
          outside,
          `import { PlatformGitHubAccountCard } from '../host/PlatformGitHubAccountCard';`,
        ),
      ).toContain("components/Seeded.tsx: imports the internal platform module '../host/PlatformGitHubAccountCard'");
      expect(findPlatformViolations(outside, `import { usesPlatformGitHub } from '../host/platformGitHub';`)).toContain(
        "components/Seeded.tsx: imports the internal platform module '../host/platformGitHub'",
      );
      expect(findPlatformViolations(outside, `import { CLOUD_NAV_ITEMS } from '../host/platformNav';`)).toContain(
        "components/Seeded.tsx: imports the internal platform module '../host/platformNav'",
      );
    });

    it('does NOT flag the public host entry import', () => {
      expect(findPlatformViolations(outside, `import { useHost } from '../host';`)).toEqual([]);
      expect(findPlatformViolations(outside, `import { repoSlug } from '../host/repoName';`)).toEqual([]);
    });

    it('does NOT flag a comment that merely mentions an old path', () => {
      const seeded = `// sign-out used to POST /webui/auth/logout and land on /login\nconst x = 1;`;
      expect(findPlatformViolations(outside, seeded)).toEqual([]);
      // Non-vacuous: the same code WITHOUT the comment marker IS flagged.
      expect(findPlatformViolations(outside, `fetch('/webui/auth/logout');`)).not.toEqual([]);
    });

    it('does NOT flag the host tree itself (the token + import rules are scoped out)', () => {
      // The scanner's caller excludes the host tree; assert the offending
      // samples would resolve inside it.
      expect(isInHostTree(join(SRC_ROOT, 'host', 'platformUrl.ts'))).toBe(true);
      expect(isInHostTree(join(SRC_ROOT, 'host', 'platform', 'index.ts'))).toBe(true);
    });

    it('allows the app entry to import the internal platform module (host selection)', () => {
      expect(findPlatformViolations(entry, `import { cloudHost } from './host/platform';`)).toEqual([]);
      // But the entry is still bound by the token rule where it is scanned.
      expect(isInHostTree(entry)).toBe(false);
    });
  });
});
