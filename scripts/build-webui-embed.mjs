#!/usr/bin/env node

import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const webuiDir = join(repoRoot, 'webui');
const targetDir = join(repoRoot, 'pkg', 'webui', 'static');
const buildDir = join(webuiDir, 'dist'); // Vite output directory
const embeddedLogoPath = join(repoRoot, 'pkg', 'webui', 'logo-mark.svg');
const targetLogoPath = join(targetDir, 'logo-mark.svg');

function run(command, args, cwd, extraEnv = {}) {
  const executable = process.platform === 'win32' && command === 'npm' ? 'npm.cmd' : command;
  console.log(`↪ ${executable} ${args.join(' ')} (cwd: ${cwd})`);
  const result = spawnSync(executable, args, {
    cwd,
    stdio: 'inherit',
    env: { ...process.env, ...extraEnv },
    shell: process.platform === 'win32',
  });
  if (result.error) {
    console.error(`Command failed to start: ${executable} ${args.join(' ')}`);
    console.error(result.error);
    process.exit(1);
  }
  if (result.signal) {
    console.error(`Command terminated by signal ${result.signal}: ${executable} ${args.join(' ')}`);
    process.exit(1);
  }
  if (result.status !== 0) {
    console.error(`Command failed with exit code ${result.status ?? 1}: ${executable} ${args.join(' ')}`);
    process.exit(result.status ?? 1);
  }
}

function copyBuildOutput() {
  mkdirSync(targetDir, { recursive: true });

  // Preserve the placeholder file — it's tracked in git to satisfy
  // //go:embed static when the directory is otherwise empty on a fresh clone.
  const placeholderPath = join(targetDir, "placeholder");
  let hasPlaceholder = false;
  try {
    hasPlaceholder = existsSync(placeholderPath);
  } catch { /* ignore */ }

  for (const entry of readdirSync(targetDir)) {
    // Skip the placeholder so it survives the build
    if (hasPlaceholder && entry === "placeholder") continue;
    rmSync(join(targetDir, entry), { recursive: true, force: true });
  }

  // Copy all files from dist/ to targetDir
  for (const entry of readdirSync(buildDir, { withFileTypes: true })) {
    cpSync(join(buildDir, entry.name), join(targetDir, entry.name), { recursive: true });
  }

  if (existsSync(embeddedLogoPath) && !existsSync(targetLogoPath)) {
    cpSync(embeddedLogoPath, targetLogoPath);
  }
}

console.log('🏗️  Building React Web UI with Vite...');

// Check for --no-build flag
const noBuild = process.argv.includes('--no-build');

if (!noBuild) {
  // Install all workspaces from root if node_modules is missing.
  // Per-workspace `npm install` was previously called here; it pruned
  // root-hoisted devDeps (notably electron-builder) and broke later
  // `desktop:dist`. Always install from root with workspaces.
  if (!existsSync(join(repoRoot, 'node_modules'))) {
    console.log('📦 Installing root workspaces...');
    run('npm', ['ci'], repoRoot);
  }

  // Build workspace packages that the webui depends on via file: links.
  // Their `prepare` script was removed (61ba3f17) and `npm ci` does not
  // run `prepare` anyway, so build them explicitly to ensure `dist/`
  // exists before Vite resolves the `exports` fields in their package.json.
  for (const pkg of ['@sprout/events', '@sprout/ui']) {
    const distDir = join(repoRoot, 'packages', pkg.split('/')[1], 'dist');
    if (!existsSync(distDir)) {
      console.log(`📦 Building ${pkg}...`);
      run('npm', ['run', 'build', '-w', pkg], repoRoot);
    }
  }

  console.log('🔨 Building React app with Vite...');
  run('npm', ['run', 'build', '-w', 'sprout-webui'], repoRoot, {
    // Pass mode for cloud/local builds
    ...(process.env.SPROUT_MODE && { VITE_SPROUT_MODE: process.env.SPROUT_MODE }),
  });
} else {
  console.log('⏭️  Skipping React build (--no-build flag)');
}

console.log('📁 Copying build assets to Go package...');
copyBuildOutput();

console.log(`✅ React Web UI build completed: ${resolve(targetDir)}`);
