/**
 * SP-160 Acceptance criteria 1 / host.8 — the build-mode boundary guard.
 *
 * The host contract exists so Sprout never infers its host from a build flag:
 * the host tree (webui/src/host/) owns `localHost`/`cloudHost` and the entry
 * point picks one ONCE. Everything downstream reads the host through the
 * provider (`useHost()`) or the non-React accessor (`getActiveHost()`).
 *
 * This scan fails if any module OUTSIDE webui/src/host/:
 *   - reads the build mode (`import.meta.env.VITE_SPROUT_MODE`),
 *   - names `appMode`,
 *   - imports the removed `isCloud` binding,
 *   - imports a host IMPLEMENTATION by name (`localHost`, `cloudHost`,
 *     `headlessHost`, `defaultHost`, `setActiveHost`) — i.e. names a host.
 *
 * Exactly one module may name a host implementation: the app entry
 * (`src/index.tsx`), which selects the host at startup. That carve-out is the
 * ALLOWLIST below and is deliberately explicit — a new file naming a host is a
 * failure, not a silent second entry point.
 *
 * Comments are stripped before scanning (a doc-comment that mentions the old
 * flag is prose, not a dependency). The scanner is factored into
 * `findBoundaryViolations` and given a positive-control test that feeds it a
 * seeded violation, so a broken scanner cannot pass vacuously.
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const SRC_ROOT = dirname(dirname(fileURLToPath(import.meta.url)));

/**
 * The one module allowed to name a host implementation: the app entry selects
 * the host once at startup (the build flag lives here and nowhere else).
 */
const HOST_SELECTION_ALLOWLIST = ['index.tsx'];

/**
 * Host-implementation bindings that must not be imported outside the host
 * tree (except the entry above). Sprout reads the ACTIVE host, never a
 * specific one.
 */
const HOST_IMPLEMENTATION_NAMES = ['localHost', 'cloudHost', 'headlessHost', 'defaultHost'];

/**
 * True when the code imports from the host BARE BARREL (`'./host'`,
 * `'../host'`, or `'../../host'`) — the entry's own host selection. Anchored so
 * a deep host-module path (`'./host/cloudHost'`) or any specifier merely
 * containing '/host' (`'./services/host-x'`) is NOT accepted: the carve-out is
 * for importing the host contract's entry, not for reaching into host internals.
 */
function importsHostTree(code: string): boolean {
  return /from\s+['"](?:\.\.?\/)*host['"]/.test(code);
}

/** Remove line and block comments so prose examples don't count as usage. */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else out.push(full);
  }
  return out;
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
 * Every build-mode boundary violation in a single source string. Pure, so the
 * positive-control test can feed it crafted sources. `file` is only used to
 * build the message.
 */
export function findBoundaryViolations(file: string, source: string): string[] {
  const code = stripComments(source);
  const offenders: string[] = [];
  const isHostSelection = HOST_SELECTION_ALLOWLIST.includes(relPath(file));

  if (/import\.meta\.env\.VITE_SPROUT_MODE/.test(code) && !isHostSelection) {
    offenders.push(`${relPath(file)}: reads the build mode (VITE_SPROUT_MODE)`);
  }
  if (/\bappMode\b/.test(code)) {
    offenders.push(`${relPath(file)}: names appMode`);
  }
  if (/\bisCloud\b/.test(code)) {
    offenders.push(`${relPath(file)}: names isCloud`);
  }

  // A host implementation by name, imported from a non-host specifier, or a
  // bare import of `setActiveHost` (the entry's own host-selection call).
  if (!isHostSelection) {
    for (const name of HOST_IMPLEMENTATION_NAMES) {
      if (new RegExp(`\\b${name}\\b`).test(code)) {
        offenders.push(`${relPath(file)}: names host implementation '${name}'`);
      }
    }
    if (/\bsetActiveHost\b/.test(code)) {
      offenders.push(`${relPath(file)}: names host implementation 'setActiveHost'`);
    }
  } else if (!importsHostTree(code)) {
    // The entry is exempt from the name check, but it must still import the
    // host from the host tree rather than reaching into a host module path.
    offenders.push(`${relPath(file)}: host selection must import from the host tree`);
  }

  return offenders;
}

