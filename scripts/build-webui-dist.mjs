#!/usr/bin/env node

import { cpSync, existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = resolve(fileURLToPath(new URL('..', import.meta.url)));
const webuiDir = join(repoRoot, 'webui');
const buildDir = join(webuiDir, 'dist'); // Vite output directory

// ── Native feature flags (Track R) ─────────────────────────────────
// --native-fs        Implemented: strips WASM FS/workspace ops from the
//                    bundle (shell provides them natively).
// --native-terminal  Implemented: strips the WASM/PTY terminal transport
//                    from the bundle (shell provides it natively).
// --native-chat      Reserved (R-4) — not yet implemented.
// --native-git       Reserved (R-5) — not yet implemented.
// See docs/WEBUI_DECOUPLING_AUDIT.md and docs/adr-0008-webui-native-seams.md.
const RESERVED_NATIVE_FLAGS = {
  '--native-chat': 'Error: --native-chat is reserved for future Track R work (R-4) and is not yet implemented. See docs/WEBUI_DECOUPLING_AUDIT.md and docs/adr-0008-webui-native-seams.md.',
  '--native-git': 'Error: --native-git is reserved for future Track R work (R-5) and is not yet implemented. See docs/WEBUI_DECOUPLING_AUDIT.md and docs/adr-0008-webui-native-seams.md.',
};

function printHelp() {
  console.log('Usage: node build-webui-dist.mjs [options]');
  console.log('');
  console.log('Options:');
  console.log('  --mode <cloud|local>  Build mode (default: cloud)');
  console.log('  --output <dir>         Output directory (default: dist/<mode>/)');
  console.log('  --api-url <url>        Foundry API base URL (runtime-configurable)');
  console.log('  --ws-url <url>         Foundry WebSocket URL (runtime-configurable)');
  console.log('  --components           Build standalone editor + terminal entries (root base)');
  console.log('  --native-fs            Track R: strip WASM FS/workspace ops (shell provides them natively)');
  console.log('  --ratify-fs            Track R (R-2w): emit the fs portion of capabilities.json as "ratified"');
  console.log('                         (requires --native-fs; makes the dist a shell-servable swap)');
  console.log('  --native-terminal      Track R: strip the terminal transport (shell provides it natively)');
  console.log('  --ratify-terminal      Track R (R-3): emit the terminal portion of capabilities.json as "ratified"');
  console.log('                         (requires --native-terminal; makes the dist a shell-servable swap)');
  console.log('  --help, -h             Show this help message');
  console.log('');
  console.log('Modes:');
  console.log('  cloud   - Sets VITE_SPROUT_MODE=cloud during build');
  console.log('            Produces cloud-mode bundle (remote terminal/SSH enabled)');
  console.log('  local   - Sets VITE_SPROUT_MODE=local during build');
  console.log('            Produces local-mode bundle (local terminal enabled)');
  console.log('');
  console.log('Runtime configuration:');
  console.log('  If --api-url and --ws-url are NOT provided, the built application');
  console.log('  will derive these URLs from window.location at runtime.');
  console.log('  Provide them only if you need to pin a specific backend.');
  console.log('');
  console.log('Native feature flags (Track R):');
  console.log('  --native-fs         Implemented. Removes the WASM FS / workspace');
  console.log('                      operations from the bundle; the host shell supplies');
  console.log('                      them natively. Cannot be combined with --components.');
  console.log('  --ratify-fs       Implemented (R-2w). Marks the fs portion of');
  console.log('                      capabilities.json as status "ratified" (a');
  console.log('                      parity-proven, shell-servable swap) instead of the');
  console.log('                      default "seam-only". Requires --native-fs; with the');
  console.log('                      flag alone it fails fast (exit 1, no build).');
  console.log('  --native-terminal   Implemented (R-3). Removes the terminal transport');
  console.log('                      (PTY WebSocket + WASM terminal tier) from the bundle;');
  console.log('                      the host shell supplies it natively. Cannot be');
  console.log('                      combined with --components.');
  console.log('  --ratify-terminal   Implemented (R-3). Marks the terminal portion of');
  console.log('                      capabilities.json as status "ratified" (a');
  console.log('                      parity-proven, shell-servable swap) instead of the');
  console.log('                      default "seam-only". Requires --native-terminal; with');
  console.log('                      the flag alone it fails fast (exit 1, no build).');
  console.log('  --native-chat       Reserved (R-4). Not yet implemented — fails fast');
  console.log('  --native-git        Reserved (R-5). Not yet implemented — fails fast');
  console.log('  Reserved flags cause exit 1 before any build step (no npm/vite run).');
  console.log('');
  console.log('  capabilities.json   Written to the output dir ONLY when a --native-*');
  console.log('                      flag excludes a portion (e.g. --native-fs -> "fs").');
  console.log('                      Absence of the file = full webui dist, nothing excluded.');
  console.log('                      Each excluded entry carries status "seam-only" (a build-time');
  console.log('                      artifact; a shell must not serve it until the R-2 parity');
  console.log('                      gate ratifies the swap, status "ratified").');
  console.log('                      Add --ratify-fs to emit the fs entry as "ratified" —');
  console.log('                      the R-2w parity-proven, shell-servable swap. Add');
  console.log('                      --ratify-terminal to emit the terminal entry as');
  console.log('                      "ratified" (the R-3 parity-proven, shell-servable swap).');
  console.log('  Rollback            Rebuild with the flag omitted to restore the portion.');
  console.log('');
  console.log('Examples:');
  console.log('  node build-webui-dist.mjs                 # Build cloud-mode to dist/cloud/');
  console.log('  node build-webui-dist.mjs --mode local    # Build local-mode to dist/local/');
  console.log('  node build-webui-dist.mjs --mode cloud --output ./release');
  console.log('  node build-webui-dist.mjs --api-url https://api.example.com/api --ws-url wss://api.example.com/ws');
  console.log('  node build-webui-dist.mjs --native-fs     # Cloud build with WASM FS stripped');
  console.log('  node build-webui-dist.mjs --native-fs --ratify-fs  # fs portion ratified (shell-servable)');
  console.log('  node build-webui-dist.mjs --native-terminal # Cloud build with terminal transport stripped');
  console.log('  node build-webui-dist.mjs --native-fs --native-terminal  # additive: fs + terminal stripped');
  console.log('  node build-webui-dist.mjs --native-terminal --ratify-terminal  # terminal portion ratified (shell-servable)');
}

/**
 * Parse CLI arguments into an options object. Pure and testable: --help is
 * signalled via `{ help: true }` (no process.exit) so tests can assert it.
 * Any unrecognized `--*` token is recorded in `unknownArgs` (validation in
 * validateArgs() turns them into errors). Non-flag tokens are ignored as today.
 */
export function parseArgs(argv) {
  const opts = {
    mode: 'cloud',
    outputDir: '',
    foundryApiUrl: undefined,
    foundryWsUrl: undefined,
    components: false,
    nativeFs: false,
    ratifyFs: false,
    nativeTerminal: false,
    ratifyTerminal: false,
    nativeChat: false,
    nativeGit: false,
    help: false,
    unknownArgs: [],
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--mode' && i + 1 < argv.length) {
      opts.mode = argv[i + 1];
      i++;
    } else if (arg === '--output' && i + 1 < argv.length) {
      opts.outputDir = argv[i + 1];
      i++;
    } else if (arg === '--api-url' && i + 1 < argv.length) {
      opts.foundryApiUrl = argv[i + 1];
      i++;
    } else if (arg === '--ws-url' && i + 1 < argv.length) {
      opts.foundryWsUrl = argv[i + 1];
      i++;
    } else if (arg === '--components') {
      // E-M3: build ONLY the standalone editor + terminal entries with
      // root base (Sprout Studio serves them at /, not under /webui/).
      // Output goes to <outputDir>/components/.
      opts.components = true;
    } else if (arg === '--native-fs') {
      opts.nativeFs = true;
    } else if (arg === '--ratify-fs') {
      // Track R (R-2w): emit the fs portion of capabilities.json with
      // status "ratified" (a parity-proven, shell-servable swap) instead of
      // "seam-only". Requires --native-fs (validated in validateArgs).
      opts.ratifyFs = true;
    } else if (arg === '--native-terminal') {
      // Track R (R-3): strip the terminal transport (PTY WS + WASM terminal
      // tier) from the bundle; the shell provides it natively. Sets
      // VITE_SPROUT_NATIVE_TERMINAL=1 for the Vite build (enables
      // nativeTerminalStubAliases in webui/vite.config.ts).
      opts.nativeTerminal = true;
    } else if (arg === '--ratify-terminal') {
      // Track R (R-3): emit the terminal portion of capabilities.json with
      // status "ratified" (a parity-proven, shell-servable swap) instead of
      // "seam-only". Requires --native-terminal (validated in validateArgs).
      opts.ratifyTerminal = true;
    } else if (arg === '--native-chat') {
      opts.nativeChat = true;
    } else if (arg === '--native-git') {
      opts.nativeGit = true;
    } else if (arg === '--help' || arg === '-h') {
      opts.help = true;
    } else if (arg.startsWith('--')) {
      opts.unknownArgs.push(arg);
    }
    // Any other token (no leading --): left unhandled, exactly as today.
  }

  if (opts.components) {
    opts.mode = 'components';
  }

  return opts;
}

