/**
 * Desktop persistent state: recent workspaces, window bounds, backend log streams.
 */

const { app } = require('electron');
const { createWriteStream } = require('node:fs');
const fs = require('node:fs');
const path = require('node:path');
const { normalizeWorkspaceEntry, getWorkspaceKey } = require('./utils');
const ctx = require('./context');

function getUserStatePath() {
  return path.join(app.getPath('userData'), 'desktop-state.json');
}

function getLogDirectory() {
  const dir = path.join(app.getPath('userData'), 'logs');
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

function openBackendLogStream(label) {
  const dir = getLogDirectory();
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  const logPath = path.join(dir, `backend-${label}-${timestamp}.log`);
  const stream = createWriteStream(logPath, { flags: 'a' });
  stream.on('error', () => {});
  console.log(`[desktop] backend log: ${logPath}`);
  return stream;
}

function readDesktopState() {
  try {
    const state = JSON.parse(fs.readFileSync(getUserStatePath(), 'utf8'));
    return {
      recentWorktrees: Array.isArray(state?.recentWorktrees) ? state.recentWorktrees : [],
      openWorktrees: Array.isArray(state?.openWorktrees) ? state.openWorktrees : [],
      windowBoundsByWorktree: state?.windowBoundsByWorktree || {},
    };
  } catch {
    return { recentWorktrees: [], openWorktrees: [], windowBoundsByWorktree: {} };
  }
}

function writeDesktopState(state) {
  fs.mkdirSync(path.dirname(getUserStatePath()), { recursive: true });
  fs.writeFileSync(getUserStatePath(), JSON.stringify(state, null, 2));
}

function addRecentWorktree(worktreePath) {
  const normalizedEntry = normalizeWorkspaceEntry(worktreePath);
  if (!normalizedEntry) {
    return;
  }

  const state = readDesktopState();
  const normalizedKey = getWorkspaceKey(normalizedEntry);
  const recent = [
    normalizedEntry,
    ...(state.recentWorktrees || [])
      .map((item) => normalizeWorkspaceEntry(item))
      .filter((item) => item && getWorkspaceKey(item) !== normalizedKey),
  ].slice(0, 10);
  writeDesktopState({ ...state, recentWorktrees: recent });
}

function persistOpenWorkspaces() {
  const state = readDesktopState();
  const openWorkspaces = Array.from(ctx.instanceRegistry.values())
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
  writeDesktopState({
    ...state,
    openWorktrees: openWorkspaces,
  });
}

function getRecentWorktrees() {
  return (readDesktopState().recentWorktrees || [])
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
}

function getRecentWorktreeEntries() {
  return getRecentWorktrees().map((entry) => ({
    path: entry.workspacePath,
    name: path.basename(entry.workspacePath),
    backendMode: entry.backendMode,
    wslDistro: entry.wslDistro,
  }));
}

function getRestorableWorktrees() {
  return (readDesktopState().openWorktrees || [])
    .map((entry) => normalizeWorkspaceEntry(entry))
    .filter(Boolean);
}

function sanitizeWindowBounds(bounds) {
  if (!bounds || typeof bounds !== 'object') {
    return null;
  }

  const width = Number(bounds.width);
  const height = Number(bounds.height);
  if (!Number.isFinite(width) || !Number.isFinite(height)) {
    return null;
  }

  const sanitized = {
    width: Math.max(1100, Math.round(width)),
    height: Math.max(700, Math.round(height)),
    isMaximized: Boolean(bounds.isMaximized),
  };

  const x = Number(bounds.x);
  const y = Number(bounds.y);
  if (Number.isFinite(x) && Number.isFinite(y)) {
    sanitized.x = Math.round(x);
    sanitized.y = Math.round(y);
  }

  return sanitized;
}

function getSavedWindowBounds(worktreePath) {
  const state = readDesktopState();
  return sanitizeWindowBounds(state.windowBoundsByWorktree?.[worktreePath]);
}

function writeWindowBounds(worktreePath, browserWindow) {
  if (!worktreePath || !browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const state = readDesktopState();
  const currentBounds = browserWindow.isMaximized()
    ? browserWindow.getNormalBounds()
    : browserWindow.getBounds();

  const sanitized = sanitizeWindowBounds({
    ...currentBounds,
    isMaximized: browserWindow.isMaximized(),
  });

  writeDesktopState({
    ...state,
    windowBoundsByWorktree: {
      ...(state.windowBoundsByWorktree || {}),
      [worktreePath]: sanitized,
    },
  });
}

function scheduleWindowBoundsPersist(worktreePath, browserWindow) {
  if (!worktreePath || !browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const existingTimer = ctx.windowStateWriteTimers.get(worktreePath);
  if (existingTimer) {
    clearTimeout(existingTimer);
  }

  const timer = setTimeout(() => {
    writeWindowBounds(worktreePath, browserWindow);
    ctx.windowStateWriteTimers.delete(worktreePath);
  }, 200);

  ctx.windowStateWriteTimers.set(worktreePath, timer);
}

module.exports = {
  getUserStatePath,
  getLogDirectory,
  openBackendLogStream,
  readDesktopState,
  writeDesktopState,
  addRecentWorktree,
  persistOpenWorkspaces,
  getRecentWorktrees,
  getRecentWorktreeEntries,
  getRestorableWorktrees,
  sanitizeWindowBounds,
  getSavedWindowBounds,
  writeWindowBounds,
  scheduleWindowBoundsPersist,
};
