#!/usr/bin/env node

import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const webuiDir = join(repoRoot, 'webui');
const targetDir = join(repoRoot, 'pkg', 'webui', 'static');
const buildDir = join(webuiDir, 'build');
const buildStaticDir = join(buildDir, 'static');
const embeddedLogoPath = join(repoRoot, 'pkg', 'webui', 'logo-mark.svg');
const targetLogoPath = join(targetDir, 'logo-mark.svg');

function run(command, args, cwd) {
  const executable = process.platform === 'win32' && command === 'npm' ? 'npm.cmd' : command;
  console.log(`↪ ${executable} ${args.join(' ')} (cwd: ${cwd})`);
  const result = spawnSync(executable, args, {
    cwd,
    stdio: 'inherit',
    env: process.env,
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

  for (const entry of readdirSync(targetDir)) {
    rmSync(join(targetDir, entry), { recursive: true, force: true });
  }

  for (const entry of readdirSync(buildDir, { withFileTypes: true })) {
    if (entry.name === 'static') {
      continue;
    }
    cpSync(join(buildDir, entry.name), join(targetDir, entry.name), { recursive: true });
  }

  if (existsSync(buildStaticDir)) {
    for (const entry of readdirSync(buildStaticDir, { withFileTypes: true })) {
      cpSync(join(buildStaticDir, entry.name), join(targetDir, entry.name), { recursive: true });
    }
  }

  if (existsSync(embeddedLogoPath) && !existsSync(targetLogoPath)) {
    cpSync(embeddedLogoPath, targetLogoPath);
  }
}

console.log('🏗️  Building React Web UI...');

if (!existsSync(join(webuiDir, 'node_modules'))) {
  console.log('📦 Installing dependencies...');
  run('npm', ['install'], webuiDir);
}

console.log('🔨 Building React app...');
run('npm', ['run', 'build'], webuiDir);

console.log('📁 Copying build assets to Go package...');
copyBuildOutput();

console.log(`✅ React Web UI build completed: ${resolve(targetDir)}`);
