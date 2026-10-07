// @sprout-foundry/workspace build-artifact tests
// ==============================================
// The package is the hosted artifact: a host installs `dist/` and nothing
// else. These tests assert what that artifact contains — the entry, its
// lazily loaded chunks, the type declarations and the publish allowlist —
// and that the scaffold wiring (workspaces, the `make build-all` step) is in
// place.
//
// The assertions read `packages/workspace/dist`, produced by the package's
// own build. `make install` runs the workspace-package build before this file
// and `make build-all` runs it as part of the full build, so the artifact is
// always fresh where these tests matter. A missing `dist/` (a bare `make lint`
// on a fresh checkout) skips the artifact assertions with a message instead of
// failing on an artifact the test did not produce.
//
// Run with: node docs/__tests__/workspace-package.test.js

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// ── Helpers ────────────────────────────────────────────────────────────

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT = path.resolve(__dirname, '../..');
const PACKAGE_DIR = path.join(ROOT, 'packages/workspace');
const DIST = path.join(PACKAGE_DIR, 'dist');

const read = (rel) => fs.readFileSync(path.join(ROOT, rel), 'utf-8');
const readJson = (rel) => JSON.parse(read(rel));
const exists = (rel) => fs.existsSync(path.join(ROOT, rel));

/** Every file under dist/, as posix paths relative to packages/workspace. */
function walk(dir, acc = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, acc);
    else acc.push(path.relative(PACKAGE_DIR, full).split(path.sep).join('/'));
  }
  return acc;
}

