/**
 * SP-160 Acceptance criteria 1 / host.8 + host.14 — the build-mode boundary
 * guard.
 *
 * The host contract exists so Sprout never infers its host from a build flag:
 * the host tree (webui/src/host/) owns `localHost`/`cloudHost` and the entry
 * point picks one ONCE. Everything downstream reads the host through the
 * provider (`useHost()`) or the non-React accessor (`getActiveHost()`).
 *
 * This scan fails if any module OUTSIDE webui/src/host/:
 *   - reads the build mode (`import.meta.env.VITE_SPROUT_MODE`),
 *   - reads Vite's built-in mode string (`import.meta.env.MODE`) — another
 *     way to read the build mode,
 *   - reads the env object by bracket index (`import.meta.env['…']`) — a
 *     bracket read dodges any property-NAME rule, so it is flagged outright,
 *   - aliases the env object (`const env = import.meta.env`, a destructure,
 *     or passing it to a function) so a property-name rule could be dodged,
 *   - names `appMode`,
 *   - imports the removed `isCloud` binding,
 *   - imports a host IMPLEMENTATION by name (`localHost`, `cloudHost`,
 *     `headlessHost`, `defaultHost`, `setActiveHost`) — i.e. names a host.
 *
 * Rule scope for the env reads (documented decision): "no module outside the
 * entry may read the BUILD MODE / env object". A blanket "any `import.meta.env`
 * outside the allowlist is a violation" would flag the many legitimate
 * NON-mode reads the tree relies on — `import.meta.env.VITE_FOUNDRY_API_URL`,
 * `VITE_WS_URL`, the `VITE_SPROUT_NATIVE_*` compile-time flags, and
 * `import.meta.env.PROD` (logging, service worker). Those read a build-time
 * VALUE (a URL, a seam flag, dev/prod), not the mode/env object, so the rule is
 * scoped to what can resolve to the build mode:
 *   - `VITE_SPROUT_MODE` and `MODE` property reads,
 *   - ANY bracket read (`import.meta.env[...]`),
 *   - ANY aliasing of `import.meta.env` (its occurrence not being the direct
 *     object of a simple `.PROP` read).
 * A direct `.PROP` read of any OTHER property (e.g. `.VITE_FOUNDRY_API_URL`,
 * `.PROD`) is allowed and is NOT flagged.
 *
 * Exactly one module may name a host implementation: the app entry
 * (`src/index.tsx`), which selects the host at startup. That carve-out is the
 * ALLOWLIST below and is deliberately explicit — a new file naming a host is a
 * failure, not a silent second entry point. The entry's env carve-out is
 * limited to what it actually does — the single `VITE_SPROUT_MODE` read; it may
 * NOT use `.MODE`, bracket access or aliasing, which it does not need.
 *
 * Comments are stripped before scanning (a doc-comment that mentions the old
 * flag is prose, not a dependency). The scanner is factored into
 * `findBoundaryViolations` and given positive-control tests that feed it a
 * seeded violation of each rule, so a broken scanner cannot pass vacuously.
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
 * True when the code imports the host from the tree — the entry's own host
 * selection. Two specifiers are accepted: the bare barrel (`'./host'`) and the
 * internal platform module (`'./host/platform'`, which carries `cloudHost`).
 * Anchored so any other deep host-module path (`'./host/cloudHost'`) or a
 * specifier merely containing '/host' (`'./services/host-x'`) is NOT accepted:
 * the carve-out is for selecting the host, not for reaching into host internals.
 */
