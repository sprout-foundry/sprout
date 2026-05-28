#!/usr/bin/env node

import { existsSync, mkdirSync, rmSync } from 'node:fs';
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
  platform: process.env.SPROUT_GOOS || defaultPlatform,
  arch: process.env.SPROUT_GOARCH || defaultArch,
};

const remoteTargets = [
  { platform: 'linux', arch: 'amd64' },
  { platform: 'linux', arch: 'arm64' },
  { platform: 'darwin', arch: 'amd64' },
  { platform: 'darwin', arch: 'arm64' },
];

const extraTargets = String(process.env.SPROUT_EXTRA_TARGETS || '')
  .split(',')
  .map((item) => item.trim())
  .filter(Boolean)
  .map((item) => {
    const [platform, arch] = item.split('-');
    if (!platform || !arch) {
      throw new Error(`Invalid SPROUT_EXTRA_TARGETS entry: ${item}`);
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

// Resolve repo version info from git for ldflags.
function resolveVersionInfo() {
  const now = new Date().toISOString().replace(/\.\d+Z$/, 'Z');

  let tag = '';
  let commit = '';
  try {
    tag = String(spawnSync('git', ['describe', '--tags', '--abbrev=0'], { cwd: repoRoot, encoding: 'utf8' }).stdout || '').trim();
  } catch (_) { /* no tags */ }
  try {
    commit = String(spawnSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: repoRoot, encoding: 'utf8' }).stdout || '').trim();
  } catch (_) { /* not a git repo */ }

  return { version: tag || 'dev', commit, date: now };
}

const { version: pkgVersion, commit, date } = resolveVersionInfo();
const ldflags = [
  `-X 'github.com/sprout-foundry/sprout/cmd.version=${pkgVersion}'`,
  `-X 'github.com/sprout-foundry/sprout/cmd.gitCommit=${commit}'`,
  `-X 'github.com/sprout-foundry/sprout/cmd.buildDate=${date}'`,
  `-X 'github.com/sprout-foundry/sprout/cmd.gitTag=${pkgVersion}'`,
].join(' ');

// pkg/ast/grammars_embed.go unconditionally //go:embed's five tree-sitter
// grammar blobs that are gitignored and copied from the gotreesitter module
// cache by scripts/prepare-grammars.sh. A fresh checkout (e.g. CI) doesn't
// have them, so `go build` fails on the embed directive. Ensure they exist
// before building. Only shells out when missing, so a dev tree that already
// ran `make build-all` (and machines without bash) are unaffected.
const grammarBinDir = join(repoRoot, 'pkg', 'ast', 'grammars', 'bin');
const requiredBlobs = ['go.bin', 'typescript.bin', 'tsx.bin', 'javascript.bin', 'python.bin'];
if (!requiredBlobs.every((b) => existsSync(join(grammarBinDir, b)))) {
  const prep = spawnSync('bash', [join(repoRoot, 'scripts', 'prepare-grammars.sh')], {
    cwd: repoRoot,
    stdio: 'inherit',
  });
  if (prep.status !== 0) {
    console.error('Failed to prepare tree-sitter grammar blobs (scripts/prepare-grammars.sh).');
    process.exit(prep.status ?? 1);
  }
}

for (const target of targets) {
  const binaryName = target.platform === 'windows' ? 'sprout.exe' : 'sprout';
  const outputDir = join(backendOutDir, `${target.platform}-${target.arch}`);
  const outputPath = join(outputDir, binaryName);

  rmSync(outputDir, { recursive: true, force: true });
  mkdirSync(outputDir, { recursive: true });

  const result = spawnSync('go', ['build', '-tags', 'grammar_blobs_external', `-ldflags=${ldflags}`, '-o', outputPath, '.'], {
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
