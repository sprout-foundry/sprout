/**
 * Pure utility helpers with no side-effects and no Electron dependencies.
 */

function shellEscape(value) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

function normalizeWorkspaceEntry(entry) {
  if (!entry) {
    return null;
  }

  if (typeof entry === 'string') {
    return {
      workspacePath: entry,
      backendMode: 'native',
      wslDistro: null,
    };
  }

  if (typeof entry.workspacePath !== 'string' || !entry.workspacePath.trim()) {
    return null;
  }

  return {
    workspacePath: entry.workspacePath,
    backendMode: entry.backendMode === 'wsl' ? 'wsl' : 'native',
    wslDistro: typeof entry.wslDistro === 'string' && entry.wslDistro.trim() ? entry.wslDistro.trim() : null,
  };
}

function getWorkspaceKey(entry) {
  const normalized = normalizeWorkspaceEntry(entry);
  if (!normalized) {
    return '';
  }

  return JSON.stringify({
    workspacePath: normalized.workspacePath,
    backendMode: normalized.backendMode,
    wslDistro: normalized.wslDistro || '',
  });
}

module.exports = { shellEscape, normalizeWorkspaceEntry, getWorkspaceKey };