/** Validate parsed options. Returns an array of error strings (empty = valid). */
export function validateArgs(opts) {
  const errors = [];

  for (const [flag, optKey] of [
    ['--native-chat', 'nativeChat'],
    ['--native-git', 'nativeGit'],
  ]) {
    if (opts[optKey]) {
      errors.push(RESERVED_NATIVE_FLAGS[flag]);
    }
  }

  for (const unknown of opts.unknownArgs) {
    errors.push(`Error: Unknown option '${unknown}'. Run with --help for usage.`);
  }

  if (opts.nativeFs && opts.components) {
    errors.push('Error: --native-fs cannot be combined with --components (standalone component entries are not the app bundle).');
  }

  // Track R (R-3): same prohibition as --native-fs — the standalone
  // component entries are not the app bundle, so the terminal-seam build
  // cannot target them either.
  if (opts.nativeTerminal && opts.components) {
    errors.push('Error: --native-terminal cannot be combined with --components (standalone component entries are not the app bundle).');
  }

  // Track R (R-2w): --ratify-fs ratifies the fs portion of a --native-fs build.
  // It is meaningless (and a no-op that would be a lie) without --native-fs,
  // so it fails fast like the other validators — BEFORE any build step.
  if (opts.ratifyFs && !opts.nativeFs) {
    errors.push('Error: --ratify-fs requires --native-fs.');
  }

  // Track R (R-3): --ratify-terminal ratifies the terminal portion of a
  // --native-terminal build. Meaningless without --native-terminal, so it
  // fails fast like the other validators — BEFORE any build step.
  if (opts.ratifyTerminal && !opts.nativeTerminal) {
    errors.push('Error: --ratify-terminal requires --native-terminal.');
  }

  if (opts.mode !== 'cloud' && opts.mode !== 'local' && opts.mode !== 'components') {
    errors.push(`Error: Invalid mode '${opts.mode}'. Must be 'cloud', 'local', or 'components'.`);
  }

  return errors;
}

