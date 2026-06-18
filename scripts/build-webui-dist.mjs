#!/usr/bin/env node

import { cpSync, existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = resolve(fileURLToPath(new URL('..', import.meta.url)));
const webuiDir = join(repoRoot, 'webui');
const buildDir = join(webuiDir, 'dist'); // Vite output directory

// Parse command line arguments
const args = process.argv.slice(2);
let mode = 'cloud'; // default
let outputDir = '';
let foundryApiUrl = undefined;
let foundryWsUrl = undefined;

for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  if (arg === '--mode' && i + 1 < args.length) {
    mode = args[i + 1];
    i++;
  } else if (arg === '--output' && i + 1 < args.length) {
    outputDir = args[i + 1];
    i++;
  } else if (arg === '--api-url' && i + 1 < args.length) {
    foundryApiUrl = args[i + 1];
    i++;
  } else if (arg === '--ws-url' && i + 1 < args.length) {
    foundryWsUrl = args[i + 1];
    i++;
  } else if (arg === '--help' || arg === '-h') {
    console.log('Usage: node build-webui-dist.mjs [options]');
    console.log('');
    console.log('Options:');
    console.log('  --mode <cloud|local>  Build mode (default: cloud)');
    console.log('  --output <dir>         Output directory (default: dist/<mode>/)');
    console.log('  --api-url <url>        Foundry API base URL (runtime-configurable)');
    console.log('  --ws-url <url>         Foundry WebSocket URL (runtime-configurable)');
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
    console.log('Examples:');
    console.log('  node build-webui-dist.mjs                 # Build cloud-mode to dist/cloud/');
    console.log('  node build-webui-dist.mjs --mode local    # Build local-mode to dist/local/');
    console.log('  node build-webui-dist.mjs --mode cloud --output ./release');
    console.log('  node build-webui-dist.mjs --api-url https://api.example.com/api --ws-url wss://api.example.com/ws');
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

function main() {
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

  if (mode === 'cloud') {
    buildEnv.VITE_SPROUT_MODE = 'cloud';
    console.log('🔨 Building React app with Vite in cloud mode (VITE_SPROUT_MODE=cloud)...');
  } else {
    // Explicitly override to prevent env var leak from the shell
    buildEnv.VITE_SPROUT_MODE = 'local';
    console.log('🔨 Building React app with Vite in local mode (VITE_SPROUT_MODE=local)...');
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
  console.log('  assets/         - Built assets (css/, js/)');
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
