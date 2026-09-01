// @vitest-environment node
/**
 * Deliverable 4: Track R build-script feature-flag tests.
 *
 * Exercises the pure, exported logic in scripts/build-webui-dist.mjs:
 *   - parseArgs / validateArgs (CLI flag parsing + fail-fast validation)
 *   - buildCapabilityManifest / writeCapabilityManifest (Track R manifest)
 *   - import-time side-effect hygiene (direct-run guard)
 *
 * The build script lives OUTSIDE webui/ (repo root, three levels up from
 * this file), so it is imported via a dynamic `await import()` in
 * beforeAll — relative to this file that is `../../../scripts/build-webui-dist.mjs`.
 * Node built-ins (node:fs/url/path/child_process) are imported at top level
 * without executing, so a clean import is expected under `@vitest-environment node`.
 *
 * House style: explicit vitest imports (see src/services/opfsReplica.test.ts).
 */

import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';

type BuildScript = typeof import('../../../scripts/build-webui-dist.mjs');

let mod: BuildScript;

beforeAll(async () => {
  // Dynamic import so a load failure is localized to this hook (not a
  // module-level crash) and the error message is clear.
  mod = await import('../../../scripts/build-webui-dist.mjs');
});

// ── Import-time side-effect hygiene ──────────────────────────────────────
// The script must be importable without running a build (direct-run guard
// via pathToFileURL). We re-evaluate the module fresh under a console.log
// spy: its top level only defines functions, so a clean import adds zero
// log lines and never invokes cli()/main().
describe('import side-effect hygiene', () => {
  it('exposes the expected callable exports', () => {
    const fns = [
      'parseArgs',
      'validateArgs',
      'buildCapabilityManifest',
      'writeCapabilityManifest',
      'printHelp',
      'main',
      'cli',
    ];
    const m = mod as unknown as Record<string, unknown>;
    for (const name of fns) {
      expect(typeof m[name], `${name} export`).toBe('function');
    }
  });

  it('does not run a build / flood console when (re)imported', async () => {
    vi.resetModules(); // force a FRESH evaluation of the module top level
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      const fresh = await import('../../../scripts/build-webui-dist.mjs');
      expect(typeof fresh.parseArgs).toBe('function');
      // No top-level console.log / build output: import must stay silent.
      expect(spy.mock.calls.length).toBe(0);
    } finally {
      spy.mockRestore();
    }
  });
});

// ── parseArgs ────────────────────────────────────────────────────────────
describe('parseArgs', () => {
  it('returns documented defaults for an empty argv', () => {
    const o = mod.parseArgs([]);
    expect(o.mode).toBe('cloud');
    expect(o.outputDir).toBe('');
    expect(o.foundryApiUrl).toBeUndefined();
    expect(o.foundryWsUrl).toBeUndefined();
    expect(o.components).toBe(false);
    expect(o.nativeFs).toBe(false);
    expect(o.help).toBe(false);
    expect(o.unknownArgs).toEqual([]);
  });

  it('parses --mode / --output / --api-url / --ws-url', () => {
    const o = mod.parseArgs([
      '--mode',
      'local',
      '--output',
      './out',
      '--api-url',
      'https://api.example.com',
      '--ws-url',
      'wss://api.example.com',
    ]);
    expect(o.mode).toBe('local');
    expect(o.outputDir).toBe('./out');
    expect(o.foundryApiUrl).toBe('https://api.example.com');
    expect(o.foundryWsUrl).toBe('wss://api.example.com');
  });

  it('records --native-fs on opts.nativeFs', () => {
    expect(mod.parseArgs(['--native-fs']).nativeFs).toBe(true);
  });

  it('records --native-terminal on opts.nativeTerminal (R-3, validated clean)', () => {
    expect(mod.parseArgs(['--native-terminal']).nativeTerminal).toBe(true);
  });

  it('records --native-chat on opts.nativeChat (R-4, validated clean)', () => {
    expect(mod.parseArgs(['--native-chat']).nativeChat).toBe(true);
    // --ratify-chat is not implied by --native-chat.
    expect(mod.parseArgs(['--native-chat']).ratifyChat).toBe(false);
  });

  it('records --ratify-chat on opts.ratifyChat (leaves nativeChat false when alone)', () => {
    const o = mod.parseArgs(['--ratify-chat']);
    expect(o.ratifyChat).toBe(true);
    expect(o.nativeChat).toBe(false);
  });

  it('sets both nativeChat and ratifyChat when both flags are passed', () => {
    const o = mod.parseArgs(['--native-chat', '--ratify-chat']);
    expect(o.nativeChat).toBe(true);
    expect(o.ratifyChat).toBe(true);
  });

  it('sets ratifyChat regardless of flag order', () => {
    const fwd = mod.parseArgs(['--native-chat', '--ratify-chat']);
    const rev = mod.parseArgs(['--ratify-chat', '--native-chat']);
    expect(fwd.ratifyChat).toBe(true);
    expect(rev.ratifyChat).toBe(true);
    expect(fwd.nativeChat).toBe(true);
    expect(rev.nativeChat).toBe(true);
  });

  it('records --native-git on opts.nativeGit (R-4, validated clean)', () => {
    expect(mod.parseArgs(['--native-git']).nativeGit).toBe(true);
  });

  it('sets --components and coerces mode to "components"', () => {
    const o = mod.parseArgs(['--components']);
    expect(o.components).toBe(true);
    expect(o.mode).toBe('components');
  });

  it('sets --help / -h on opts.help without exiting', () => {
    // parseArgs is pure: no process.exit. Both spellings must be recorded.
    expect(mod.parseArgs(['--help']).help).toBe(true);
    expect(mod.parseArgs(['-h']).help).toBe(true);
  });

  it('collects unrecognized --* tokens into unknownArgs', () => {
    const o = mod.parseArgs(['--frobnicate', '--native-fs']);
    expect(o.unknownArgs).toEqual(['--frobnicate']);
    // A recognized flag appearing after the unknown one is still parsed.
    expect(o.nativeFs).toBe(true);
  });
});

