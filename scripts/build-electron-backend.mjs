#!/usr/bin/env node

import { mkdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import process from 'node:process';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const backendOutDir = join(repoRoot, 'desktop', 'dist', 'backend');

const platform = process.env.LEDIT_GOOS || ({
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
}[process.platform] || process.platform);

const arch = process.env.LEDIT_GOARCH || ({
  arm64: 'arm64',
  x64: 'amd64',
}[process.arch] || process.arch);

const binaryName = platform === 'windows' ? 'ledit.exe' : 'ledit';
const outputDir = join(backendOutDir, `${platform}-${arch}`);
const outputPath = join(outputDir, binaryName);

rmSync(outputDir, { recursive: true, force: true });
mkdirSync(outputDir, { recursive: true });

const result = spawnSync('go', ['build', '-o', outputPath, '.'], {
  cwd: repoRoot,
  stdio: 'inherit',
  env: {
    ...process.env,
    GOOS: platform,
    GOARCH: arch,
    CGO_ENABLED: process.env.CGO_ENABLED || '0',
  },
});

if (result.status !== 0) {
  process.exit(result.status ?? 1);
}

console.log(`Built desktop backend: ${outputPath}`);
