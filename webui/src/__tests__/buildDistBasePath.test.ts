// @vitest-environment node
/**
 * Cloud-bundle base path: the released bundle must be built at /webui/.
 *
 * scripts/build-webui-dist.mjs builds the release cloud bundle
 * (.github/workflows/release.yml). It must invoke Vite with `--mode cloud` so
 * webui/vite.config.ts selects base /webui/ — otherwise Vite's default
 * production mode yields base / and the built index.html references
 * root-absolute /assets/* URLs, which hosts that mount the bundle at /webui/
 * answer with their own dashboard HTML (blank page).
 *
 * These are pure-function tests (no build): resolveViteBuildArgs chooses the
 * Vite args per mode, and parseAssetRefs + the same base predicate the CI
 * check uses (scripts/verify-webui-dist-base.mjs) decide whether a built
 * index.html is base-correct. A fixture with base / fails; a fixture with
 * base /webui/ passes.
 *
 * The script and check live outside webui/ (repo root), imported via a
 * relative `../../../scripts/...` path, as in buildFlags.test.ts.
 *
 * House style: explicit vitest imports.
 */

import { describe, it, expect, beforeAll } from 'vitest';
import type * as BuildScriptModule from '../../../scripts/build-webui-dist.mjs';
import viteConfig from '../../vite.config';

type BuildScript = typeof BuildScriptModule;

interface BaseCheck {
  findNonBaseAssetUrls(html: string, base: string): string[];
}

let mod: BuildScript;
let baseCheck: BaseCheck;

beforeAll(async () => {
  mod = await import('../../../scripts/build-webui-dist.mjs');
  baseCheck = (await import('../../../scripts/verify-webui-dist-base.mjs')) as unknown as BaseCheck;
});

// ── resolveViteBuildArgs ─────────────────────────────────────────────────
// The cloud branch must pass `--mode cloud` through to Vite so vite.config's
// `isCloud` branch applies. The `--` separator is required for npm to forward
// the flag (without it npm consumes `--mode` and passes a bare positional
// `cloud`). Local builds keep the plain production build.
describe('resolveViteBuildArgs', () => {
  it('passes -- --mode cloud for a cloud build', () => {
    expect(mod.resolveViteBuildArgs('cloud')).toEqual(['--', '--mode', 'cloud']);
  });

  it('separates npm args from Vite args so npm forwards --mode', () => {
    // Contract: the first element is the `--` separator. npm without it turns
    // `--mode cloud` into `npm run build cloud`, i.e. `vite build cloud`.
    const args = mod.resolveViteBuildArgs('cloud');
    expect(args[0]).toBe('--');
    expect(args).toContain('--mode');
    expect(args[args.indexOf('--mode') + 1]).toBe('cloud');
  });

  it('passes no extra args for local / other modes', () => {
    expect(mod.resolveViteBuildArgs('local')).toEqual([]);
    expect(mod.resolveViteBuildArgs('components')).toEqual([]);
    expect(mod.resolveViteBuildArgs('')).toEqual([]);
  });

  it('is exported as a pure function', () => {
    expect(typeof mod.resolveViteBuildArgs).toBe('function');
  });
});

// ── parseAssetRefs ───────────────────────────────────────────────────────
describe('parseAssetRefs', () => {
  it('collects src and href values in document order', () => {
    const html = [
      '<link rel="icon" href="/webui/logo-mark.svg" />',
      '<link rel="manifest" href="/webui/manifest.json" />',
      '<script type="module" src="/webui/assets/index-abc.js"></script>',
    ].join('\n');
    expect(mod.parseAssetRefs(html)).toEqual([
      '/webui/logo-mark.svg',
      '/webui/manifest.json',
      '/webui/assets/index-abc.js',
    ]);
  });

  it('handles single-quoted and unquoted attributes', () => {
    const html = `<link href='/a.css'><script src=/b.js></script>`;
    expect(mod.parseAssetRefs(html)).toEqual(['/a.css', '/b.js']);
  });

  it('ignores src/href mentioned only inside comments', () => {
    const html = '<!-- src="/assets/stale.js" -->\n<script src="/webui/assets/real.js"></script>';
    expect(mod.parseAssetRefs(html)).toEqual(['/webui/assets/real.js']);
  });

  it('returns an empty array for html with no refs', () => {
    expect(mod.parseAssetRefs('<html><body></body></html>')).toEqual([]);
  });
});

