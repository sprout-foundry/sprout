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
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

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

/**
 * The static import specifiers of a built JS file: `import ... from "..."`,
 * bare `import "..."` and `export ... from "..."`. A dynamic `import("...")`
 * is deliberately NOT matched — that is a lazy boundary, and a chunk reached
 * only through one is not in the importer's static graph. The `(?!\s*\()` on
 * the keyword keeps `import(` out even when the call's argument sits on the
 * next line.
 */
function staticSpecifiers(source) {
  const pattern = /\b(?:import|export)\b(?!\s*\()\s+(?:[^"'();]*?\bfrom\s+)?["']([^"']+)["']/g;
  return [...source.matchAll(pattern)].map((m) => m[1]);
}

/**
 * Every relative file statically reachable from a built entry, following the
 * `./chunks/...` specifiers transitively. This is what "importing the
 * package" means: the browser (or a bundler of the host) fetches this set
 * eagerly when the entry module is evaluated.
 */
function staticImportGraph(entryName, seen = new Set()) {
  const abs = path.join(PACKAGE_DIR, entryName);
  if (!fs.existsSync(abs) || fs.statSync(abs).isDirectory()) return seen;
  if (seen.has(entryName)) return seen;
  seen.add(entryName);
  for (const spec of staticSpecifiers(fs.readFileSync(abs, 'utf-8'))) {
    if (spec.startsWith('.')) {
      // Normalize to posix so the keys match `walk()`'s chunk names (which
      // split on `path.sep`): on Windows `path.relative` yields backslashes,
      // and a `graph.has(file)` identity check against forward-slash names
      // would silently never match.
      const rel = path.relative(PACKAGE_DIR, path.resolve(path.dirname(abs), spec)).split(path.sep).join('/');
      staticImportGraph(rel, seen);
    }
  }
  return seen;
}

/** Markers that identify editor (CodeMirror) code in a built chunk. */
const EDITOR_MARKERS = /@codemirror\/|CodeMirror|cm-editor/;
/** Markers that identify the WASM agent (its loader / instantiation). */
const WASM_MARKERS = /sprout\.wasm|wasm_exec|WebAssembly\.instantiate/;


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

  test('bundles the internal packages from the workspace (dev dependencies only)', () => {
    for (const name of ['@sprout/events', '@sprout/ui']) {
      assert.ok(pkg.devDependencies[name], `builds against ${name} from the workspace`);
      assert.ok(!pkg.dependencies?.[name], `${name} is bundled, not installed by the host`);
    }
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

  test('the providers entry point re-exports the provider wrapper', () => {
    const source = read('packages/workspace/src/providersChunk.ts');
    assert.match(source, /from "\.\.\/\.\.\/\.\.\/webui\/src\/providers\/SproutProviders"/);
    assert.match(source, /SproutProviders/);
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
    const providers = fs.statSync(path.join(DIST, 'providers.js')).size;
    assert.ok(entry < 16 * 1024, `the entry must stay small, was ${entry} bytes`);
    assert.ok(views < 16 * 1024, `the views facade must stay small, was ${views} bytes`);
    assert.ok(providers < 16 * 1024, `the providers facade must stay small, was ${providers} bytes`);

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

// ── Lazy loading (SP-160 Acceptance criteria 5) ────────────────────────

describe('lazy loading: the entry loads no editor or WASM code', { skip: !built && 'packages/workspace/dist not built' }, () => {
  // "Importing the package loads no editor or WASM code until a space opens":
  // importing is evaluating `dist/index.js`, which eager-fetches exactly the
  // static import graph below (transitively). Dynamic imports are the lazy
  // boundary — a chunk reached only through one is NOT in this set.
  const graph = staticImportGraph('dist/index.js');
  const contents = [...graph].map((file) => [file, fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8')]);

  // The chunks that carry the editor / WASM loader, identified across the whole
  // artifact. The size- and identity-based assertions below do not depend on
  // the markers surviving minification, so a re-bundled editor cannot slip past
  // by having its literals renamed.
  const allChunks = walk(DIST).filter((file) => file.startsWith('dist/chunks/') && file.endsWith('.js'));
  const chunkSources = allChunks.map((file) => [file, fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8')]);
  const heavyChunks = chunkSources
    .filter(([, source]) => EDITOR_MARKERS.test(source) || WASM_MARKERS.test(source))
    .map(([file]) => file);

  test('the entry graph reaches files (the walk is real, not vacuous)', () => {
    assert.ok(graph.size >= 2, `the entry statically imports at least one chunk, reached ${graph.size}`);
  });

  test('no file in the entry graph contains editor code', () => {
    const offenders = contents.filter(([, source]) => EDITOR_MARKERS.test(source)).map(([file]) => file);
    assert.deepEqual(offenders, [], `editor code is eagerly imported by the entry: ${offenders.join(', ')}`);
  });

  test('no file in the entry graph contains the WASM loader', () => {
    const offenders = contents.filter(([, source]) => WASM_MARKERS.test(source)).map(([file]) => file);
    assert.deepEqual(offenders, [], `the WASM loader is eagerly imported by the entry: ${offenders.join(', ')}`);
  });

  test('no file in the entry graph is a heavy chunk (a renamed editor cannot evade the marker scan)', () => {
    // Minification concatenates and renames modules, so a chunk that carries
    // the editor could in principle contain no `CodeMirror` literal. A size
    // ceiling is independent of naming: the editor bundle is hundreds of
    // kilobytes, so any eager file above this cap is a regression whatever it
    // is called. This is the assertion that does not depend on the markers.
    const CAP = 64 * 1024;
    const offenders = contents
      .map(([file]) => [file, fs.statSync(path.join(PACKAGE_DIR, file)).size])
      .filter(([, size]) => size > CAP)
      .map(([file, size]) => `${file} (${size} bytes)`);
    assert.deepEqual(offenders, [], `the entry eagerly loads a heavy chunk: ${offenders.join(', ')}`);
  });

  test('the editor and WASM chunks exist but are NOT in the entry graph', () => {
    // The split must be real, not a deletion of the functionality: the editor
    // and the WASM agent are still built, in chunks the entry does not
    // statically import. Asserting the known-heavy chunks are absent from the
    // static graph checks the boundary by chunk identity, not by re-reading
    // markers the minifier could have renamed.
    assert.ok(heavyChunks.length > 0, 'a chunk carries the editor or WASM loader');
    const eagerlyReached = heavyChunks.filter((file) => graph.has(file));
    assert.deepEqual(
      eagerlyReached,
      [],
      `a heavy chunk is in the entry's static graph: ${eagerlyReached.join(', ')}`,
    );
  });

  test('the heavy chunks are loaded by the views/providers entries, not the package entry', () => {
    // The lazy boundary in this package is the entry point, not a dynamic
    // import: a host imports the `views` (or `providers`) entry when it mounts
    // a space, and that entry pulls the heavy chunks. Prove the split is real
    // by chunk identity — at least one heavy chunk is statically reachable from
    // the `views` entry and none from the package entry (asserted above).
    const viewsGraph = staticImportGraph('dist/views.js');
    const reachableFromViews = heavyChunks.filter((file) => viewsGraph.has(file));
    assert.ok(
      reachableFromViews.length > 0,
      `the views entry must load the heavy chunks (heavy: ${heavyChunks.join(', ')})`,
    );
  });
});

// ── No platform calls on import ────────────────────────────────────────

describe('no platform calls on import', { skip: !built && 'packages/workspace/dist not built' }, () => {
  // Importing the package must not reach the bootstrap adapter (its module
  // scope fetches /api/bootstrap and installs an adapter) nor resolve platform
  // entitlements (the cloud host's /billing/status). Both used to sit in the
  // entry's static graph and run at module scope; the resolved data now arrives
  // through the host's transport instead, and entitlements resolve lazily.
  const graph = staticImportGraph('dist/index.js');
  const sources = [...graph].map((file) => [file, fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8')]);

  test('the entry graph reaches files (the walk is real, not vacuous)', () => {
    assert.ok(graph.size >= 2, `the entry statically imports at least one chunk, reached ${graph.size}`);
  });

  test('no file in the entry graph carries the bootstrap-adapter fetch on import', () => {
    const offenders = sources
      .filter(([, source]) => /bootstrapAdapter/.test(source) || /\/api\/bootstrap/.test(source))
      .map(([file]) => file);
    assert.deepEqual(offenders, [], `the bootstrap adapter is in the entry graph: ${offenders.join(', ')}`);
  });

  test('importing the built entry performs no fetch', () => {
    // The behavioral guard: a child node process stubs global.fetch before
    // importing the built entry, then asserts no call was made at import time.
    // Out-of-process keeps the runner's own module cache and globals untouched,
    // and the assertion is exactly what a host observes. A module-scope
    // /api/bootstrap or /billing/status call would make `calls` non-empty.
    const probe = `
      const calls = [];
      globalThis.fetch = (...args) => { calls.push(args); return Promise.resolve({ ok: false }); };
      await import(${JSON.stringify(pathToFileURL(path.join(PACKAGE_DIR, 'dist/index.js')).href)});
      await new Promise((resolve) => setTimeout(resolve, 300));
      process.stdout.write(String(calls.length));
    `;
    const result = spawnSync(process.execPath, ['--input-type=module', '-e', probe], {
      encoding: 'utf-8',
      timeout: 30_000,
    });
    assert.equal(result.status, 0, `the entry import probe exited ${result.status}: ${result.stderr}`);
    assert.equal(result.stdout.trim(), '0', 'importing the package performed a fetch at module scope');
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

// ── Installable by a host ──────────────────────────────────────────────

describe('a host can install the published package', () => {
  test('no installed dependency uses a file: or link: specifier', () => {
    for (const field of [
      'dependencies',
      'peerDependencies',
      'optionalDependencies',
    ]) {
      for (const [name, spec] of Object.entries(pkg[field] ?? {})) {
        assert.ok(
          !/^(file|link):/.test(spec),
          `${field}.${name} is not installable from a registry: ${spec}`,
        );
      }
    }
  });

  test('react and react-dom are peer dependencies, never regular ones', () => {
    for (const name of ['react', 'react-dom']) {
      assert.ok(pkg.peerDependencies?.[name], `${name} is a peer dependency`);
      assert.ok(
        !pkg.dependencies?.[name],
        `${name} must not be a regular dependency`,
      );
    }
  });
});

describe(
  'the build keeps peer dependencies external',
  { skip: !built && 'packages/workspace/dist not built' },
  () => {
    const scripts = () => walk(DIST).filter((file) => file.endsWith('.js'));

    test('no React internals are bundled into dist/', () => {
      const offenders = scripts().filter((file) => {
        const source = fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8');
        return /__SECRET_INTERNALS_DO_NOT_USE|ReactCurrentOwner|react\.production\.min/.test(
          source,
        );
      });
      assert.deepEqual(
        offenders,
        [],
        `React is bundled into: ${offenders.join(', ')}`,
      );
    });

    test('React is imported from the host by bare specifier', () => {
      const imports = scripts().flatMap((file) => {
        const source = fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8');
        return [
          ...source.matchAll(/from\s*["'](react(?:-dom)?(?:\/[^"']*)?)["']/g),
        ].map((m) => m[1]);
      });
      assert.ok(
        imports.includes('react'),
        'the build imports react from the host',
      );
    });

    test(
      'declarations resolve without paths outside the package',
      { todo: 'declarations are not yet bundled (SP-160 §160e)' },
      () => {
        const declarations = walk(DIST).filter((file) =>
          file.endsWith('.d.ts'),
        );
        const offenders = declarations.filter((file) => {
          const source = fs.readFileSync(path.join(PACKAGE_DIR, file), 'utf-8');
          return /from\s*["'](?:(?:\.\.\/)+webui\/|@sprout-foundry\/workspace-webui)/.test(
            source,
          );
        });
        assert.deepEqual(
          offenders,
          [],
          `declarations reference files outside the package: ${offenders.join(', ')}`,
        );
      },
    );
  },
);