// ── validateArgs ─────────────────────────────────────────────────────────
describe('validateArgs', () => {
  it('returns no errors for clean defaults', () => {
    expect(mod.validateArgs(mod.parseArgs([]))).toEqual([]);
  });

  it('accepts a valid native-fs cloud build', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-fs']))).toEqual([]);
  });

  it('accepts --native-terminal (R-3) with no errors', () => {
    // R-3 is now implemented: --native-terminal validates clean on its own.
    expect(mod.validateArgs(mod.parseArgs(['--native-terminal']))).toEqual([]);
  });

  it('accepts the additive --native-fs --native-terminal build', () => {
    // Flags are additive: a fs + terminal build carries both exclusions.
    expect(mod.validateArgs(mod.parseArgs(['--native-fs', '--native-terminal']))).toEqual([]);
  });

  it('accepts --native-chat (R-4) with no errors', () => {
    // R-4 is now implemented: --native-chat validates clean on its own.
    expect(mod.validateArgs(mod.parseArgs(['--native-chat']))).toEqual([]);
  });

  it('accepts the additive --native-fs --native-terminal --native-chat build', () => {
    // All three native-* flags are additive: a single build may exclude fs +
    // terminal + chat together.
    expect(mod.validateArgs(mod.parseArgs(['--native-fs', '--native-terminal', '--native-chat']))).toEqual([]);
  });

  it('accepts --native-chat --ratify-chat (ratified R-4 build)', () => {
    // The R-4 ratify flag validates clean alongside the exclusion flag.
    expect(mod.validateArgs(mod.parseArgs(['--native-chat', '--ratify-chat']))).toEqual([]);
  });

  it('rejects --ratify-chat without --native-chat', () => {
    const [err] = mod.validateArgs(mod.parseArgs(['--ratify-chat']));
    expect(err).toContain('--native-chat');
  });

  it('accepts --native-git (R-4) with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-git']))).toEqual([]);
  });

  it('reports unknown --* options with an "Unknown option" message', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--frobnicate']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toContain("Unknown option '--frobnicate'");
  });

  it('rejects combining --native-fs with --components', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-fs', '--components']));
    expect(errs.some((e) => e.toLowerCase().includes('cannot be combined'))).toBe(true);
  });

  it('rejects combining --native-terminal with --components', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-terminal', '--components']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toContain('cannot be combined with --components');
  });

  it('rejects a lone --ratify-terminal (requires --native-terminal)', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--ratify-terminal']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toContain('--ratify-terminal requires --native-terminal');
  });

  it('accepts --native-terminal --ratify-terminal', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-terminal', '--ratify-terminal']))).toEqual([]);
  });

  it('rejects an invalid --mode', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--mode', 'bogus']));
    expect(errs.some((e) => e.includes("Invalid mode 'bogus'"))).toBe(true);
  });
});

// ── buildCapabilityManifest ──────────────────────────────────────────────
describe('buildCapabilityManifest', () => {
  it('produces an empty exclusion list by default', () => {
    const m = mod.buildCapabilityManifest(mod.parseArgs([]));
    expect(m.schemaVersion).toBe(1);
    expect(m.excluded).toEqual([]);
    expect(m.buildMode).toBe('cloud');
    // generatedAt must be an ISO-8601 timestamp.
    expect(new Date(m.generatedAt).toISOString()).toBe(m.generatedAt);
  });

  it('records the fs portion (native replacement) when --native-fs', () => {
    const m = mod.buildCapabilityManifest(mod.parseArgs(['--native-fs']));
    expect(m.excluded).toHaveLength(1);
    const entry = m.excluded[0];
    expect(entry.portion).toBe('fs');
    expect(entry.flag).toBe('--native-fs');
    expect(entry.replacedBy).toBe('native');
    expect(entry.hardExclusion).toBe(true);
    // status gates servability: today every build is seam-only (build-time
    // artifact, not shell-servable).
    expect(entry.status).toBe('seam-only');
    expect(typeof entry.notes).toBe('string');
    expect(entry.notes.length).toBeGreaterThan(0);
  });

  it('propagates buildMode from opts', () => {
    const m = mod.buildCapabilityManifest(mod.parseArgs(['--mode', 'local']));
    expect(m.buildMode).toBe('local');
  });
});