// ── base-path predicate (the CI check's pure core) ───────────────────────
// Mirrors scripts/verify-webui-dist-base.mjs: every absolute asset URL must
// start with /webui/, except the small allow-list of host-root paths
// (/api/…, /webui/auth/…, /logo-mark.svg). A build at base / must FAIL.
describe('findNonBaseAssetUrls (cloud /webui/ base check)', () => {
  const cloudHtml = [
    '<!doctype html>',
    '<html lang="en">',
    '  <head>',
    '    <link rel="icon" type="image/svg+xml" href="/webui/logo-mark.svg" />',
    '    <link rel="apple-touch-icon" href="/webui/icon-192.png" />',
    '    <link rel="manifest" href="/webui/manifest.json" />',
    '    <script type="module" crossorigin src="/webui/assets/index-abc123.js"></script>',
    '    <link rel="stylesheet" crossorigin href="/webui/assets/index-abc123.css">',
    '  </head>',
    '  <body><div id="root"></div></body>',
    '</html>',
  ].join('\n');

  const rootHtml = [
    '<!doctype html>',
    '<html lang="en">',
    '  <head>',
    '    <link rel="icon" type="image/svg+xml" href="/logo-mark.svg" />',
    '    <link rel="manifest" href="/manifest.json" />',
    '    <script type="module" crossorigin src="/assets/index-abc123.js"></script>',
    '    <link rel="stylesheet" crossorigin href="/assets/index-abc123.css">',
    '  </head>',
    '  <body><div id="root"></div></body>',
    '</html>',
  ].join('\n');

  it('PASSES a bundle built at /webui/ (the release cloud bundle)', () => {
    expect(baseCheck.findNonBaseAssetUrls(cloudHtml, '/webui/')).toEqual([]);
  });

  it('FAILS a bundle built at base / with root-absolute /assets URLs', () => {
    const offenders = baseCheck.findNonBaseAssetUrls(rootHtml, '/webui/');
    // Both the JS and CSS root-absolute asset URLs must be flagged.
    expect(offenders).toContain('/assets/index-abc123.js');
    expect(offenders).toContain('/assets/index-abc123.css');
    expect(offenders.length).toBeGreaterThan(0);
  });

  it('flags a single stray root-absolute asset among /webui/ ones', () => {
    const mixed = cloudHtml.replace('/webui/assets/index-abc123.js', '/assets/index-abc123.js');
    expect(baseCheck.findNonBaseAssetUrls(mixed, '/webui/')).toEqual(['/assets/index-abc123.js']);
  });

  it('allows host-root API and auth references', () => {
    // Extensionless host routes stay off the base; they are not bundle assets.
    const html = [
      '<script type="module" src="/webui/assets/index-abc.js"></script>',
      '<link rel="stylesheet" href="/webui/assets/index-abc.css">',
      '<a href="/webui/auth/logout">Sign out</a>',
      '<a href="/api/health">Health</a>',
    ].join('\n');
    expect(baseCheck.findNonBaseAssetUrls(html, '/webui/')).toEqual([]);
  });

  it('flags a root-absolute public file in a cloud bundle', () => {
    // Vite rewrites /logo-mark.svg to /webui/logo-mark.svg under base
    // /webui/; a root-absolute one means the bundle was built at base / and
    // would 404 on the host, so it must be flagged (not allow-listed).
    const html = '<link rel="icon" href="/logo-mark.svg" />';
    expect(baseCheck.findNonBaseAssetUrls(html, '/webui/')).toEqual(['/logo-mark.svg']);
  });

  it('does not flag relative asset URLs (offline-tolerant)', () => {
    const html = '<link rel="stylesheet" href="./assets/index-abc.css"><script src="assets/index-abc.js"></script>';
    expect(baseCheck.findNonBaseAssetUrls(html, '/webui/')).toEqual([]);
  });

  it('does not flag navigation routes (not bundle assets)', () => {
    const html = '<a href="/webui/auth/logout">Sign out</a><a href="/about">About</a>';
    expect(baseCheck.findNonBaseAssetUrls(html, '/webui/')).toEqual([]);
  });

  it('accepts a root build when checked against base /', () => {
    // Symmetry: the same predicate with base / clears the root fixture, so
    // the cloud failure above is the base mismatch, not a blanket rejection.
    expect(baseCheck.findNonBaseAssetUrls(rootHtml, '/')).toEqual([]);
  });
});

// ── vite.config.ts: cloud build base + production optimizations ──────────
// Real guard for the release path: `vite build --mode cloud` must yield base
// /webui/ AND keep production optimizations. `isProd` must key off the
// `command` (build), not the mode label — otherwise --mode cloud, now used by
// the release bundle, would silently drop treeshake and the debugger drop.
describe('vite config (cloud build)', () => {
  type ConfigFn = (env: { mode: string; command: string }) => unknown;

  async function resolve(mode: string, command: string) {
    const fn = viteConfig as unknown as ConfigFn;
    return (await fn({ mode, command })) as {
      base?: string;
      esbuild?: { drop?: string[] };
      build?: { rollupOptions?: { treeshake?: unknown } };
    };
  }

  it('selects base /webui/ for a cloud build', async () => {
    expect((await resolve('cloud', 'build')).base).toBe('/webui/');
  });

  it('keeps base / for a non-cloud build', async () => {
    expect((await resolve('production', 'build')).base).toBe('/');
    expect((await resolve('localbuild', 'build')).base).toBe('/');
  });

  it('keeps production optimizations (treeshake + debugger drop) under --mode cloud', async () => {
    const cfg = await resolve('cloud', 'build');
    expect(cfg.build?.rollupOptions?.treeshake).toEqual({
      moduleSideEffects: 'no-external',
    });
    expect(cfg.esbuild?.drop).toContain('debugger');
  });

  it('does not apply production optimizations for a dev server', async () => {
    const cfg = await resolve('development', 'serve');
    expect(cfg.build?.rollupOptions?.treeshake).toBeUndefined();
    expect(cfg.esbuild).toBeUndefined();
  });
});
