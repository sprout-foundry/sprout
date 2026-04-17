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

function mapOsArch(os, arch) {
  const goArch = ({ x64: 'amd64', arm64: 'arm64' }[arch] || arch);
  return `${os}-${goArch}`;
}

function parsePlatformFromArgs() {
  // Check for --platform flag
  const platformIndex = process.argv.indexOf('--platform');
  if (platformIndex !== -1 && platformIndex + 1 < process.argv.length) {
    return process.argv[platformIndex + 1];
  }

  // Check for PLATFORM or DESKTOP_PLATFORM environment variable
  const envPlatform = process.env.PLATFORM || process.env.DESKTOP_PLATFORM;
  if (envPlatform) {
    const envArch = process.env.DESKTOP_ARCH || (process.arch === 'x64' ? 'amd64' : process.arch);
    // If already in os-arch format (e.g., linux-amd64), return as-is
    if (/^(linux|darwin|windows)-(amd64|arm64)$/.test(envPlatform)) {
      return envPlatform;
    }
    // Otherwise combine with DESKTOP_ARCH env or detected arch
    return mapOsArch(envPlatform, envArch);
  }

  return null;
}

function detectPlatformFromDirName(unpackedDir) {
  const dirName = unpackedDir.split('/').pop() || unpackedDir.split('\\').pop();

  // Electron's default unpacked directory names include arch
  // Linux: <name>-linux-x64-unpacked or <name>-linux-arm64-unpacked
  if (dirName.includes('linux-x64-unpacked')) {
    return 'linux-amd64';
  }
  if (dirName.includes('linux-arm64-unpacked')) {
    return 'linux-arm64';
  }

  // macOS: <name>.app (assume arm64 for modern Macs, or could be detected from binary)
  if (dirName.endsWith('.app')) {
    return 'darwin-arm64'; // Default to arm64 for modern macOS
  }
  if (dirName.includes('darwin-unpacked')) {
    return 'darwin-arm64'; // Default to arm64
  }

  // Windows: <name>-win32-x64-unpacked or <name>-win32-arm64-unpacked
  if (dirName.includes('win32-x64-unpacked')) {
    return 'windows-amd64';
  }
  if (dirName.includes('win32-arm64-unpacked')) {
    return 'windows-arm64';
  }

  return null;
}

function getPlatform(unpackedDir) {
  const explicitPlatform = parsePlatformFromArgs();
  if (explicitPlatform) {
    return explicitPlatform;
  }

  const detectedPlatform = detectPlatformFromDirName(unpackedDir);
  if (detectedPlatform) {
    return detectedPlatform;
  }

  fail(`Could not detect platform. Provide --platform flag or PLATFORM environment variable.`);
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

const platform = getPlatform(unpackedDir);
console.log(`verify-electron-package: platform=${platform}`);

// Only require the native platform's backend binary
const nativeBackendDir = join(backendRoot, platform);
ensureDirectory(nativeBackendDir, `native backend directory for ${platform}`);

const backendExecutables = [];
for (const platformDir of backendPlatforms) {
  const candidateDir = join(backendRoot, platformDir);
  const entries = readdirSync(candidateDir, { withFileTypes: true }).filter((entry) => entry.isFile());
  backendExecutables.push(...entries.map((entry) => join(candidateDir, entry.name)));
}

// Verify native backend has at least one executable
const nativeExecutables = readdirSync(nativeBackendDir, { withFileTypes: true })
  .filter((entry) => entry.isFile())
  .map((entry) => join(nativeBackendDir, entry.name));

if (nativeExecutables.length === 0) {
  fail(`No native backend executable found for ${platform} under ${nativeBackendDir}`);
}

console.log('verify-electron-package: ok');
console.log(`platform=${platform}`);
console.log(`unpackedDir=${unpackedDir}`);
console.log(`nativeBackend=${nativeExecutables.join(',')}`);
console.log(`backendExecutables=${backendExecutables.join(',')}`);