// ── writeCapabilityManifest ──────────────────────────────────────────────
describe('writeCapabilityManifest', () => {
  let tmp: string;
  beforeAll(() => {
    // createFresh per test below; the suite-level tmp just anchors the path.
    tmp = mkdtempSync(join(tmpdir(), 'caps-manifest-'));
  });
  afterAll(() => {
    if (existsSync(tmp)) rmSync(tmp, { recursive: true, force: true });
  });

  function freshOutputDir(suffix = 'out') {
    // writeCapabilityManifest writes into the dir but does NOT create it
    // (the build script's cleanOutputDirectory does that first). Each call
    // gets its own subdirectory so a file written by one test can never
    // leak into another (they share this suite-level tmp anchor).
    const dir = join(tmp, suffix);
    mkdirSync(dir, { recursive: true });
    return dir;
  }

  it('returns null and writes no file for default opts', () => {
    const path = mod.writeCapabilityManifest(freshOutputDir('default'), mod.parseArgs([]));
    expect(path).toBeNull();
    // Absence of capabilities.json == nothing excluded.
    expect(existsSync(join(tmp, 'default', 'capabilities.json'))).toBe(false);
  });

  it('writes a parseable, 2-space-indented capabilities.json for --native-fs', () => {
    const outDir = freshOutputDir('native-fs');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--native-fs']));
    expect(path).toBe(join(outDir, 'capabilities.json'));
    if (path === null) throw new Error('expected capabilities.json path, got null');
    expect(existsSync(path)).toBe(true);

    const raw = readFileSync(path, 'utf-8');
    const parsed = JSON.parse(raw);
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.excluded).toHaveLength(1);
    expect(parsed.excluded[0].portion).toBe('fs');
    expect(parsed.excluded[0].flag).toBe('--native-fs');
    expect(parsed.excluded[0].replacedBy).toBe('native');
    expect(parsed.excluded[0].status).toBe('seam-only');
    // 2-space JSON indentation (line 2 is "  " prefixed).
    expect(raw.split('\n')[1].startsWith('  "')).toBe(true);
  });

  it('writes a ratified capabilities.json for --native-fs --ratify-fs', () => {
    const outDir = freshOutputDir('ratified');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--native-fs', '--ratify-fs']));
    expect(path).toBe(join(outDir, 'capabilities.json'));
    if (path === null) throw new Error('expected capabilities.json path, got null');
    expect(existsSync(path)).toBe(true);

    const parsed = JSON.parse(readFileSync(path, 'utf-8'));
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.excluded).toHaveLength(1);
    expect(parsed.excluded[0].portion).toBe('fs');
    // The R-2w swap: ratify-fs flips the entry to a shell-servable status.
    expect(parsed.excluded[0].status).toBe('ratified');
  });

  it('writes no file when --ratify-fs alone (no fs portion excluded)', () => {
    // writeCapabilityManifest emits a file only when at least one portion is
    // excluded. --ratify-fs without --native-fs excludes nothing, so it must
    // return null and write nothing (mirrors the default-build contract).
    const outDir = freshOutputDir('ratify-alone');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--ratify-fs']));
    expect(path).toBeNull();
    expect(existsSync(join(outDir, 'capabilities.json'))).toBe(false);
  });
});

// ── --ratify-fs (R-2w ratified manifest) ────────────────────────────────
// Focused coverage for the --ratify-fs flag across parseArgs, validateArgs,
// buildCapabilityManifest, and the help text. Pure functions only — no build.
describe('--ratify-fs (R-2w ratified manifest)', () => {
  // ── parseArgs ──────────────────────────────────────────────────────────
  it('sets ratifyFs true and leaves nativeFs false when --ratify-fs is alone', () => {
    const o = mod.parseArgs(['--ratify-fs']);
    expect(o.ratifyFs).toBe(true);
    expect(o.nativeFs).toBe(false);
  });

  it('sets both nativeFs and ratifyFs when both flags are passed', () => {
    const o = mod.parseArgs(['--native-fs', '--ratify-fs']);
    expect(o.nativeFs).toBe(true);
    expect(o.ratifyFs).toBe(true);
  });

  it('leaves ratifyFs falsey when the flag is absent', () => {
    expect(mod.parseArgs([]).ratifyFs).toBe(false);
    expect(mod.parseArgs(['--native-fs']).ratifyFs).toBe(false);
  });

  it('sets ratifyFs regardless of flag order', () => {
    const fwd = mod.parseArgs(['--native-fs', '--ratify-fs']);
    const rev = mod.parseArgs(['--ratify-fs', '--native-fs']);
    expect(fwd.ratifyFs).toBe(true);
    expect(rev.ratifyFs).toBe(true);
    expect(fwd.nativeFs).toBe(true);
    expect(rev.nativeFs).toBe(true);
  });

  it('does not consume the following token and leaves unknownArgs untouched', () => {
    const o = mod.parseArgs(['--ratify-fs', '--frobnicate', '--native-fs']);
    expect(o.ratifyFs).toBe(true);
    expect(o.nativeFs).toBe(true);
    // --ratify-fs is a boolean flag; the next token must still be examined.
    expect(o.unknownArgs).toEqual(['--frobnicate']);
  });

  // ── validateArgs ───────────────────────────────────────────────────────
  it('rejects --ratify-fs alone with a single "requires --native-fs" error', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--ratify-fs']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toMatch(/--ratify-fs requires --native-fs/);
  });

  it('accepts --native-fs --ratify-fs with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-fs', '--ratify-fs']))).toEqual([]);
  });

  it('still rejects --components when --native-fs --ratify-fs are combined', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-fs', '--ratify-fs', '--components']));
    expect(errs.length).toBeGreaterThan(0);
    // The --native-fs/--components prohibition must surface (independent of
    // ratify-fs); the requires error is absent because nativeFs is set.
    expect(errs.some((e) => e.toLowerCase().includes('cannot be combined'))).toBe(true);
    expect(errs.some((e) => e.includes('--ratify-fs requires'))).toBe(false);
  });

  it('treats --ratify-fs-typo as an unknown option (no prefix match)', () => {
    const o = mod.parseArgs(['--ratify-fs-typo']);
    expect(o.ratifyFs).toBe(false);
    expect(o.unknownArgs).toEqual(['--ratify-fs-typo']);
    const errs = mod.validateArgs(o);
    expect(errs.some((e) => e.includes("Unknown option '--ratify-fs-typo'"))).toBe(true);
  });

  // ── buildCapabilityManifest ────────────────────────────────────────────
  it('emits an empty exclusion list when nativeFs is false', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud' });
    expect(m.excluded).toEqual([]);
  });

  it('emits a seam-only fs entry for nativeFs alone (original notes)', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeFs: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.status).toBe('seam-only');
    expect(e.portion).toBe('fs');
    // The seam-only notes point at the decoupling audit, not R-2w.
    expect(e.notes).not.toMatch(/R-2w/i);
  });

  it('emits a ratified fs entry for nativeFs + ratifyFs', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      ratifyFs: true,
    });
    expect(m.schemaVersion).toBe(1);
    expect(m.buildMode).toBe('cloud');
    expect(new Date(m.generatedAt).toISOString()).toBe(m.generatedAt);
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.status).toBe('ratified');
    expect(e.portion).toBe('fs');
    expect(e.flag).toBe('--native-fs');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    expect(e.notes).toMatch(/R-2w/i);
    expect(e.notes).toMatch(/ratif/i);
  });

  it('propagates buildMode from opts.mode into the manifest', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'local',
      nativeFs: true,
      ratifyFs: true,
    });
    expect(m.buildMode).toBe('local');
    expect(m.excluded[0].status).toBe('ratified');
  });

  it('still emits no fs entry when ratifyFs is set but nativeFs is not', () => {
    // ratify-fs only ratifies the fs portion of a --native-fs build; without
    // --native-fs there is nothing to ratify, so the manifest stays empty.
    // (validateArgs fails fast on this combo in the CLI, but the pure
    // manifest builder must not invent an entry.)
    const m = mod.buildCapabilityManifest({ mode: 'cloud', ratifyFs: true });
    expect(m.excluded).toEqual([]);
  });

  // ── help text ──────────────────────────────────────────────────────────
  it('mentions --ratify-fs in the help output', () => {
    // printHelp is exported; capture its console output and assert the new
    // flag (and the R-2w ratification semantics) are documented.
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      mod.printHelp();
      const out = spy.mock.calls.map((c) => c.join(' ')).join('\n');
      expect(out).toContain('--ratify-fs');
      expect(out).toMatch(/ratif/i);
    } finally {
      spy.mockRestore();
    }
  });
});

