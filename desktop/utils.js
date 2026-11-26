/**
 * Pure utility helpers with no side-effects and no Electron dependencies.
 */

function shellEscape(value) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

function normalizeProjectFolderEntry(entry) {
  if (!entry) {
    return null;
  }

  if (typeof entry === 'string') {
    return {
      projectFolderPath: entry,
      backendMode: 'native',
      wslDistro: null,
    };
  }

  // Entries saved before the rename carry the path as workspacePath.
  const projectFolderPath = entry.projectFolderPath ?? entry.workspacePath;
  if (typeof projectFolderPath !== 'string' || !projectFolderPath.trim()) {
    return null;
  }

  return {
    projectFolderPath,
    backendMode: entry.backendMode === 'wsl' ? 'wsl' : 'native',
    wslDistro: typeof entry.wslDistro === 'string' && entry.wslDistro.trim() ? entry.wslDistro.trim() : null,
  };
}

function getProjectFolderKey(entry) {
  const normalized = normalizeProjectFolderEntry(entry);
  if (!normalized) {
    return '';
  }

  return JSON.stringify({
    projectFolderPath: normalized.projectFolderPath,
    backendMode: normalized.backendMode,
    wslDistro: normalized.wslDistro || '',
  });
}

module.exports = { shellEscape, normalizeProjectFolderEntry, getProjectFolderKey };
