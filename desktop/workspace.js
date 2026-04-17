/**
 * Git workspace and worktree utilities; native dialog helpers.
 */

const { dialog, BrowserWindow } = require('electron');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

function getGitResolutionPath(candidatePath) {
  if (!candidatePath) {
    return null;
  }

  try {
    const stat = fs.statSync(candidatePath);
    return stat.isDirectory() ? candidatePath : path.dirname(candidatePath);
  } catch {
    return fs.existsSync(path.dirname(candidatePath)) ? path.dirname(candidatePath) : null;
  }
}

function resolveWorkspaceDirectory(candidatePath) {
  return getGitResolutionPath(candidatePath);
}

function validateGitWorktree(worktreePath) {
  const gitPath = getGitResolutionPath(worktreePath);
  if (!gitPath) {
    return { ok: false, error: 'Selected path does not exist.' };
  }

  const gitCheck = spawnSync('git', ['rev-parse', '--is-inside-work-tree'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (gitCheck.status !== 0 || gitCheck.stdout.trim() !== 'true') {
    return { ok: false, error: 'Selected folder is not inside a Git worktree.' };
  }

  const rootCheck = spawnSync('git', ['rev-parse', '--show-toplevel'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (rootCheck.status !== 0) {
    return { ok: false, error: 'Failed to resolve Git worktree root.' };
  }

  return { ok: true, root: rootCheck.stdout.trim() };
}

function resolveGitRoot(candidatePath) {
  const gitPath = getGitResolutionPath(candidatePath);
  if (!gitPath) {
    return { ok: false, error: 'Selected path does not exist.' };
  }

  const rootCheck = spawnSync('git', ['rev-parse', '--show-toplevel'], {
    cwd: gitPath,
    encoding: 'utf8',
  });

  if (rootCheck.status !== 0) {
    return { ok: false, error: 'Selected folder is not inside a Git repository.' };
  }

  return { ok: true, root: rootCheck.stdout.trim() };
}

async function promptForWorkspace(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Open Folder',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose the working directory for this Ledit window.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  return selection.filePaths[0];
}

async function promptForRepository(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Choose Git Repository',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose a Git repository or an existing worktree.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  const candidate = selection.filePaths[0];
  const resolution = resolveGitRoot(candidate);
  if (!resolution.ok) {
    await dialog.showMessageBox(browserWindow ?? null, {
      type: 'error',
      title: 'Invalid Repository',
      message: resolution.error,
      detail: 'Choose a folder that is already part of a Git repository.',
    });
    return null;
  }

  return resolution.root;
}

async function promptForWorktreeParent(browserWindow) {
  const selection = await dialog.showOpenDialog(browserWindow ?? null, {
    title: 'Choose Worktree Parent Folder',
    properties: ['openDirectory', 'createDirectory'],
    message: 'Choose the parent directory for the new worktree.',
  });

  if (selection.canceled || selection.filePaths.length === 0) {
    return null;
  }

  return selection.filePaths[0];
}

function createWorktree(options = {}) {
  const { repositoryPath, worktreePath, branchName, baseRef } = options;

  if (!repositoryPath || !repositoryPath.trim()) {
    throw new Error('A repository path is required.');
  }

  const resolvedRepository = resolveGitRoot(repositoryPath);
  if (!resolvedRepository.ok) {
    throw new Error(resolvedRepository.error);
  }

  if (!branchName || !branchName.trim()) {
    throw new Error('A branch name is required.');
  }

  if (!worktreePath || !worktreePath.trim()) {
    throw new Error('A worktree path is required.');
  }

  const targetPath = path.resolve(worktreePath.trim());
  const targetParent = path.dirname(targetPath);
  fs.mkdirSync(targetParent, { recursive: true });

  if (fs.existsSync(targetPath)) {
    const stat = fs.statSync(targetPath);
    if (!stat.isDirectory()) {
      throw new Error('The target worktree path already exists and is not a directory.');
    }
    const existingEntries = fs.readdirSync(targetPath);
    if (existingEntries.length > 0) {
      throw new Error('The target worktree directory already exists and is not empty.');
    }
  }

  const args = ['worktree', 'add', '-b', branchName.trim(), targetPath];
  if (baseRef && baseRef.trim()) {
    args.push(baseRef.trim());
  }

  const result = spawnSync('git', args, {
    cwd: resolvedRepository.root,
    encoding: 'utf8',
  });

  if (result.status !== 0) {
    const detail = result.stderr?.trim() || result.stdout?.trim() || 'git worktree add failed.';
    throw new Error(detail);
  }

  const validation = validateGitWorktree(targetPath);
  if (!validation.ok) {
    throw new Error(validation.error);
  }

  return validation.root;
}

module.exports = {
  getGitResolutionPath,
  resolveWorkspaceDirectory,
  validateGitWorktree,
  resolveGitRoot,
  promptForWorkspace,
  promptForRepository,
  promptForWorktreeParent,
  createWorktree,
};