// ── --native-terminal / --ratify-terminal (R-3) ─────────────────────────
// Focused coverage for the R-3 terminal seam across parseArgs, validateArgs,
// buildCapabilityManifest, and the help text. Pure functions only — no build.
// Mirrors the --ratify-fs block above; all R-3-specific assertions live here
// (the generic --native-terminal parse/validate cases that already exist in
// the parseArgs / validateArgs blocks are intentionally NOT duplicated).
describe('--native-terminal / --ratify-terminal (R-3)', () => {
  // ── parseArgs ──────────────────────────────────────────────────────────
  it('records --ratify-terminal on opts.ratifyTerminal (leaves nativeTerminal false when alone)', () => {
    const o = mod.parseArgs(['--ratify-terminal']);
    expect(o.ratifyTerminal).toBe(true);
    expect(o.nativeTerminal).toBe(false);
  });

  it('sets both nativeTerminal and ratifyTerminal when both flags are passed', () => {
    const o = mod.parseArgs(['--native-terminal', '--ratify-terminal']);
    expect(o.nativeTerminal).toBe(true);
    expect(o.ratifyTerminal).toBe(true);
  });

  it('leaves ratifyTerminal falsey when the flag is absent', () => {
    expect(mod.parseArgs([]).ratifyTerminal).toBe(false);
    expect(mod.parseArgs(['--native-terminal']).ratifyTerminal).toBe(false);
  });

  it('sets ratifyTerminal regardless of flag order', () => {
    const fwd = mod.parseArgs(['--native-terminal', '--ratify-terminal']);
    const rev = mod.parseArgs(['--ratify-terminal', '--native-terminal']);
    expect(fwd.ratifyTerminal).toBe(true);
    expect(rev.ratifyTerminal).toBe(true);
    expect(fwd.nativeTerminal).toBe(true);
    expect(rev.nativeTerminal).toBe(true);
  });

  it('does not consume the following token and leaves unknownArgs untouched', () => {
    const o = mod.parseArgs(['--ratify-terminal', '--frobnicate', '--native-terminal']);
    expect(o.ratifyTerminal).toBe(true);
    expect(o.nativeTerminal).toBe(true);
    expect(o.unknownArgs).toEqual(['--frobnicate']);
  });

  it('treats --ratify-terminal-typo as an unknown option (no prefix match)', () => {
    const o = mod.parseArgs(['--ratify-terminal-typo']);
    expect(o.ratifyTerminal).toBe(false);
    expect(o.unknownArgs).toEqual(['--ratify-terminal-typo']);
  });

  // ── validateArgs ───────────────────────────────────────────────────────
  it('accepts the additive --native-fs --native-terminal build', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-fs', '--native-terminal']))).toEqual([]);
  });

  it('accepts --native-terminal --ratify-terminal', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-terminal', '--ratify-terminal']))).toEqual([]);
  });

  it('rejects a lone --ratify-terminal with a single "requires --native-terminal" error', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--ratify-terminal']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe('Error: --ratify-terminal requires --native-terminal.');
  });

  it('still rejects --components when --native-terminal --ratify-terminal are combined', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-terminal', '--ratify-terminal', '--components']));
    expect(errs.length).toBeGreaterThan(0);
    // The --native-terminal/--components prohibition must surface; the
    // requires error is absent because nativeTerminal is set.
    expect(errs.some((e) => e.toLowerCase().includes('cannot be combined'))).toBe(true);
    expect(errs.some((e) => e.includes('--ratify-terminal requires'))).toBe(false);
  });

  it('rejects the exact --native-terminal + --components message', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-terminal', '--components']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe(
      'Error: --native-terminal cannot be combined with --components (standalone component entries are not the app bundle).',
    );
  });

  // ── buildCapabilityManifest ────────────────────────────────────────────
  it('emits a single seam-only terminal entry for nativeTerminal alone', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeTerminal: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.portion).toBe('terminal');
    expect(e.flag).toBe('--native-terminal');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    expect(e.status).toBe('seam-only');
    // The exact seam-only notes string (points at the decoupling audit).
    expect(e.notes).toBe(
      'WASM/PTY terminal transport provided natively by the shell; terminal modules stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)',
    );
  });

  it('emits a ratified terminal entry for nativeTerminal + ratifyTerminal', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeTerminal: true, ratifyTerminal: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.status).toBe('ratified');
    expect(e.portion).toBe('terminal');
    expect(e.flag).toBe('--native-terminal');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    // The R-3 ratified notes (parity-proven swap, points at the ADR).
    expect(e.notes).toBe(
      'R-3 ratified: parity-proven swap — the WASM/PTY terminal transport is ' +
        'provided natively by the shell; the terminal module is stubbed out of ' +
        'the bundle (see docs/adr-0008-webui-native-seams.md, terminal seam).',
    );
  });

  it('carries BOTH the fs and terminal entries when both flags are set (additive)', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeFs: true, nativeTerminal: true });
    expect(m.excluded).toHaveLength(2);
    // Entry order follows the fs → terminal order of the code.
    expect(m.excluded[0].portion).toBe('fs');
    expect(m.excluded[0].flag).toBe('--native-fs');
    expect(m.excluded[1].portion).toBe('terminal');
    expect(m.excluded[1].flag).toBe('--native-terminal');
    // Both default to seam-only without their ratify flags.
    expect(m.excluded[0].status).toBe('seam-only');
    expect(m.excluded[1].status).toBe('seam-only');
  });

  it('ratifies only the terminal entry when both flags + --ratify-terminal are set', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      nativeTerminal: true,
      ratifyTerminal: true,
    });
    expect(m.excluded).toHaveLength(2);
    expect(m.excluded[0].status).toBe('seam-only'); // fs untouched
    expect(m.excluded[1].status).toBe('ratified'); // terminal ratified
  });

  it('still emits no terminal entry when ratifyTerminal is set but nativeTerminal is not', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', ratifyTerminal: true });
    expect(m.excluded).toEqual([]);
  });

  it('propagates buildMode into the R-3 manifest', () => {
    const m = mod.buildCapabilityManifest({ mode: 'local', nativeTerminal: true, ratifyTerminal: true });
    expect(m.buildMode).toBe('local');
    expect(m.excluded[0].status).toBe('ratified');
  });

  // ── help text ──────────────────────────────────────────────────────────
  it('documents --native-terminal and --ratify-terminal as Implemented (R-3)', () => {
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      mod.printHelp();
      const out = spy.mock.calls.map((c) => c.join(' ')).join('\n');
      expect(out).toContain('--native-terminal');
      expect(out).toContain('--ratify-terminal');
      // Both R-3 flags are documented as implemented.
      expect(out).toMatch(/--native-terminal\s+Implemented \(R-3\)/);
      expect(out).toMatch(/--ratify-terminal\s+Implemented \(R-3\)/);
      // --ratify-terminal requires --native-terminal (documented).
      expect(out).toMatch(/Requires --native-terminal/);
    } finally {
      spy.mockRestore();
    }
  });

  it('documents --native-chat / --ratify-chat and --native-git / --ratify-git as Implemented (R-4)', () => {
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      mod.printHelp();
      const out = spy.mock.calls.map((c) => c.join(' ')).join('\n');
      // Both R-4 chat flags are documented as implemented.
      expect(out).toMatch(/--native-chat\s+Implemented \(R-4\)/);
      expect(out).toMatch(/--ratify-chat\s+Implemented \(R-4\)/);
      // --ratify-chat requires --native-chat (documented).
      expect(out).toMatch(/Requires --native-chat/);
      // The R-4 git flags are also documented as implemented (no reserved
      // --native-* flag remains).
      expect(out).toMatch(/--native-git\s+Implemented \(R-4\)/);
      expect(out).toMatch(/--ratify-git\s+Implemented \(R-4\)/);
      expect(out).toMatch(/Requires --native-git/);
    } finally {
      spy.mockRestore();
    }
  });
});

