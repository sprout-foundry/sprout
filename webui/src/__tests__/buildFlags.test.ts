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

  function freshOutputDir() {
    // writeCapabilityManifest writes into the dir but does NOT create it
    // (the build script's cleanOutputDirectory does that first).
    const dir = join(tmp, 'out');
    mkdirSync(dir, { recursive: true });
    return dir;
  }

  it('returns null and writes no file for default opts', () => {
    const path = mod.writeCapabilityManifest(freshOutputDir(), mod.parseArgs([]));
    expect(path).toBeNull();
    // Absence of capabilities.json == nothing excluded.
    expect(existsSync(join(tmp, 'out', 'capabilities.json'))).toBe(false);
  });

  it('writes a parseable, 2-space-indented capabilities.json for --native-fs', () => {
    const outDir = freshOutputDir();
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
});
