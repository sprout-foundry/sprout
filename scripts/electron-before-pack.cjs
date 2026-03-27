const { spawnSync } = require('node:child_process');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..');

function run(command, args, options = {}) {
  const executable = process.platform === 'win32' && command === 'npm' ? 'npm.cmd' : command;
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

module.exports = async function beforePack(context) {
  const platform = context.electronPlatformName;
  const arch = mapElectronArch(context.arch);

  run('npm', ['run', 'build:webui:embed']);
  run('node', ['scripts/build-electron-backend.mjs'], {
    env: {
      LEDIT_GOOS: platform,
      LEDIT_GOARCH: arch,
    },
  });
};