// ── --native-chat / --ratify-chat (R-4) ────────────────────────────
// Focused coverage for the R-4 chat seam across parseArgs, validateArgs,
// buildCapabilityManifest, and the help text. Pure functions only — no build.
// Mirrors the R-3 block above; all R-4-specific assertions live here (the
// generic parse/validate/manifest cases that already exist in the
// parseArgs / validateArgs / buildCapabilityManifest blocks are
// intentionally NOT duplicated).
describe('--native-chat / --ratify-chat (R-4)', () => {
  // ── parseArgs ──────────────────────────────────────────────────────────
  it('records --native-chat on opts.nativeChat', () => {
    expect(mod.parseArgs(['--native-chat']).nativeChat).toBe(true);
  });

  it('records --ratify-chat on opts.ratifyChat (leaves nativeChat false when alone)', () => {
    const o = mod.parseArgs(['--ratify-chat']);
    expect(o.ratifyChat).toBe(true);
    expect(o.nativeChat).toBe(false);
  });

  it('sets both nativeChat and ratifyChat when both flags are passed', () => {
    const o = mod.parseArgs(['--native-chat', '--ratify-chat']);
    expect(o.nativeChat).toBe(true);
    expect(o.ratifyChat).toBe(true);
  });

  it('leaves ratifyChat falsey when the flag is absent', () => {
    expect(mod.parseArgs([]).ratifyChat).toBe(false);
    expect(mod.parseArgs(['--native-chat']).ratifyChat).toBe(false);
  });

  it('sets ratifyChat regardless of flag order', () => {
    const fwd = mod.parseArgs(['--native-chat', '--ratify-chat']);
    const rev = mod.parseArgs(['--ratify-chat', '--native-chat']);
    expect(fwd.ratifyChat).toBe(true);
    expect(rev.ratifyChat).toBe(true);
    expect(fwd.nativeChat).toBe(true);
    expect(rev.nativeChat).toBe(true);
  });

  it('does not consume the following token and leaves unknownArgs untouched', () => {
    const o = mod.parseArgs(['--ratify-chat', '--frobnicate', '--native-chat']);
    expect(o.ratifyChat).toBe(true);
    expect(o.nativeChat).toBe(true);
    expect(o.unknownArgs).toEqual(['--frobnicate']);
  });

  it('treats --ratify-chat-typo as an unknown option (no prefix match)', () => {
    const o = mod.parseArgs(['--ratify-chat-typo']);
    expect(o.ratifyChat).toBe(false);
    expect(o.unknownArgs).toEqual(['--ratify-chat-typo']);
  });

  // ── validateArgs ───────────────────────────────────────────────────────
  it('accepts --native-chat alone with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-chat']))).toEqual([]);
  });

  it('accepts --native-chat --ratify-chat with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-chat', '--ratify-chat']))).toEqual([]);
  });

  it('accepts the additive --native-fs --native-terminal --native-chat build', () => {
    // Flags are additive: an fs + terminal + chat build carries all three.
    expect(mod.validateArgs(mod.parseArgs(['--native-fs', '--native-terminal', '--native-chat']))).toEqual([]);
  });

  it('rejects a lone --ratify-chat with a single "requires --native-chat" error', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--ratify-chat']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe('Error: --ratify-chat requires --native-chat.');
  });

  it('still rejects --components when --native-chat --ratify-chat are combined', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-chat', '--ratify-chat', '--components']));
    expect(errs.length).toBeGreaterThan(0);
    // The --native-chat/--components prohibition must surface; the
    // requires error is absent because nativeChat is set.
    expect(errs.some((e) => e.toLowerCase().includes('cannot be combined'))).toBe(true);
    expect(errs.some((e) => e.includes('--ratify-chat requires'))).toBe(false);
  });

  it('rejects the exact --native-chat + --components message', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-chat', '--components']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe(
      'Error: --native-chat cannot be combined with --components (standalone component entries are not the app bundle).',
    );
  });

  it('accepts --native-chat --native-git (R-4) as an additive build', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-chat', '--native-git']))).toEqual([]);
  });

  // ── buildCapabilityManifest ────────────────────────────────────────────
  it('emits a single seam-only chat entry for nativeChat alone', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeChat: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.portion).toBe('chat');
    expect(e.flag).toBe('--native-chat');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    expect(e.status).toBe('seam-only');
    // The exact seam-only notes string (points at the decoupling audit).
    expect(e.notes).toBe(
      'fetch/SSE agent-turn chat transport provided natively by the shell; chat transport modules stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)',
    );
  });

  it('emits a ratified chat entry for nativeChat + ratifyChat', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeChat: true, ratifyChat: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.status).toBe('ratified');
    expect(e.portion).toBe('chat');
    expect(e.flag).toBe('--native-chat');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    // The R-4 ratified notes (parity-proven swap, points at the ADR).
    expect(e.notes).toBe(
      'R-4 ratified: parity-proven swap — the fetch/SSE agent-turn chat ' +
        'transport is provided natively by the shell; the chat transport ' +
        'module is stubbed out of the bundle (see docs/adr-0008-webui-native-seams.md, chat seam).',
    );
  });

  it('carries fs + terminal + chat entries when all three flags are set (additive, in order)', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      nativeTerminal: true,
      nativeChat: true,
    });
    expect(m.excluded).toHaveLength(3);
    // Entry order follows the fs → terminal → chat order of the code.
    expect(m.excluded[0].portion).toBe('fs');
    expect(m.excluded[0].flag).toBe('--native-fs');
    expect(m.excluded[1].portion).toBe('terminal');
    expect(m.excluded[1].flag).toBe('--native-terminal');
    expect(m.excluded[2].portion).toBe('chat');
    expect(m.excluded[2].flag).toBe('--native-chat');
    // All default to seam-only without their ratify flags.
    expect(m.excluded[0].status).toBe('seam-only');
    expect(m.excluded[1].status).toBe('seam-only');
    expect(m.excluded[2].status).toBe('seam-only');
  });

  it('ratifies only the chat entry when all three flags + --ratify-chat are set', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      nativeTerminal: true,
      nativeChat: true,
      ratifyChat: true,
    });
    expect(m.excluded).toHaveLength(3);
    expect(m.excluded[0].status).toBe('seam-only'); // fs untouched
    expect(m.excluded[1].status).toBe('seam-only'); // terminal untouched
    expect(m.excluded[2].status).toBe('ratified'); // chat ratified
  });

  it('still emits no chat entry when ratifyChat is set but nativeChat is not', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', ratifyChat: true });
    expect(m.excluded).toEqual([]);
  });

  it('propagates buildMode into the R-4 manifest', () => {
    const m = mod.buildCapabilityManifest({ mode: 'local', nativeChat: true, ratifyChat: true });
    expect(m.buildMode).toBe('local');
    expect(m.excluded[0].status).toBe('ratified');
  });

  // ── help text ──────────────────────────────────────────────────────────
  it('documents --native-chat and --ratify-chat as Implemented (R-4)', () => {
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      mod.printHelp();
      const out = spy.mock.calls.map((c) => c.join(' ')).join('\n');
      expect(out).toContain('--native-chat');
      expect(out).toContain('--ratify-chat');
      // Both R-4 flags are documented as implemented.
      expect(out).toMatch(/--native-chat\s+Implemented \(R-4\)/);
      expect(out).toMatch(/--ratify-chat\s+Implemented \(R-4\)/);
      // --ratify-chat requires --native-chat (documented).
      expect(out).toMatch(/Requires --native-chat/);
    } finally {
      spy.mockRestore();
    }
  });
});