function importsHostTree(code: string): boolean {
  return /from\s+['"](?:\.\.?\/)*host(?:\/platform)?['"]/.test(code);
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
 * True when the source uses `import.meta.env` other than as the direct object
 * of a simple `.PROP` property read — i.e. aliasing (assignment, destructure,
 * argument passing) that a property-name rule could be dodged through.
 *
 * Implementation: any `import.meta.env` occurrence that is NOT immediately
 * followed by `.<identifier>` is a violation. The regex's global `lastIndex`
 * consumes each `import.meta.env.<PROP>` match, so occurrences inside a `.PROP`
 * read are not re-scanned (a match never re-includes its own prefix). The
 * optional `skipViteSproutMode` carve-out lets the ENTRY keep its single
 * `import.meta.env.VITE_SPROUT_MODE` read (a non-global probe run on a copy, so
 * `lastIndex` does not leak into the main scan).
 */
function findEnvAlias(code: string, skipViteSproutMode: boolean): boolean {
  if (skipViteSproutMode && /import\.meta\.env\.VITE_SPROUT_MODE/.test(code)) {
    return false;
  }
  const re = /import\.meta\.env(?!\s*\.\s*[A-Za-z_$])/g;
  return re.test(code);
}

/**
 * Every build-mode boundary violation in a single source string. Pure, so the
 * positive-control tests can feed it crafted sources. `file` is only used to
 * build the message.
 */
export function findBoundaryViolations(file: string, source: string): string[] {
  const code = stripComments(source);
  const offenders: string[] = [];
  const isHostSelection = HOST_SELECTION_ALLOWLIST.includes(relPath(file));

  if (/import\.meta\.env\.VITE_SPROUT_MODE/.test(code) && !isHostSelection) {
    offenders.push(`${relPath(file)}: reads the build mode (VITE_SPROUT_MODE)`);
  }
  // Vite's built-in mode string is another way to read the build mode. `.MODE`
  // is anchored on `import.meta.env.` so a bare `MODE` identifier,
  // `.VITE_SPROUT_MODE` (covered above), and a longer `…MODE_…` property are
  // not matched. Never carved out for the entry: it reads only VITE_SPROUT_MODE.
  if (/import\.meta\.env\.MODE\b/.test(code)) {
    offenders.push(`${relPath(file)}: reads the build mode (import.meta.env.MODE)`);
  }
  // Any INDEXED read of the env object can dodge a property-name rule, so it is
  // flagged outright — legitimate env reads are always `.PROP` form.
  if (/import\.meta\.env\s*\[/.test(code)) {
    offenders.push(`${relPath(file)}: reads env via bracket access (import.meta.env[…])`);
  }
  // Aliasing (or otherwise using) the env object other than as the direct
  // object of a simple `.PROP` read. Any surviving `import.meta.env` occurrence
  // is one that is NOT exactly `import.meta.env.<PROP>`; `.VITE_SPROUT_MODE` is
  // carved out for the entry (does not collide: a `.MODE` read's match starts at
  // its own prefix).
  if (findEnvAlias(code, isHostSelection)) {
    offenders.push(`${relPath(file)}: aliases the env object (import.meta.env)`);
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

    // ── host.14 evasions ───────────────────────────────────────────────

    it('flags a Vite built-in mode read (import.meta.env.MODE)', () => {
      expect(findBoundaryViolations(outside, `const isCloudBuild = import.meta.env.MODE === 'production';`)).toContain(
        'components/Seeded.tsx: reads the build mode (import.meta.env.MODE)',
      );
      // Also as a value, not just a comparison.
      expect(findBoundaryViolations(outside, `const m = import.meta.env.MODE;`)).toContain(
        'components/Seeded.tsx: reads the build mode (import.meta.env.MODE)',
      );
    });

    it('flags a bracket read of the env object (any index)', () => {
      expect(findBoundaryViolations(outside, `const m = import.meta.env['VITE_SPROUT_MODE'];`)).toContain(
        'components/Seeded.tsx: reads env via bracket access (import.meta.env[…])',
      );
      expect(findBoundaryViolations(outside, `const m = import.meta.env["MODE"];`)).toContain(
        'components/Seeded.tsx: reads env via bracket access (import.meta.env[…])',
      );
      // A dynamic index dodges a property-name rule entirely, so any bracket
      // read is flagged — even one that (today) names a non-mode var.
      expect(findBoundaryViolations(outside, 'const u = import.meta.env[key];')).toContain(
        'components/Seeded.tsx: reads env via bracket access (import.meta.env[…])',
      );
    });

    it('flags aliasing the env object (assignment)', () => {
      expect(findBoundaryViolations(outside, `const env = import.meta.env;`)).toContain(
        'components/Seeded.tsx: aliases the env object (import.meta.env)',
      );
      // Aliasing is caught even when the alias is then used as a bridge read
      // that briefly looks like `.PROD` — only a direct `.PROP` read is allowed.
      expect(findBoundaryViolations(outside, `const env = (0, import.meta.env); env.PROD;`)).toContain(
        'components/Seeded.tsx: aliases the env object (import.meta.env)',
      );
    });

    it('flags aliasing the env object (destructure)', () => {
      expect(findBoundaryViolations(outside, `const { VITE_SPROUT_MODE } = import.meta.env;`)).toContain(
        'components/Seeded.tsx: aliases the env object (import.meta.env)',
      );
    });

    it('flags aliasing the env object (passing it to a function)', () => {
      expect(findBoundaryViolations(outside, `readMode(import.meta.env);`)).toContain(
        'components/Seeded.tsx: aliases the env object (import.meta.env)',
      );
    });

    it('does NOT flag legitimate direct property reads (negative control)', () => {
      // A URL, the dev/prod flag and a Track R seam flag all read a build-time
      // VALUE — not the mode — so they must stay green outside the allowlist.
      expect(findBoundaryViolations(outside, `const url = import.meta.env.VITE_FOUNDRY_API_URL;`)).toEqual([]);
      expect(findBoundaryViolations(outside, `const ws = import.meta.env.VITE_WS_URL || '/ws';`)).toEqual([]);
      expect(findBoundaryViolations(outside, `const on = import.meta.env.VITE_SPROUT_NATIVE_FS === '1';`)).toEqual([]);
      expect(findBoundaryViolations(outside, `const level = import.meta.env.PROD ? 'warn' : 'debug';`)).toEqual([]);
      expect(findBoundaryViolations(outside, `if (!import.meta.env.PROD) { log(); }`)).toEqual([]);
      // Non-vacuous: a MODE read on the SAME line shape IS flagged, so
      // "not flagged" is distinguishable from "rule disabled".
      expect(findBoundaryViolations(outside, `const m = import.meta.env.MODE;`)).toContain(
        'components/Seeded.tsx: reads the build mode (import.meta.env.MODE)',
      );
    });

    it('does NOT flag a comment that merely mentions the env object', () => {
      expect(findBoundaryViolations(outside, `// reads import.meta.env.MODE historically\nconst x = 1;`)).toEqual([]);
      expect(findBoundaryViolations(outside, `/* import.meta.env['MODE'] */ const x = 1;`)).toEqual([]);
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
      // The internal platform module is the other legitimate selection path
      // (the entry imports cloudHost from it).
      expect(findBoundaryViolations(entry, `import { cloudHost } from './host/platform';`)).toEqual([]);
      expect(findBoundaryViolations(entry, `import { cloudHost } from '../host/platform';`)).toEqual([]);
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

    it('allows the entry its single VITE_SPROUT_MODE read but no other mode evasion', () => {
      // The entry's env carve-out is exactly what it does: one direct
      // VITE_SPROUT_MODE read. `.MODE`, bracket access and aliasing are NOT
      // carved out — the entry does not need them, so they stay violations.
      // (Each seed carries the entry's required host import, so the only
      // possible offender is the mode rule under test.)
      const host = `import { localHost } from './host';\n`;
      expect(findBoundaryViolations(entry, `${host}const h = import.meta.env.VITE_SPROUT_MODE === 'cloud';`)).toEqual(
        [],
      );
      expect(findBoundaryViolations(entry, `${host}const m = import.meta.env.MODE;`)).toContain(
        'index.tsx: reads the build mode (import.meta.env.MODE)',
      );
      expect(findBoundaryViolations(entry, `${host}const m = import.meta.env['VITE_SPROUT_MODE'];`)).toContain(
        'index.tsx: reads env via bracket access (import.meta.env[…])',
      );
      expect(findBoundaryViolations(entry, `${host}const env = import.meta.env;`)).toContain(
        'index.tsx: aliases the env object (import.meta.env)',
      );
    });

    it('does not treat a specifier that merely contains /host as the host barrel', () => {
      expect(findBoundaryViolations(entry, `import { cloudHost } from './services/host-x';`)).toContain(
        'index.tsx: host selection must import from the host tree',
      );
    });
  });
});