/**
 * Build the Track R capability manifest (pure). `excluded` is empty by default.
 *
 * Track R (R-2w): when `opts.ratifyFs` is set (i.e. `--native-fs --ratify-fs`),
 * the fs entry carries `status: "ratified"` (a parity-proven, shell-servable
 * swap) instead of the default `"seam-only"` (build-time artifact only). The
 * `notes` field records which mode produced the entry.
 *
 * Track R (R-3): `opts.nativeTerminal` adds a `terminal` entry AFTER any `fs`
 * entry (flags are additive; entry order follows the fs → terminal order of
 * the code). `opts.ratifyTerminal` flips its status to "ratified".
 */
export function buildCapabilityManifest(opts) {
  const excluded = [];
  if (opts.nativeFs) {
    const ratified = Boolean(opts.ratifyFs);
    excluded.push({
      portion: 'fs',
      flag: '--native-fs',
      replacedBy: 'native',
      hardExclusion: true,
      status: ratified ? 'ratified' : 'seam-only',
      notes: ratified
        ? 'R-2w ratified: WASM FS / workspace ops provided natively by the shell; ' +
          'the webui defers file-tree open/browse/read/write/save to the bridge ' +
          'files channel (see docs/adr-0008-webui-native-seams.md, R-2w deferral wiring).'
        : 'WASM FS / workspace ops provided natively by the shell; FS modules stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)',
    });
  }
  if (opts.nativeTerminal) {
    const ratified = Boolean(opts.ratifyTerminal);
    excluded.push({
      portion: 'terminal',
      flag: '--native-terminal',
      replacedBy: 'native',
      hardExclusion: true,
      status: ratified ? 'ratified' : 'seam-only',
      notes: ratified
        ? 'R-3 ratified: parity-proven swap — the WASM/PTY terminal transport is ' +
          'provided natively by the shell; the terminal module is stubbed out of ' +
          'the bundle (see docs/adr-0008-webui-native-seams.md, terminal seam).'
        : 'WASM/PTY terminal transport provided natively by the shell; terminal modules stubbed out of the bundle (see docs/WEBUI_DECOUPLING_AUDIT.md)',
    });
  }
  return {
    schemaVersion: 1,
    generatedAt: new Date().toISOString(),
    buildMode: opts.mode,
    excluded,
  };
}