// ── --native-git / --ratify-git (R-4) ─────────────────────────────────
// Focused coverage for the R-4 git seam across parseArgs, validateArgs,
// buildCapabilityManifest, and the help text. Pure functions only — no build.
// Mirrors the R-4 chat block above; all R-4-git-specific assertions live
// here (the generic parse/validate/manifest cases that already exist in the
// parseArgs / validateArgs / buildCapabilityManifest blocks are
// intentionally NOT duplicated).
describe('--native-git / --ratify-git (R-4)', () => {
  // ── parseArgs ──────────────────────────────────────────────────────────
  it('records --native-git on opts.nativeGit', () => {
    expect(mod.parseArgs(['--native-git']).nativeGit).toBe(true);
  });

  it('records --ratify-git on opts.ratifyGit (leaves nativeGit false when alone)', () => {
    const o = mod.parseArgs(['--ratify-git']);
    expect(o.ratifyGit).toBe(true);
    expect(o.nativeGit).toBe(false);
  });

  it('sets both nativeGit and ratifyGit when both flags are passed', () => {
    const o = mod.parseArgs(['--native-git', '--ratify-git']);
    expect(o.nativeGit).toBe(true);
    expect(o.ratifyGit).toBe(true);
  });

  it('leaves ratifyGit falsey when the flag is absent', () => {
    expect(mod.parseArgs([]).ratifyGit).toBe(false);
    expect(mod.parseArgs(['--native-git']).ratifyGit).toBe(false);
  });

  it('sets ratifyGit regardless of flag order', () => {
    const fwd = mod.parseArgs(['--native-git', '--ratify-git']);
    const rev = mod.parseArgs(['--ratify-git', '--native-git']);
    expect(fwd.ratifyGit).toBe(true);
    expect(rev.ratifyGit).toBe(true);
    expect(fwd.nativeGit).toBe(true);
    expect(rev.nativeGit).toBe(true);
  });

  it('does not consume the following token and leaves unknownArgs untouched', () => {
    const o = mod.parseArgs(['--ratify-git', '--frobnicate', '--native-git']);
    expect(o.ratifyGit).toBe(true);
    expect(o.nativeGit).toBe(true);
    expect(o.unknownArgs).toEqual(['--frobnicate']);
  });

  it('treats --ratify-git-typo as an unknown option (no prefix match)', () => {
    const o = mod.parseArgs(['--ratify-git-typo']);
    expect(o.ratifyGit).toBe(false);
    expect(o.unknownArgs).toEqual(['--ratify-git-typo']);
  });

  // ── validateArgs ───────────────────────────────────────────────────────
  it('accepts --native-git alone with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-git']))).toEqual([]);
  });

  it('accepts --native-git --ratify-git with no errors', () => {
    expect(mod.validateArgs(mod.parseArgs(['--native-git', '--ratify-git']))).toEqual([]);
  });

  it('accepts the additive --native-fs --native-terminal --native-chat --native-git build', () => {
    // Flags are additive: an fs + terminal + chat + git build carries all four.
    expect(
      mod.validateArgs(mod.parseArgs(['--native-fs', '--native-terminal', '--native-chat', '--native-git'])),
    ).toEqual([]);
  });

  it('rejects a lone --ratify-git with a single "requires --native-git" error', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--ratify-git']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe('Error: --ratify-git requires --native-git.');
  });

  it('still rejects --components when --native-git --ratify-git are combined', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-git', '--ratify-git', '--components']));
    expect(errs.length).toBeGreaterThan(0);
    // The --native-git/--components prohibition must surface; the
    // requires error is absent because nativeGit is set.
    expect(errs.some((e) => e.toLowerCase().includes('cannot be combined'))).toBe(true);
    expect(errs.some((e) => e.includes('--ratify-git requires'))).toBe(false);
  });

  it('rejects the exact --native-git + --components message', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-git', '--components']));
    expect(errs).toHaveLength(1);
    expect(errs[0]).toBe(
      'Error: --native-git cannot be combined with --components (standalone component entries are not the app bundle).',
    );
  });

  // ── buildCapabilityManifest ────────────────────────────────────────────
  it('emits a single seam-only git entry for nativeGit alone', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeGit: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.portion).toBe('git');
    expect(e.flag).toBe('--native-git');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    expect(e.status).toBe('seam-only');
    // The exact seam-only notes string (points at the decoupling audit).
    expect(e.notes).toBe(
      'git client API + boot wiring provided natively by the shell; git client API module stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)',
    );
  });

  it('emits a ratified git entry for nativeGit + ratifyGit', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', nativeGit: true, ratifyGit: true });
    expect(m.excluded).toHaveLength(1);
    const e = m.excluded[0];
    expect(e.status).toBe('ratified');
    expect(e.portion).toBe('git');
    expect(e.flag).toBe('--native-git');
    expect(e.replacedBy).toBe('native');
    expect(e.hardExclusion).toBe(true);
    // The R-4 ratified notes (parity-proven swap, points at the ADR).
    expect(e.notes).toBe(
      'R-4 ratified: parity-proven swap — the git client API + boot ' +
        'wiring is provided natively by the shell; the git client API ' +
        'module is stubbed out of the bundle (see docs/adr-0008-webui-native-seams.md, git seam).',
    );
  });

  it('carries fs + terminal + chat + git entries when all four flags are set (additive, in order)', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      nativeTerminal: true,
      nativeChat: true,
      nativeGit: true,
    });
    expect(m.excluded).toHaveLength(4);
    // Entry order follows the fs → terminal → chat → git order of the code.
    expect(m.excluded[0].portion).toBe('fs');
    expect(m.excluded[0].flag).toBe('--native-fs');
    expect(m.excluded[1].portion).toBe('terminal');
    expect(m.excluded[1].flag).toBe('--native-terminal');
    expect(m.excluded[2].portion).toBe('chat');
    expect(m.excluded[2].flag).toBe('--native-chat');
    expect(m.excluded[3].portion).toBe('git');
    expect(m.excluded[3].flag).toBe('--native-git');
    // All default to seam-only without their ratify flags.
    expect(m.excluded[0].status).toBe('seam-only');
    expect(m.excluded[1].status).toBe('seam-only');
    expect(m.excluded[2].status).toBe('seam-only');
    expect(m.excluded[3].status).toBe('seam-only');
  });

  it('ratifies only the git entry when all four flags + --ratify-git are set', () => {
    const m = mod.buildCapabilityManifest({
      mode: 'cloud',
      nativeFs: true,
      nativeTerminal: true,
      nativeChat: true,
      nativeGit: true,
      ratifyGit: true,
    });
    expect(m.excluded).toHaveLength(4);
    expect(m.excluded[0].status).toBe('seam-only'); // fs untouched
    expect(m.excluded[1].status).toBe('seam-only'); // terminal untouched
    expect(m.excluded[2].status).toBe('seam-only'); // chat untouched
    expect(m.excluded[3].status).toBe('ratified'); // git ratified
  });

  it('still emits no git entry when ratifyGit is set but nativeGit is not', () => {
    const m = mod.buildCapabilityManifest({ mode: 'cloud', ratifyGit: true });
    expect(m.excluded).toEqual([]);
  });

  it('propagates buildMode into the R-4 git manifest', () => {
    const m = mod.buildCapabilityManifest({ mode: 'local', nativeGit: true, ratifyGit: true });
    expect(m.buildMode).toBe('local');
    expect(m.excluded[0].status).toBe('ratified');
  });

  // ── help text ──────────────────────────────────────────────────────────
  it('documents --native-git and --ratify-git as Implemented (R-4)', () => {
    const spy = vi.spyOn(console, 'log').mockImplementation(() => {});
    try {
      mod.printHelp();
      const out = spy.mock.calls.map((c) => c.join(' ')).join('\n');
      expect(out).toContain('--native-git');
      expect(out).toContain('--ratify-git');
      // Both R-4 git flags are documented as implemented.
      expect(out).toMatch(/--native-git\s+Implemented \(R-4\)/);
      expect(out).toMatch(/--ratify-git\s+Implemented \(R-4\)/);
      // --ratify-git requires --native-git (documented).
      expect(out).toMatch(/Requires --native-git/);
    } finally {
      spy.mockRestore();
    }
  });

  // ── manifest write (acceptance criteria: default build + git write case) ─
  // Each write case gets its own fresh tempdir (the git suite does not share
  // the "writeCapabilityManifest" block's suite-level tmp anchor — that
  // helper is scoped to that block). Mirrors the chat/fs no-write
  // convention (null return + file absence).
  let gitTmp: string;
  beforeAll(() => {
    gitTmp = mkdtempSync(join(tmpdir(), 'caps-manifest-git-'));
  });
  afterAll(() => {
    if (existsSync(gitTmp)) rmSync(gitTmp, { recursive: true, force: true });
  });

  function gitOutputDir(suffix: string): string {
    const dir = join(gitTmp, suffix);
    mkdirSync(dir, { recursive: true });
    return dir;
  }

  it('default build (no flags): manifest excluded:[] and writeCapabilityManifest writes NO capabilities.json', () => {
    // No excluded portions → the manifest is empty AND no capabilities.json
    // file is emitted (the shell only sees a manifest when something was
    // actually excluded). Same contract the fs/chat no-write cases assert.
    const m = mod.buildCapabilityManifest(mod.parseArgs([]));
    expect(m.excluded).toEqual([]);

    const outDir = gitOutputDir('git-default');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs([]));
    expect(path).toBeNull();
    expect(existsSync(join(outDir, 'capabilities.json'))).toBe(false);
  });

  it('writeCapabilityManifest writes a parseable capabilities.json for --native-git (seam-only)', () => {
    const outDir = gitOutputDir('native-git');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--native-git']));
    expect(path).toBe(join(outDir, 'capabilities.json'));
    if (path === null) throw new Error('expected capabilities.json path, got null');
    expect(existsSync(path)).toBe(true);

    const raw = readFileSync(path, 'utf-8');
    const parsed = JSON.parse(raw);
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.excluded).toHaveLength(1);
    expect(parsed.excluded[0].portion).toBe('git');
    expect(parsed.excluded[0].flag).toBe('--native-git');
    expect(parsed.excluded[0].replacedBy).toBe('native');
    expect(parsed.excluded[0].hardExclusion).toBe(true);
    expect(parsed.excluded[0].status).toBe('seam-only');
    // 2-space JSON indentation (line 2 is "  " prefixed).
    expect(raw.split('\n')[1].startsWith('  "')).toBe(true);
  });

  it('writeCapabilityManifest writes a ratified capabilities.json for --native-git --ratify-git', () => {
    const outDir = gitOutputDir('ratified-git');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--native-git', '--ratify-git']));
    expect(path).toBe(join(outDir, 'capabilities.json'));
    if (path === null) throw new Error('expected capabilities.json path, got null');
    expect(existsSync(path)).toBe(true);

    const parsed = JSON.parse(readFileSync(path, 'utf-8'));
    expect(parsed.schemaVersion).toBe(1);
    expect(parsed.excluded).toHaveLength(1);
    expect(parsed.excluded[0].portion).toBe('git');
    // The R-4 ratify-git swap: the git entry is emitted shell-servable.
    expect(parsed.excluded[0].status).toBe('ratified');
  });

  it('writes no file when --ratify-git alone (no git portion excluded)', () => {
    // --ratify-git without --native-git excludes nothing (validated as an
    // error by validateArgs, but the manifest writer must still be inert).
    const outDir = gitOutputDir('ratify-git-alone');
    const path = mod.writeCapabilityManifest(outDir, mod.parseArgs(['--ratify-git']));
    expect(path).toBeNull();
    expect(existsSync(join(outDir, 'capabilities.json'))).toBe(false);
  });
});