/** The built entry points and every `.d.ts`, from the manifest. */
function declaredArtifacts(pkg) {
  const files = [];
  for (const spec of pkg.files) {
    if (spec.endsWith('/')) continue;
    files.push(spec);
  }
  for (const target of Object.values(pkg.exports)) {
    for (const value of Object.values(target)) files.push(value.replace(/^\.\//, ''));
  }
  if (pkg.types) files.push(pkg.types.replace(/^\.\//, ''));
  if (pkg.module) files.push(pkg.module.replace(/^\.\//, ''));
  return [...new Set(files.map((file) => (file.startsWith('dist/') ? file : `dist/${file}`)))];
}

const pkg = readJson('packages/workspace/package.json');
const rootPkg = readJson('package.json');
const built = fs.existsSync(DIST);
if (!built) {
  console.warn(
    '[workspace-package] packages/workspace/dist is absent — skipping artifact assertions. ' +
      'Run `npm run build -w @sprout-foundry/workspace` (or `make build-all`) first.',
  );
}

// ── Package manifest ───────────────────────────────────────────────────

describe('@sprout-foundry/workspace package.json', () => {
  test('is published to GitHub Packages like @sprout-foundry/design', () => {
    const design = readJson('packages/design/package.json');
    assert.equal(pkg.name, '@sprout-foundry/workspace');
    assert.equal(pkg.publishConfig.registry, design.publishConfig.registry);
    assert.equal(pkg.license, design.license);
    assert.equal(pkg.repository.type, design.repository.type);
    assert.equal(pkg.repository.url, design.repository.url);
  });

  test('is an ESM package with a build script', () => {
    assert.equal(pkg.type, 'module');
    assert.match(pkg.scripts.build, /vite build/);
  });

  test('exports the entry with types, ESM only', () => {
    const entry = pkg.exports['.'];
    assert.ok(entry, 'exports has a "." entry');
    assert.equal(entry.import, './dist/index.js');
    assert.equal(entry.types, './dist/index.d.ts');
    assert.equal(entry.require, undefined, 'the package is ESM only');
  });

  test('declares a package manager resolution path into the workspace', () => {
    assert.ok(pkg.dependencies['@sprout/events'], 'depends on @sprout/events');
    assert.ok(pkg.dependencies['@sprout/ui'], 'depends on @sprout/ui');
  });
});

// ── Lockstep version ───────────────────────────────────────────────────

describe('version lockstep with the sprout release', () => {
  test('the package version equals the root sprout version', () => {
    assert.equal(pkg.version, rootPkg.version, 'packages/workspace/package.json must track the root sprout version');
  });

  test('npm workspaces resolve the package (glob covers packages/*)', () => {
    assert.ok(rootPkg.workspaces.includes('packages/*'), 'the workspace glob covers the package');
  });

  test('the lockfile records the package at the sprout version', () => {
    const lock = readJson('package-lock.json');
    const entry = lock.packages?.['packages/workspace'];
    assert.ok(entry, 'package-lock.json has a packages/workspace entry');
    assert.equal(entry.name, pkg.name);
    assert.equal(entry.version, rootPkg.version);
  });
});

// ── Build wiring ───────────────────────────────────────────────────────

describe('build wiring', () => {
  test('the Makefile builds the package and depends on it from build-all', () => {
    const makefile = read('Makefile');
    assert.match(makefile, /^build-workspace-package:/m, 'Makefile has a build-workspace-package target');
    assert.match(
      makefile,
      /^build-all: .*\bbuild-workspace-package\b/m,
      'build-all depends on build-workspace-package',
    );
  });

  test('the package is built after its dependencies (events, ui, webui)', () => {
    const makefile = read('Makefile');
    const target = makefile.match(/^build-workspace-package:(.*)$/m);
    assert.ok(target, 'build-workspace-package has a prerequisite list');
    assert.match(
      target[1],
      /^[ \t]*deploy-ui\b/,
      'build-workspace-package depends on deploy-ui (which builds @sprout/events and @sprout/ui first)',
    );
  });
});

// ── Source entry points ────────────────────────────────────────────────

describe('package sources', () => {
  test('the entry re-exports the public host entry point', () => {
    const source = read('packages/workspace/src/index.ts');
    assert.match(source, /export \* from '\.\/host';/);
  });

  test('the views entry point re-exports the public views module', () => {
    const source = read('packages/workspace/src/viewsChunk.ts');
    assert.match(source, /from '\.\.\/\.\.\/\.\.\/webui\/src\/views\/index'/);
  });

  test('the host entry point re-exports the public host module', () => {
    const source = read('packages/workspace/src/host.ts');
    assert.match(source, /from '\.\.\/\.\.\/\.\.\/webui\/src\/host\/index'/);
  });
});

// ── Build artifacts ────────────────────────────────────────────────────

describe('build artifacts', { skip: !built && 'packages/workspace/dist not built' }, () => {
  test('the declared entry points and type declarations were emitted', () => {
    for (const file of declaredArtifacts(pkg)) {
      if (file === 'dist/README.md') continue;
      assert.ok(fs.existsSync(path.join(PACKAGE_DIR, file)), `missing emitted artifact: ${file}`);
    }
  });

  test('the entry declares the host contract and the package version', () => {
    const types = fs.readFileSync(path.join(DIST, 'index.d.ts'), 'utf-8');
    assert.match(types, /SproutHost/, 'the declaration names the host contract');
    assert.match(types, /WORKSPACE_PACKAGE_NAME/, 'the declaration names the package identity');
    assert.match(types, /WORKSPACE_PACKAGE_VERSION/, 'the declaration names the package version');
  });

  test('the flat declaration resolves its own relative re-exports', () => {
    // The published entry is dist/index.d.ts and the declaration tree it was
    // flattened from lives under dist/chunks/declarations. A relative
    // specifier left pointing at a sibling that is no longer there (e.g.
    // './host') is a broken `exports.types`, so every relative import in the
    // entry must resolve on disk.
    const types = fs.readFileSync(path.join(DIST, 'index.d.ts'), 'utf-8');
    const specifiers = [...types.matchAll(/(?:from|import\()\s*'(\.[^']+)'/g)].map((m) => m[1]);
    for (const spec of specifiers) {
      const target = path.join(DIST, spec);
      assert.ok(
        fs.existsSync(target) || fs.existsSync(`${target}.d.ts`),
        `the entry's relative specifier does not resolve: ${spec}`,
      );
    }
  });

  test('the version constants are declared exactly once', () => {
    const types = fs.readFileSync(path.join(DIST, 'index.d.ts'), 'utf-8');
    const declarations = [...types.matchAll(/declare const (WORKSPACE_PACKAGE_\w+)/g)].map((m) => m[1]);
    assert.deepEqual(
      [...declarations].sort(),
      ['WORKSPACE_PACKAGE_NAME', 'WORKSPACE_PACKAGE_VERSION'],
      'each version constant is declared exactly once',
    );
  });

  test('the entry JS resolves its chunks relatively (no bare first-party imports)', () => {
    const entry = fs.readFileSync(path.join(DIST, 'index.js'), 'utf-8');
    const specifiers = [...entry.matchAll(/from\s+"([^"]+)"/g)].map((m) => m[1]);
    assert.ok(specifiers.length > 0, 'the entry imports at least one chunk');
    for (const spec of specifiers) {
      assert.ok(spec.startsWith('./'), `entry import is relative: ${spec}`);
    }
  });

  test('code-splitting: the entry is small and the views are their own chunks', () => {
    const entry = fs.statSync(path.join(DIST, 'index.js')).size;
    const views = fs.statSync(path.join(DIST, 'views.js')).size;
    assert.ok(entry < 16 * 1024, `the entry must stay small, was ${entry} bytes`);
    assert.ok(views < 16 * 1024, `the views facade must stay small, was ${views} bytes`);

    const chunks = walk(DIST).filter((file) => /^dist\/chunks\/.*\.js$/.test(file));
    assert.ok(chunks.length > 0, 'the build emitted at least one lazily loaded chunk');
    const heavy = chunks.some((file) => fs.statSync(path.join(PACKAGE_DIR, file)).size > 64 * 1024);
    assert.ok(heavy, 'at least one chunk carries the heavy views (the split is real)');
  });

  test('no unexpected files are emitted (dist is within the publish allowlist)', () => {
    const allowedExact = new Set(declaredArtifacts(pkg));
    const allowedPrefixes = pkg.files.filter((spec) => spec.endsWith('/')).map((spec) => spec);
    const unexpected = walk(DIST).filter((file) => {
      if (allowedExact.has(file)) return false;
      return !allowedPrefixes.some((prefix) => file.startsWith(prefix));
    });
    assert.deepEqual(unexpected, [], `unexpected files in dist/: ${unexpected.join(', ')}`);
  });

  test('the artifact ships no source maps or test files', () => {
    const offenders = walk(DIST).filter((file) => /\.map$|\.test\.|\.spec\./.test(file));
    assert.deepEqual(offenders, [], `unexpected files in dist/: ${offenders.join(', ')}`);
  });

  test('the emitted stylesheet carries the views styles', () => {
    // The stylesheet is emitted only because `ViewsLayout.tsx` imports its CSS
    // (the layout is the views entry's own component); assert the content
    // survived the library build rather than asserting a live JS reference,
    // which vite provides through the manifest/`cssCodeSplit` machinery this
    // scaffold does not expose in the published entry semantics yet.
    const chunks = walk(DIST).filter((file) => file.startsWith('dist/chunks/') && file.endsWith('.css'));
    assert.ok(chunks.length > 0, 'the build emits the views stylesheet');
    const css = chunks.map((file) => fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8')).join('\n');
    assert.ok(css.includes('.sprout-views-layout'), 'the emitted stylesheet carries the layout styles');
  });
});

describe('source is not published', { skip: !built && 'packages/workspace/dist not built' }, () => {
  test('the publish allowlist never includes src/ or the vite config', () => {
    for (const spec of pkg.files) {
      assert.ok(!spec.startsWith('src'), `files must not publish source: ${spec}`);
      assert.ok(!spec.includes('tsconfig') && !spec.includes('vite.config'), `files must not publish config: ${spec}`);
    }
  });
});
