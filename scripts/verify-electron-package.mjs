#!/usr/bin/env node

import { existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

function fail(message) {
  console.error(`verify-electron-package: ${message}`);
  process.exit(1);
}

function findSingleUnpackedDir(releasesDir) {
  const entries = readdirSync(releasesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name.endsWith('-unpacked'))
    .map((entry) => entry.name);

  if (entries.length === 0) {
    fail(`No unpacked app directory found under ${releasesDir}`);
  }

  if (entries.length > 1) {
    fail(`Multiple unpacked app directories found under ${releasesDir}: ${entries.join(', ')}`);
  }

  return join(releasesDir, entries[0]);
}

function ensureFile(filePath, description) {
  if (!existsSync(filePath)) {
    fail(`Missing ${description}: ${filePath}`);
  }
  const stat = statSync(filePath);
  if (!stat.isFile()) {
    fail(`Expected file for ${description}: ${filePath}`);
  }
}

function ensureDirectory(dirPath, description) {
  if (!existsSync(dirPath)) {
    fail(`Missing ${description}: ${dirPath}`);
  }
  const stat = statSync(dirPath);
  if (!stat.isDirectory()) {
    fail(`Expected directory for ${description}: ${dirPath}`);
  }
}

const releasesDir = process.argv[2] ? join(process.cwd(), process.argv[2]) : join(process.cwd(), 'desktop', 'dist', 'releases');
ensureDirectory(releasesDir, 'release output directory');

const unpackedDir = process.argv[3] ? join(releasesDir, process.argv[3]) : findSingleUnpackedDir(releasesDir);
ensureDirectory(unpackedDir, 'unpacked app directory');

const resourcesDir = join(unpackedDir, 'resources');
const backendRoot = join(resourcesDir, 'backend');

ensureFile(join(resourcesDir, 'app.asar'), 'app bundle');
ensureDirectory(backendRoot, 'bundled backend directory');

const backendPlatforms = readdirSync(backendRoot, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name);

if (backendPlatforms.length === 0) {
  fail(`No backend platform directories found under ${backendRoot}`);
}

const requiredTargets = [
  'linux-amd64',
  'linux-arm64',
  'darwin-amd64',
  'darwin-arm64',
];

for (const target of requiredTargets) {
  if (!backendPlatforms.includes(target)) {
    fail(`Missing bundled remote backend target ${target} under ${backendRoot}`);
  }
}

const backendExecutables = [];
for (const platformDir of backendPlatforms) {
  const candidateDir = join(backendRoot, platformDir);
  const entries = readdirSync(candidateDir, { withFileTypes: true }).filter((entry) => entry.isFile());
  backendExecutables.push(...entries.map((entry) => join(candidateDir, entry.name)));
}

if (backendExecutables.length === 0) {
  fail(`No bundled backend executable found under ${backendRoot}`);
}

console.log('verify-electron-package: ok');
console.log(`unpackedDir=${unpackedDir}`);
console.log(`backendExecutables=${backendExecutables.join(',')}`);