/**
 * Write capabilities.json into outputDir (2-space indent) and return its path.
 * Emits the file only when the manifest excludes at least one portion; a
 * default build produces no capabilities.json (absence = nothing excluded)
 * and returns null BEFORE creating anything on disk. When it does write, the
 * output dir is created if missing (the build flow cleans/creates it first;
 * this keeps direct callers working).
 */
export function writeCapabilityManifest(outputDir, opts) {
  const manifest = buildCapabilityManifest(opts);
  if (manifest.excluded.length === 0) {
    return null;
  }
  mkdirSync(outputDir, { recursive: true });
  const path = join(outputDir, 'capabilities.json');
  writeFileSync(path, JSON.stringify(manifest, null, 2));
  console.log('  ✓ capabilities.json');
  for (const entry of manifest.excluded) {
    console.log(`    excluded: ${entry.portion} (replaced by ${entry.replacedBy})`);
  }
  return path;
}

function run(command, argsList, cwd, extraEnv = {}) {
  const executable = process.platform === 'win32' && command === 'npm' ? 'npm.cmd' : command;
  console.log(`↪ ${executable} ${argsList.join(' ')} (cwd: ${cwd})`);
  const result = spawnSync(executable, argsList, {
    cwd,
    stdio: 'inherit',
    env: { ...process.env, ...extraEnv },
    shell: process.platform === 'win32',
  });
  if (result.error) {
    console.error(`Command failed to start: ${executable} ${argsList.join(' ')}`);
    console.error(result.error);
    process.exit(1);
  }
  if (result.signal) {
    console.error(`Command terminated by signal ${result.signal}: ${executable} ${argsList.join(' ')}`);
    process.exit(1);
  }
  if (result.status !== 0) {
    console.error(`Command failed with exit code ${result.status ?? 1}: ${executable} ${argsList.join(' ')}`);
    process.exit(result.status ?? 1);
  }
}

function cleanOutputDirectory(dir) {
  console.log(`🧹 Cleaning output directory: ${dir}`);

  const resolvedDir = resolve(dir);

  // If the output directory is within the repo, it's safe to clean.
  // Only apply extra safety checks for paths outside the repo.
  if (!resolvedDir.startsWith(repoRoot + '/')) {
    // Safety checks for external output paths: never delete critical directories
    const dangerousPaths = [
      '/',
      '/usr',
      '/var',
      '/etc',
      '/opt',
      '/home',
      '/tmp',
      process.env.HOME || '',
      repoRoot,
    ];

    for (const dangerous of dangerousPaths) {
      if (!dangerous) continue;
      const resolvedDangerous = resolve(dangerous);
      if (resolvedDir === resolvedDangerous || resolvedDir.startsWith(resolvedDangerous + '/')) {
        console.error(`Error: Refusing to delete '${dir}' — inside protected path '${dangerous}'`);
        process.exit(1);
      }
    }
  }

  if (dir.length < 5) {
    console.error(`Error: Directory path '${dir}' looks too short to be safe`);
    process.exit(1);
  }

  if (existsSync(dir)) {
    const stats = lstatSync(dir, { throwIfNoEntry: false });
    if (stats && stats.isSymbolicLink()) {
      console.error(`Error: '${dir}' is a symbolic link. Refusing to follow and delete.`);
      process.exit(1);
    }
    rmSync(dir, { recursive: true, force: true });
    console.log('  ✓ Existing directory removed');
  }

  mkdirSync(dir, { recursive: true });
  console.log('  ✓ Directory ready');
}

function copyBuildOutput(sourceDir, targetDir) {
  console.log(`📁 Copying build assets to ${targetDir}...`);

  if (!existsSync(sourceDir)) {
    console.error(`Error: Build directory not found: ${sourceDir}`);
    console.error('Make sure the React build succeeded before copying.');
    process.exit(1);
  }

  for (const entry of readdirSync(sourceDir, { withFileTypes: true })) {
    cpSync(join(sourceDir, entry.name), join(targetDir, entry.name), { recursive: true });
  }

  console.log('  ✓ Build assets copied');
}

