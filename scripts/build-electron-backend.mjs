#!/usr/bin/env node

import { mkdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import process from 'node:process';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));
const backendOutDir = join(repoRoot, 'desktop', 'dist', 'backend');

const defaultPlatform = ({
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
}[process.platform] || process.platform);

const defaultArch = ({
  arm64: 'arm64',
  x64: 'amd64',
}[process.arch] || process.arch);

const primaryTarget = {
  platform: process.env.LEDIT_GOOS || defaultPlatform,
  arch: process.env.LEDIT_GOARCH || defaultArch,
};

const remoteTargets = [
  { platform: 'linux', arch: 'amd64' },
  { platform: 'linux', arch: 'arm64' },
  { platform: 'darwin', arch: 'amd64' },
  { platform: 'darwin', arch: 'arm64' },
];

const extraTargets = String(process.env.LEDIT_EXTRA_TARGETS || '')
  .split(',')
  .map((item) => item.trim())
  .filter(Boolean)
  .map((item) => {
    const [platform, arch] = item.split('-');
    if (!platform || !arch) {
      throw new Error(`Invalid LEDIT_EXTRA_TARGETS entry: ${item}`);
    }
    return { platform, arch };
  });

if (extraTargets.length === 0) {
  extraTargets.push(
    ...remoteTargets.filter((target) =>
      !(target.platform === primaryTarget.platform && target.arch === primaryTarget.arch)
    )
  );
}

const targets = [primaryTarget, ...extraTargets].filter((target, index, array) =>
  array.findIndex((candidate) => candidate.platform === target.platform && candidate.arch === target.arch) === index
);

for (const target of targets) {
  const binaryName = target.platform === 'windows' ? 'ledit.exe' : 'ledit';
  const outputDir = join(backendOutDir, `${target.platform}-${target.arch}`);
  const outputPath = join(outputDir, binaryName);

  rmSync(outputDir, { recursive: true, force: true });
  mkdirSync(outputDir, { recursive: true });

  const result = spawnSync('go', ['build', '-o', outputPath, '.'], {
    cwd: repoRoot,
    stdio: 'inherit',
    env: {
      ...process.env,
      GOOS: target.platform,
      GOARCH: target.arch,
      CGO_ENABLED: process.env.CGO_ENABLED || '0',
    },
  });

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }

  console.log(`Built desktop backend: ${outputPath}`);
}
