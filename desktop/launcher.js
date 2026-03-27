const recentContainer = document.getElementById('recent-worktrees');
const openWorktreeButton = document.getElementById('open-worktree');
const createWorktreeButton = document.getElementById('create-worktree');
const openWorktreeNewButton = document.getElementById('open-worktree-new');
const wslPanel = document.getElementById('wsl-launcher-panel');
const wslDistroSelect = document.getElementById('wsl-distro-select');
const wslWorkspacePathInput = document.getElementById('wsl-workspace-path');
const openWslWorkspaceButton = document.getElementById('open-wsl-workspace');
const openWslWorkspaceNewButton = document.getElementById('open-wsl-workspace-new');
const wslLauncherErrorNode = document.getElementById('wsl-launcher-error');
const versionNode = document.getElementById('app-version');
const createModal = document.getElementById('create-worktree-modal');
const createForm = document.getElementById('create-worktree-form');
const createErrorNode = document.getElementById('create-worktree-error');
const repositoryPathInput = document.getElementById('create-repository-path');
const branchNameInput = document.getElementById('create-branch-name');
const baseRefInput = document.getElementById('create-base-ref');
const worktreePathInput = document.getElementById('create-worktree-path');
const browseRepositoryButton = document.getElementById('browse-repository');
const browseWorktreeParentButton = document.getElementById('browse-worktree-parent');
const submitCreateWorktreeButton = document.getElementById('submit-create-worktree');
const closeCreateWorktreeButton = document.getElementById('close-create-worktree');
const cancelCreateWorktreeButton = document.getElementById('cancel-create-worktree');

function normalizePathForDisplay(value) {
  return value.replace(/\\/g, '/');
}

function joinPath(parentPath, childName) {
  const trimmedParent = parentPath.replace(/[\\/]+$/, '');
  if (!trimmedParent) {
    return childName;
  }
  const separator = trimmedParent.includes('\\') ? '\\' : '/';
  return `${trimmedParent}${separator}${childName}`;
}

