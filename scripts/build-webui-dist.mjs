#!/usr/bin/env node

import { cpSync, existsSync, mkdirSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const webuiDir = join(repoRoot, 'webui');
const buildDir = join(webuiDir, 'build');

// Parse command line arguments
const args = process.argv.slice(2);
let mode = 'cloud'; // default
let outputDir = '';

for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === '--mode' && i + 1 < args.length) {
    mode = args[i + 1];
    i++;
  } else if (arg === '--output' && i + 1 < args.length) {
    outputDir = args[i + 1];
    i++;
  } else if (arg === '--help' || arg === '-h') {
    console.log('Usage: node build-webui-dist.mjs [options]');
    console.log('');
    console.log('Options:');
    console.log('  --mode <cloud|local>  Build mode (default: cloud)');
    console.log('  --output <dir>         Output directory (default: dist/<mode>/)');
    console.log('  --help, -h             Show this help message');
    console.log('');
    console.log('Modes:');
    console.log('  cloud   - Sets REACT_APP_SPROUT_MODE=cloud during build');
    console.log('            Produces cloud-mode bundle (remote terminal/SSH enabled)');
    console.log('  local   - Omits REACT_APP_SPROUT_MODE during build');
    console.log('            Produces local-mode bundle (local terminal enabled)');
    console.log('');
    console.log('Examples:');
    console.log('  node build-webui-dist.mjs                 # Build cloud-mode to dist/cloud/');
    console.log('  node build-webui-dist.mjs --mode local    # Build local-mode to dist/local/');
    console.log('  node build-webui-dist.mjs --mode cloud --output ./release');
    process.exit(0);
  }
}

// Validate mode
if (mode !== 'cloud' && mode !== 'local') {
  console.error(`Error: Invalid mode '${mode}'. Must be 'cloud' or 'local'.`);
  process.exit(1);
}

// Set default output directory if not specified
if (!outputDir) {
  outputDir = join(repoRoot, 'dist', mode);
}

// Resolve to absolute path
outputDir = resolve(outputDir);

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

  // Safety checks: never delete critical directories
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
    if (resolve(dir) === resolve(dangerous)) {
      console.error(`Error: Refusing to delete directory '${dir}' (safety check)`);
      process.exit(1);
    }
  }

  if (dir.length < 5) {
    console.error(`Error: Directory path '${dir}' looks too short to be safe`);
    process.exit(1);
  }

  if (existsSync(dir)) {
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

  // Copy top-level files and directories (except static/)
  for (const entry of readdirSync(sourceDir, { withFileTypes: true })) {
    if (entry.name === 'static') {
      continue;
    }
    cpSync(join(sourceDir, entry.name), join(targetDir, entry.name), { recursive: true });
  }

  // Merge static/ contents into target directory (flatten)
  if (existsSync(join(sourceDir, 'static'))) {
    for (const entry of readdirSync(join(sourceDir, 'static'), { withFileTypes: true })) {
      cpSync(join(sourceDir, 'static', entry.name), join(targetDir, entry.name), { recursive: true });
    }
  }

  console.log('  ✓ Build assets copied');
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

  // Remove stale version.json that CRA may have copied from public/wasm/.
  // The authoritative version.json is generated at the dist root by generateVersionJson().
  const staleVersionJson = join(targetWasmDir, 'version.json');
  if (existsSync(staleVersionJson)) {
    rmSync(staleVersionJson);
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
  const version = tag || `dev-${commit}`;

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

function main() {
  console.log(`🏗️  Building ${mode}-mode WebUI distribution...`);
  console.log('');

  // Clean output directory
  cleanOutputDirectory(outputDir);
  console.log('');

  // Install dependencies (always run npm ci for reproducible dist builds)
  console.log('📦 Installing dependencies...');
  run('npm', ['ci'], webuiDir);
  console.log('');

  // Set build environment variables
  const buildEnv = {
    DISABLE_ESLINT_PLUGIN: 'true',
  };

  if (mode === 'cloud') {
    buildEnv.REACT_APP_SPROUT_MODE = 'cloud';
    console.log('🔨 Building React app in cloud mode (REACT_APP_SPROUT_MODE=cloud)...');
  } else {
    console.log('🔨 Building React app in local mode (no REACT_APP_SPROUT_MODE)...');
  }

  // Build React app
  run('npm', ['run', 'build'], webuiDir, buildEnv);
  console.log('');

  // Copy build output
  copyBuildOutput(buildDir, outputDir);
  console.log('');

  // Copy WASM files
  copyWasmFiles(outputDir);
  console.log('');

  // Generate version.json
  generateVersionJson(outputDir, mode);
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
  console.log('  css/            - Stylesheets');
  console.log('  js/             - JavaScript bundles');
  console.log('  wasm/           - WASM binary and runtime (if available)');
  console.log('  version.json    - Version and build metadata');
  console.log('');
}

try {
  main();
} catch (err) {
  console.error('Build failed:', err);
  process.exit(1);
}
