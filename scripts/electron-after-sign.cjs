const { spawnSync } = require('node:child_process');
const path = require('node:path');

function run(command, args) {
  const result = spawnSync(command, args, {
    stdio: 'inherit',
    env: process.env,
  });

  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed with exit code ${result.status ?? 1}`);
  }
}

module.exports = async function afterSign(context) {
  if (context.electronPlatformName !== 'darwin') {
    return;
  }

  const appleID = process.env.APPLE_ID;
  const appleTeamID = process.env.APPLE_TEAM_ID;
  const appleAppPassword = process.env.APPLE_APP_SPECIFIC_PASSWORD;

  if (!appleID || !appleTeamID || !appleAppPassword) {
    console.log('Skipping macOS notarization because APPLE_ID / APPLE_TEAM_ID / APPLE_APP_SPECIFIC_PASSWORD are not fully configured.');
    return;
  }

  const appName = `${context.packager.appInfo.productFilename}.app`;
  const appPath = path.join(context.appOutDir, appName);

  run('xcrun', [
    'notarytool',
    'submit',
    appPath,
    '--apple-id',
    appleID,
    '--team-id',
    appleTeamID,
    '--password',
    appleAppPassword,
    '--wait',
  ]);

  run('xcrun', ['stapler', 'staple', appPath]);
};