function sanitizeBranchForPath(branchName) {
  return branchName.replace(/[<>:"|?*\s]+/g, '-').replace(/[\\/]+/g, '-');
}

function inferWorktreePath() {
  const repositoryPath = repositoryPathInput.value.trim();
  const branchName = branchNameInput.value.trim();
  if (!repositoryPath || !branchName) {
    return;
  }

  const normalizedRepository = normalizePathForDisplay(repositoryPath);
  const lastSeparatorIndex = normalizedRepository.lastIndexOf('/');
  const parentPath = lastSeparatorIndex >= 0
    ? normalizedRepository.slice(0, lastSeparatorIndex)
    : normalizedRepository;
  worktreePathInput.value = joinPath(parentPath, sanitizeBranchForPath(branchName));
  delete worktreePathInput.dataset.manual;
}

function showCreateError(message) {
  createErrorNode.textContent = message;
  createErrorNode.classList.remove('hidden');
}

function clearCreateError() {
  createErrorNode.textContent = '';
  createErrorNode.classList.add('hidden');
}

function showWslError(message) {
  wslLauncherErrorNode.textContent = message;
  wslLauncherErrorNode.classList.remove('hidden');
}

function clearWslError() {
  wslLauncherErrorNode.textContent = '';
  wslLauncherErrorNode.classList.add('hidden');
}

function openCreateModal() {
  clearCreateError();
  createModal.classList.remove('hidden');
  createModal.setAttribute('aria-hidden', 'false');
  if (!baseRefInput.value.trim()) {
    baseRefInput.value = 'HEAD';
  }
  if (!repositoryPathInput.value.trim()) {
    repositoryPathInput.focus();
    return;
  }
  branchNameInput.focus();
}

function closeCreateModal() {
  clearCreateError();
  createModal.classList.add('hidden');
  createModal.setAttribute('aria-hidden', 'true');
}

async function refreshRecentWorktrees() {
  const entries = await window.leditDesktop.listRecentWorktrees();
  renderRecentWorktrees(entries);
}

function renderRecentWorktrees(entries) {
  if (!entries.length) {
    recentContainer.innerHTML = `
      <div class="recent-worktree-empty">
        No recent folders yet. Open any folder here, or create a git worktree if you want branch-isolated workspaces.
      </div>
    `;
    return;
  }

  recentContainer.innerHTML = '';
  for (const entry of entries) {
    const row = document.createElement('div');
    row.className = 'recent-worktree';
    const tags = [];
    if (entry.backendMode === 'wsl') {
      tags.push('<span class="recent-worktree-tag">WSL</span>');
      if (entry.wslDistro) {
        tags.push(`<span class="recent-worktree-tag">${entry.wslDistro}</span>`);
      }
    }
    row.innerHTML = `
      <div class="recent-worktree-meta">
        <div class="recent-worktree-name">${entry.name}<span class="recent-worktree-tags">${tags.join('')}</span></div>
        <div class="recent-worktree-path">${entry.path}</div>
      </div>
      <div class="recent-worktree-actions">
        <button class="recent-worktree-btn" data-open="${entry.path}">Open</button>
        <button class="recent-worktree-btn" data-open-new="${entry.path}">New Window</button>
      </div>
    `;
    recentContainer.appendChild(row);
  }

  recentContainer.querySelectorAll('[data-open]').forEach((button) => {
    button.addEventListener('click', async () => {
      await window.leditDesktop.openWorkspace({
        workspacePath: button.getAttribute('data-open'),
        forceNewWindow: false,
        backendMode: entry.backendMode,
        wslDistro: entry.wslDistro,
      });
    });
  });

  recentContainer.querySelectorAll('[data-open-new]').forEach((button) => {
    button.addEventListener('click', async () => {
      await window.leditDesktop.openWorkspace({
        workspacePath: button.getAttribute('data-open-new'),
        forceNewWindow: true,
        backendMode: entry.backendMode,
        wslDistro: entry.wslDistro,
      });
    });
  });
}

async function openViaPicker(forceNewWindow) {
  const workspacePath = await window.leditDesktop.pickWorkspace();
  if (!workspacePath) {
    return;
  }
  await window.leditDesktop.openWorkspace({ workspacePath, forceNewWindow });
}

async function setupWslLauncher() {
  if (window.leditDesktop.platform !== 'win32') {
    return;
  }

  const distros = await window.leditDesktop.listWslDistros();
  if (!distros.length) {
    return;
  }

  wslDistroSelect.innerHTML = distros
    .map((distro) => `<option value="${distro}">${distro}</option>`)
    .join('');
  wslPanel.classList.remove('hidden');
}

async function openWslWorkspace(forceNewWindow) {
  clearWslError();
  const workspacePath = wslWorkspacePathInput.value.trim();
  const wslDistro = wslDistroSelect.value.trim();

  if (!workspacePath) {
    showWslError('A WSL workspace path is required.');
    return;
  }
  if (!wslDistro) {
    showWslError('Choose a WSL distro first.');
    return;
  }

  try {
    await window.leditDesktop.openWorkspace({
      workspacePath,
      forceNewWindow,
      backendMode: 'wsl',
      wslDistro,
    });
  } catch (error) {
    showWslError(error?.message || String(error));
  }
}

async function browseRepository() {
  const repositoryPath = await window.leditDesktop.pickRepository();
  if (!repositoryPath) {
    return;
  }
  repositoryPathInput.value = repositoryPath;
  if (!worktreePathInput.value.trim() || worktreePathInput.dataset.manual !== 'true') {
    inferWorktreePath();
  }
}

async function browseWorktreeParent() {
  const parentPath = await window.leditDesktop.pickWorktreeParent();
  if (!parentPath) {
    return;
  }

  const branchName = branchNameInput.value.trim();
  worktreePathInput.value = branchName
    ? joinPath(parentPath, sanitizeBranchForPath(branchName))
    : parentPath;
  delete worktreePathInput.dataset.manual;
}

openWorktreeButton.addEventListener('click', () => openViaPicker(false));
createWorktreeButton.addEventListener('click', () => openCreateModal());
openWorktreeNewButton.addEventListener('click', () => openViaPicker(true));
openWslWorkspaceButton.addEventListener('click', () => openWslWorkspace(false));
openWslWorkspaceNewButton.addEventListener('click', () => openWslWorkspace(true));
browseRepositoryButton.addEventListener('click', () => browseRepository());
browseWorktreeParentButton.addEventListener('click', () => browseWorktreeParent());
closeCreateWorktreeButton.addEventListener('click', () => closeCreateModal());
cancelCreateWorktreeButton.addEventListener('click', () => closeCreateModal());

repositoryPathInput.addEventListener('change', () => {
  if (!worktreePathInput.value.trim() || worktreePathInput.dataset.manual !== 'true') {
    inferWorktreePath();
  }
});

branchNameInput.addEventListener('input', () => {
  if (!worktreePathInput.value.trim() || worktreePathInput.dataset.manual !== 'true') {
    inferWorktreePath();
  }
});

worktreePathInput.addEventListener('input', () => {
  worktreePathInput.dataset.manual = 'true';
});

createModal.addEventListener('click', (event) => {
  if (event.target instanceof HTMLElement && event.target.dataset.closeModal === 'true') {
    closeCreateModal();
  }
});

document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && !createModal.classList.contains('hidden')) {
    closeCreateModal();
  }
});

createForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  clearCreateError();

  const repositoryPath = repositoryPathInput.value.trim();
  const branchName = branchNameInput.value.trim();
  const worktreePath = worktreePathInput.value.trim();
  const baseRef = baseRefInput.value.trim() || 'HEAD';

  if (!repositoryPath || !branchName || !worktreePath) {
    showCreateError('Repository, branch name, and worktree path are required.');
    return;
  }

  submitCreateWorktreeButton.disabled = true;
  try {
    await window.leditDesktop.createWorktree({
      repositoryPath,
      branchName,
      worktreePath,
      baseRef,
    });
    closeCreateModal();
    await refreshRecentWorktrees();
  } catch (error) {
    showCreateError(error?.message || String(error));
  } finally {
    submitCreateWorktreeButton.disabled = false;
  }
});

Promise.all([
  refreshRecentWorktrees(),
  setupWslLauncher(),
  window.leditDesktop.appVersion(),
]).then(([, version]) => {
  versionNode.textContent = `v${version}`;
});