function copyEntry(sourceDir, targetDir, name) {
  if (!existsSync(join(sourceDir, name))) {
    console.error(`Error: expected entry '${name}' missing from component build`);
    process.exit(1);
  }
  cpSync(join(sourceDir, name), join(targetDir, name));
}

function copyWasmFiles(targetDir) {
  console.log('📦 Copying WASM files...');

  const wasmDir = join(webuiDir, 'public', 'wasm');
  const targetWasmDir = join(targetDir, 'wasm');

  if (!existsSync(wasmDir)) {
    console.log('  ⚠ WASM directory not found, skipping');
    return;
  }

  mkdirSync(targetWasmDir, { recursive: true });

  const wasmFile = join(wasmDir, 'sprout.wasm');
  const wasmExecFile = join(wasmDir, 'wasm_exec.js');

  if (existsSync(wasmFile)) {
    cpSync(wasmFile, join(targetWasmDir, 'sprout.wasm'));
    console.log('  ✓ sprout.wasm');
  } else {
    console.log('  ⚠ sprout.wasm not found, skipping');
  }

  if (existsSync(wasmExecFile)) {
    cpSync(wasmExecFile, join(targetWasmDir, 'wasm_exec.js'));
    console.log('  ✓ wasm_exec.js');
  } else {
    console.log('  ⚠ wasm_exec.js not found, skipping');
  }

  const embeddingWasmFile = join(wasmDir, 'embedding.wasm');
  if (existsSync(embeddingWasmFile)) {
    cpSync(embeddingWasmFile, join(targetWasmDir, 'embedding.wasm'));
    console.log('  ✓ embedding.wasm');
  } else {
    console.log('  ⚠ embedding.wasm not found, skipping (lazy-load module)');
  }

  // Remove stale version.json that CRA may have copied from public/wasm/.
  // The authoritative version.json is generated at the dist root by generateVersionJson().
  const staleVersionJson = join(targetWasmDir, 'version.json');
  if (existsSync(staleVersionJson)) {
    rmSync(staleVersionJson);
  }

  // Verify WASM files were successfully copied to the output directory
  verifyWasmFiles(targetWasmDir);
}

function verifyWasmFiles(targetWasmDir) {
  console.log('🔍 Verifying WASM files in output...');

  const expectedFiles = ['sprout.wasm', 'wasm_exec.js'];
  let allPresent = true;

  for (const file of expectedFiles) {
    const filePath = join(targetWasmDir, file);
    if (existsSync(filePath)) {
      console.log(`  ✓ ${file} present in ${targetWasmDir}`);
    } else {
      console.error(`  ✗ ${file} MISSING from ${targetWasmDir}`);
      allPresent = false;
    }
  }

  if (!allPresent) {
    console.error('');
    console.error('Error: WASM files were not successfully copied to the output directory.');
    console.error(`Expected files in ${targetWasmDir}:`);
    for (const file of expectedFiles) {
      console.error(`  - ${file}`);
    }
    process.exit(1);
  }
}

function getGitTag() {
  const result = spawnSync('git', ['describe', '--tags', '--abbrev=0'], {
    cwd: repoRoot,
    stdio: 'pipe',
  });
  if (result.status === 0) {
    return result.stdout.toString().trim();
  }
  return '';
}

function getGitCommit() {
  const result = spawnSync('git', ['rev-parse', '--short', 'HEAD'], {
    cwd: repoRoot,
    stdio: 'pipe',
  });
  if (result.status === 0) {
    return result.stdout.toString().trim();
  }
  return '';
}

function getBuildDate() {
  return new Date().toISOString();
}

function generateVersionJson(targetDir, buildMode) {
  console.log('📝 Generating version.json...');

  const tag = getGitTag();
  const commit = getGitCommit();
  const date = getBuildDate();

  // If no tag, use commit hash as version
  const version = tag || (commit ? `dev-${commit}` : 'unknown');

  const versionData = {
    version,
    commit,
    buildDate: date,
    gitTag: tag,
    mode: buildMode,
  };

  const versionFile = join(targetDir, 'version.json');
  writeFileSync(versionFile, JSON.stringify(versionData, null, 2));

  console.log('  ✓ version.json');
  console.log(`    version: ${version}`);
  console.log(`    commit: ${commit}`);
  console.log(`    buildDate: ${date}`);
  console.log(`    gitTag: ${tag}`);
  console.log(`    mode: ${buildMode}`);
}

