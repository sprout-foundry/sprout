const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..');

function run(command, args, options = {}) {
  const executable = process.platform === 'win32' && command === 'npm' ? 'npm.cmd' : command;
  console.log(`[beforePack] running: ${executable} ${args.join(' ')}`);
  const result = spawnSync(executable, args, {
    cwd: repoRoot,
    stdio: 'inherit',
    env: {
      ...process.env,
      ...options.env,
    },
  });

  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed with exit code ${result.status ?? 1}`);
  }
}

function mapElectronArch(arch) {
  switch (arch) {
    case 3:
    case 'arm64':
      return 'arm64';
    case 1:
    case 'x64':
      return 'amd64';
    default:
      throw new Error(`Unsupported Electron arch: ${String(arch)}`);
  }
}

function validateMacIcon() {
  const icnsPath = path.join(repoRoot, 'desktop', 'resources', 'icon.icns');
  if (!fs.existsSync(icnsPath)) {
    return;
  }

  const header = fs.readFileSync(icnsPath).subarray(0, 4).toString('ascii');
  if (header !== 'icns') {
    throw new Error(`desktop/resources/icon.icns exists but is not a valid ICNS file (magic=${JSON.stringify(header)}). Remove it or replace it with a real .icns asset.`);
  }
}

function generateMacIcon() {
  const iconsetPath = path.join(repoRoot, 'desktop', 'resources', 'icon.iconset');
  const icnsPath = path.join(repoRoot, 'desktop', 'resources', 'icon.icns');

  if (!fs.existsSync(iconsetPath)) {
    console.log('[beforePack] icon.iconset not found, skipping icon generation');
    return;
  }

  console.log('[beforePack] generating icon.icns from icon.iconset');
  const result = spawnSync('iconutil', ['-c', 'icns', iconsetPath, '-o', icnsPath], {
    cwd: repoRoot,
    stdio: 'inherit',
  });

  if (result.status !== 0) {
    console.warn(`[beforePack] iconutil failed with exit code ${result.status ?? 1}, continuing anyway`);
    return;
  }

  console.log('[beforePack] icon.icns generated successfully');
}

module.exports = async function beforePack(context) {
  const platform = context.electronPlatformName;
  const arch = mapElectronArch(context.arch);
  const extraTargets = [
    'linux-amd64',
    'linux-arm64',
    'darwin-amd64',
    'darwin-arm64',
  ].filter((target) => target !== `${platform}-${arch}`);

  // Generate icon.icns from iconset on macOS (must run before validateMacIcon)
  if (platform === 'darwin') {
    generateMacIcon();
  }

  validateMacIcon();
  run('node', ['scripts/build-webui-embed.mjs']);

  if (!process.env.SKIP_BACKEND_BUILD) {
    run('node', ['scripts/build-electron-backend.mjs'], {
      env: {
        LEDIT_GOOS: platform,
        LEDIT_GOARCH: arch,
        LEDIT_EXTRA_TARGETS: extraTargets.join(','),
      },
    });
  } else {
    console.log('[beforePack] SKIP_BACKEND_BUILD is set; skipping backend compilation');
  }
};