/** Scan the whole src tree (minus the host tree and tests). */
function scanSourceTree(): string[] {
  const offenders: string[] = [];
  for (const file of walk(SRC_ROOT)) {
    if (!/\.(ts|tsx)$/.test(file)) continue;
    if (isInHostTree(file) || isTestFile(file)) continue;
    // vite-env.d.ts declares the env var's type; it is ambient, not a read.
    if (relPath(file) === 'vite-env.d.ts') continue;
    offenders.push(...findBoundaryViolations(file, readFileSync(file, 'utf8')));
  }
  return offenders;
}

describe('host boundary (SP-160 Acceptance criteria 1 / host.8)', () => {
  it('no module outside webui/src/host/ reads the build mode, names appMode/isCloud, or names a host', () => {
    expect(scanSourceTree()).toEqual([]);
  });

  it('the entry point is the only module allowed to name a host implementation', () => {
    // Prove the allowlist is real, not dead: index.tsx must actually select a
    // host (if this ever stops being true the carve-out should go).
    const entry = readFileSync(join(SRC_ROOT, HOST_SELECTION_ALLOWLIST[0]), 'utf8');
    expect(/\b(localHost|cloudHost)\b/.test(entry)).toBe(true);
  });

  describe('scanner catches seeded violations (positive control)', () => {
    const outside = join(SRC_ROOT, 'components', 'Seeded.tsx');
    const entry = join(SRC_ROOT, 'index.tsx');

    it('flags a build-mode read', () => {
      expect(findBoundaryViolations(outside, `const c = import.meta.env.VITE_SPROUT_MODE === 'cloud';`)).toContain(
        'components/Seeded.tsx: reads the build mode (VITE_SPROUT_MODE)',
      );
    });

    it('flags an appMode reference', () => {
      expect(findBoundaryViolations(outside, `if (config.appMode === 'cloud') {}`)).toContain(
        'components/Seeded.tsx: names appMode',
      );
    });

    it('flags an isCloud reference', () => {
      expect(findBoundaryViolations(outside, `import { isCloud } from '../config/mode';`)).toContain(
        'components/Seeded.tsx: names isCloud',
      );
    });

    it('flags naming a host implementation', () => {
      const offenders = findBoundaryViolations(outside, `import { cloudHost } from '../host';`);
      expect(offenders).toContain("components/Seeded.tsx: names host implementation 'cloudHost'");
    });

    it('flags setActiveHost outside the entry', () => {
      expect(findBoundaryViolations(outside, `setActiveHost(host);`)).toContain(
        "components/Seeded.tsx: names host implementation 'setActiveHost'",
      );
    });

    it('does NOT flag a comment that merely mentions the old flag', () => {
      expect(findBoundaryViolations(outside, `// this replaced the isCloud branch\nconst x = 1;`)).toEqual([]);
      // Non-vacuous: the same code WITHOUT the comment marker IS flagged, so
      // "ignored" is distinguishable from "rule disabled".
      expect(findBoundaryViolations(outside, `const c = import.meta.env.VITE_SPROUT_MODE;`)).toContain(
        'components/Seeded.tsx: reads the build mode (VITE_SPROUT_MODE)',
      );
    });

    it('does NOT flag the host tree itself', () => {
      // The scanner's caller excludes the host tree; assert the paths it
      // would otherwise flag live there.
      expect(isInHostTree(join(SRC_ROOT, 'host', 'cloudHost.ts'))).toBe(true);
    });

    it('allows the entry to name a host when it imports from the host tree', () => {
      expect(findBoundaryViolations(entry, `import { localHost, cloudHost } from './host';`)).toEqual([]);
      expect(findBoundaryViolations(entry, `import { localHost } from '../host';`)).toEqual([]);
    });

    it('flags the entry naming a host while importing it from elsewhere', () => {
      expect(findBoundaryViolations(entry, `import { cloudHost } from './services/cloud');`)).toContain(
        'index.tsx: host selection must import from the host tree',
      );
    });

    it('flags the entry reaching into a host internal path', () => {
      // A deep host-module specifier is not the host barrel: the carve-out is
      // for the contract's entry, not for naming host internals.
      expect(findBoundaryViolations(entry, `import { cloudHost } from './host/cloudHost';`)).toContain(
        'index.tsx: host selection must import from the host tree',
      );
    });

    it('does not treat a specifier that merely contains /host as the host barrel', () => {
      expect(findBoundaryViolations(entry, `import { cloudHost } from './services/host-x';`)).toContain(
        'index.tsx: host selection must import from the host tree',
      );
    });
  });
});