function getDirectorySize(dir) {
  const result = spawnSync('du', ['-sk', dir], {
    stdio: 'pipe',
    shell: true,
  });
  if (result.status === 0) {
    const sizeKb = parseInt(result.stdout.toString().split('\t')[0], 10);
    if (!isNaN(sizeKb)) {
      if (sizeKb < 1024) {
        return `${sizeKb}KB`;
      } else {
        return `${(sizeKb / 1024).toFixed(1)}MB`;
      }
    }
  }
  return 'unknown';
}

function postProcessBrowserConfig(targetDir) {
  console.log('📝 Post-processing browserconfig.xml...');

  const browserConfigPath = join(targetDir, 'browserconfig.xml');

  if (!existsSync(browserConfigPath)) {
    console.log('  ℹ browserconfig.xml not found, skipping');
    return;
  }

  let xml = readFileSync(browserConfigPath, 'utf-8');

  // Replace %PUBLIC_URL% placeholders with empty string (app served from root /)
  const beforeLength = xml.length;
  xml = xml.replace(/%PUBLIC_URL%/g, '');
  const afterLength = xml.length;

  if (beforeLength !== afterLength) {
    console.log('  ✓ Replaced %PUBLIC_URL% placeholders in browserconfig.xml');
  } else {
    console.log('  ℹ No %PUBLIC_URL% placeholders found in browserconfig.xml');
  }

  writeFileSync(browserConfigPath, xml, 'utf-8');
  console.log('  ✓ browserconfig.xml updated');
}

function postProcessIndexHtml(targetDir, buildMode) {
  console.log('📝 Post-processing index.html...');

  const indexHtmlPath = join(targetDir, 'index.html');

  if (!existsSync(indexHtmlPath)) {
    console.error(`Error: index.html not found at ${indexHtmlPath}`);
    process.exit(1);
  }

  let html = readFileSync(indexHtmlPath, 'utf-8');

  // Vite builds don't have %PUBLIC_URL% placeholders, so no processing needed
  console.log('  ✓ index.html requires no post-processing (Vite build)');

  writeFileSync(indexHtmlPath, html, 'utf-8');
}

// ── Dist Layout Verification (SP-015-R6) ───────────────────────────
// Verifies the output directory matches the canonical dist-bundle layout
// documented in docs/DIST_BUNDLE_LAYOUT.md.

function verifyDistLayout(outputDir) {
  console.log('🔍 Verifying canonical dist-bundle layout...');

  const required = [
    { path: 'index.html', desc: 'SPA entry point' },
    { path: 'assets', desc: 'Vite build output (JS/CSS)', isDir: true },
    { path: 'wasm', desc: 'Go WASM modules', isDir: true },
    { path: 'wasm/wasm_exec.js', desc: 'Go WASM runtime' },
    { path: 'version.json', desc: 'Build metadata' },
  ];

  // Optional files — warn if missing but don't fail
  const optional = [
    { path: 'wasm/sprout.wasm', desc: 'Shell WASM binary' },
    { path: 'wasm/embedding.wasm', desc: 'Embedding WASM binary (SP-045-3)' },
    { path: 'manifest.json', desc: 'PWA manifest' },
    { path: 'sw.js', desc: 'Service worker' },
    { path: 'capabilities.json', desc: 'Track R native capability manifest (only present when a --native-* flag was used)' },
  ];

  let allRequired = true;

  for (const item of required) {
    const fullPath = join(outputDir, item.path);
    if (existsSync(fullPath)) {
      console.log(`  ✓ ${item.path} — ${item.desc}`);
    } else {
      console.error(`  ✗ ${item.path} MISSING — ${item.desc}`);
      allRequired = false;
    }
  }

  for (const item of optional) {
    const fullPath = join(outputDir, item.path);
    if (existsSync(fullPath)) {
      console.log(`  ✓ ${item.path} — ${item.desc}`);
    } else {
      console.warn(`  ⚠ ${item.path} not found — ${item.desc} (optional)`);
    }
  }

  // Verify assets/ has at least one .js file
  const assetsDir = join(outputDir, 'assets');
  if (existsSync(assetsDir)) {
    const jsFiles = readdirSync(assetsDir).filter((f) => f.endsWith('.js'));
    if (jsFiles.length === 0) {
      console.error('  ✗ assets/ has no .js files — build may have failed');
      allRequired = false;
    } else {
      console.log(`  ✓ assets/ contains ${jsFiles.length} JS file(s)`);
    }
  }

  if (!allRequired) {
    console.error('');
    console.error('Error: Dist-bundle layout verification failed.');
    console.error('See docs/DIST_BUNDLE_LAYOUT.md for the canonical structure.');
    process.exit(1);
  }

  console.log('  ✓ Canonical layout verified.');
}

