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

import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

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

  it('records the reserved --native-* flags on opts (validation is separate)', () => {
    expect(mod.parseArgs(['--native-terminal']).nativeTerminal).toBe(true);
    expect(mod.parseArgs(['--native-chat']).nativeChat).toBe(true);
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

  it('rejects --native-terminal (R-3) with the reserved + roadmap messages', () => {
    const errs = mod.validateArgs(mod.parseArgs(['--native-terminal']));
    expect(errs).toHaveLength(1);
    expect(errs[0].toLowerCase()).toContain('reserved');
    expect(errs[0]).toContain('R-3');
    expect(errs[0]).toContain('docs/WEBUI_DECOUPLING_AUDIT.md');
    expect(errs[0]).toContain('docs/adr-0008-webui-native-seams.md');
  });

  it('rejects --native-chat (R-4) with the reserved + roadmap messages', () => {
    const [err] = mod.validateArgs(mod.parseArgs(['--native-chat']));
    expect(err.toLowerCase()).toContain('reserved');
    expect(err).toContain('R-4');
    expect(err).toContain('docs/WEBUI_DECOUPLING_AUDIT.md');
    expect(err).toContain('docs/adr-0008-webui-native-seams.md');
  });

  it('rejects --native-git (R-5) with the reserved + roadmap messages', () => {
    const [err] = mod.validateArgs(mod.parseArgs(['--native-git']));
    expect(err.toLowerCase()).toContain('reserved');
    expect(err).toContain('R-5');
    expect(err).toContain('docs/WEBUI_DECOUPLING_AUDIT.md');
    expect(err).toContain('docs/adr-0008-webui-native-seams.md');
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