function main(opts) {
  const mode = opts.mode;
  const outputDir = opts.outputDir || join(repoRoot, 'dist', mode);
  const foundryApiUrl = opts.foundryApiUrl;
  const foundryWsUrl = opts.foundryWsUrl;

  console.log(`🏗️  Building ${mode}-mode WebUI distribution...`);
  console.log('');

  // Clean output directory
  cleanOutputDirectory(outputDir);
  console.log('');

  // Install dependencies (always run npm ci for reproducible dist builds)
  console.log('📦 Installing dependencies...');
  run('npm', ['ci', '--legacy-peer-deps'], webuiDir);
  console.log('');

  // Set build environment variables
  const buildEnv = {};

  if (mode === 'components') {
    // E-M3: standalone editor + terminal only, root base. Sprout Studio
    // hosts these in scoped WKWebViews served at / (Telegraph), NOT
    // under the platform daemon's /webui/ mount.
    console.log('🔨 Building standalone components (editor + terminal, root base)...');
    const componentsDir = join(outputDir, 'components');
    cleanOutputDirectory(componentsDir);
    run('npx', [
      'vite', 'build', '--mode', 'cloud', '--base', '/',
      '--outDir', join('..', '.components-build'),
    ], webuiDir, { VITE_SPROUT_MODE: 'cloud' });
    // Copy only the component entries + their assets (skip the app).
    const compBuild = join(repoRoot, '.components-build');
    mkdirSync(componentsDir, { recursive: true });
    copyEntry(compBuild, componentsDir, 'editor.html');
    copyEntry(compBuild, componentsDir, 'terminal.html');
    // Copy the full assets dir: component chunks + their shared deps
    // (xterm chunk, codemirror chunks, preload helper).
    cpSync(join(compBuild, 'assets'), join(componentsDir, 'assets'), { recursive: true });
    // Components load wasm lazily from /wasm/ (root base) — the parent
    // dist's wasm/ dir serves them; ensure it exists next to components/.
    if (!existsSync(join(outputDir, 'wasm'))) {
      copyWasmFiles(outputDir);
    }
    rmSync(compBuild, { recursive: true, force: true });
    console.log(`✅ Components built to ${componentsDir}`);
    process.exit(0);
  }

  if (mode === 'cloud') {
    buildEnv.VITE_SPROUT_MODE = 'cloud';
    console.log('🔨 Building React app with Vite in cloud mode (VITE_SPROUT_MODE=cloud)...');
  } else {
    // Explicitly override to prevent env var leak from the shell
    buildEnv.VITE_SPROUT_MODE = 'local';
    console.log('🔨 Building React app with Vite in local mode (VITE_SPROUT_MODE=local)...');
  }

  // Track R: native filesystem — tell the frontend bundle that FS/workspace
  // ops are provided natively by the shell so the WASM FS code paths are
  // stubbed out of the build.
  if (opts.nativeFs) {
    buildEnv.VITE_SPROUT_NATIVE_FS = '1';
    console.log('    VITE_SPROUT_NATIVE_FS=1 (--native-fs: WASM FS stripped, shell provides it)');
  }

  // Track R (R-3): native terminal — tell the frontend bundle that the
  // terminal transport (PTY WebSocket + WASM terminal tier) is provided
  // natively by the shell so the terminal module is stubbed out of the
  // build.
  if (opts.nativeTerminal) {
    buildEnv.VITE_SPROUT_NATIVE_TERMINAL = '1';
    console.log('    VITE_SPROUT_NATIVE_TERMINAL=1 (--native-terminal: terminal transport stripped, shell provides it)');
  }

  // Track R (R-2w): --ratify-fs marks the fs portion of capabilities.json as
  // "ratified" (a parity-proven, shell-servable swap). It does not change the
  // Vite build itself (the bundle is identical to --native-fs); it only
  // changes the capabilities.json manifest that the shell reads at serve time.
  if (opts.ratifyFs) {
    console.log('    capabilities.json fs portion will be emitted as status "ratified" (--ratify-fs)');
  }

  // Track R (R-3): --ratify-terminal marks the terminal portion of
  // capabilities.json as "ratified". Like --ratify-fs, it does not change
  // the Vite build (the bundle is identical to --native-terminal); it only
  // changes the capabilities.json manifest that the shell reads at serve
  // time.
  if (opts.ratifyTerminal) {
    console.log('    capabilities.json terminal portion will be emitted as status "ratified" (--ratify-terminal)');
  }

  // Runtime-configurable Foundry URLs — only bake them in if explicitly provided.
  // When omitted, bootstrapAdapter.ts falls back to window.location at runtime.
  if (foundryApiUrl !== undefined) {
    buildEnv.VITE_FOUNDRY_API_URL = foundryApiUrl;
    console.log(`    VITE_FOUNDRY_API_URL=${foundryApiUrl}`);
  }
  if (foundryWsUrl !== undefined) {
    buildEnv.VITE_FOUNDRY_WS_URL = foundryWsUrl;
    console.log(`    VITE_FOUNDRY_WS_URL=${foundryWsUrl}`);
  }

  // Build React app with Vite
  run('npm', ['run', 'build'], webuiDir, buildEnv);
  console.log('');

  // Copy build output
  copyBuildOutput(buildDir, outputDir);
  console.log('');

  // Post-process browserconfig.xml
  postProcessBrowserConfig(outputDir);
  console.log('');

  // Post-process index.html
  postProcessIndexHtml(outputDir, mode);
  console.log('');

  // Copy WASM files
  copyWasmFiles(outputDir);
  console.log('');

  // Generate version.json
  generateVersionJson(outputDir, mode);
  console.log('');

  // Track R: write capabilities.json (only when a --native-* flag excludes
  // a portion). Absence of the file in a default build means nothing excluded.
  const capabilityPath = writeCapabilityManifest(outputDir, opts);
  console.log('');

  // Verify canonical dist layout (SP-015-R6)
  verifyDistLayout(outputDir);
  console.log('');

  // Print summary
  const size = getDirectorySize(outputDir);
  console.log('');
  console.log('✅ Distribution build complete!');
  console.log('');
  console.log(`Output: ${outputDir}`);
  console.log(`Size: ${size}`);
  console.log(`Mode: ${mode}`);
  console.log('');
  console.log('Contents:');
  console.log('  index.html      - Application entry point');
  console.log('  assets/         - Vite build output (JS, CSS, fonts)');
  console.log('  wasm/           - Go WASM modules (sprout.wasm, embedding.wasm, wasm_exec.js)');
  console.log('  version.json    - Version and build metadata');
  console.log('  manifest.json   - PWA manifest');
  console.log('  sw.js           - Service worker');
  if (capabilityPath) {
    console.log('  capabilities.json - Track R native capability manifest');
  }
  console.log('');
  console.log('See docs/DIST_BUNDLE_LAYOUT.md for the canonical layout spec.');
  console.log('');
}

/** CLI entry point: parse, validate (fail fast), help, then build. */
function cli() {
  const opts = parseArgs(process.argv.slice(2));

  if (opts.help) {
    printHelp();
    process.exit(0);
  }

  // Fail fast on validation errors BEFORE any spawn / npm ci runs.
  const errors = validateArgs(opts);
  if (errors.length > 0) {
    for (const e of errors) {
      console.error(e);
    }
    process.exit(1);
  }

  // Resolve the (possibly relative) output directory to an absolute path.
  opts.outputDir = opts.outputDir || join(repoRoot, 'dist', opts.mode);
  opts.outputDir = resolve(opts.outputDir);

  main(opts);
}

/**
 * Direct-run guard (realpath-robust): run the CLI only when this module is
 * the launched script. import.meta.url reflects the module's REAL path, while
 * pathToFileURL(process.argv[1]) keeps a SYMLINK path — the old URL-only
 * comparison therefore silently no-oped (exit 0, nothing built) when the
 * script was invoked through a symlink. Resolve both sides with realpathSync
 * so symlinked invocations (in either direction) still fire the guard, while
 * importing the module (e.g. from vitest) still runs nothing.
 */
function isDirectRunCheck() {
  if (!process.argv[1]) return false;
  try {
    const moduleReal = realpathSync(fileURLToPath(import.meta.url));
    const invokedReal = realpathSync(resolve(process.argv[1]));
    // The realpath comparison is authoritative; keep the legacy URL
    // comparison as a harmless OR fallback.
    return (
      invokedReal === moduleReal ||
      import.meta.url === pathToFileURL(process.argv[1]).href
    );
  } catch {
    return false;
  }
}
if (isDirectRunCheck()) {
  try {
    cli();
  } catch (err) {
    console.error('Build failed:', err);
    process.exit(1);
  }
}

export { printHelp, main, cli };
