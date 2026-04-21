/**
 * Protocol handling for the desktop app.
 * Handles custom URL scheme (ledit://) registration and parsing.
 */

const { app } = require('electron');
const path = require('node:path');

/**
 * Register the 'ledit://' protocol handler with the OS.
 * This allows users to open workspaces via ledit:// URLs.
 */
function registerDesktopProtocol() {
  if (app.isPackaged) {
    app.setAsDefaultProtocolClient('ledit');
    return;
  }

  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient('ledit', process.execPath, [path.resolve(process.argv[1])]);
    return;
  }

  app.setAsDefaultProtocolClient('ledit');
}

/**
 * Extract workspace path from a variety of open target formats.
 * Supports both file paths and ledit:// protocol URLs.
 *
 * @param {string} candidate - Path or URL to extract workspace from
 * @returns {string|null} Resolved workspace path or null if invalid
 */
function extractWorkspacePathFromOpenTarget(candidate) {
  if (!candidate) {
    return null;
  }

  if (candidate.startsWith('ledit://')) {
    try {
      const parsed = new URL(candidate);
      const requestedPath = parsed.searchParams.get('path') || parsed.searchParams.get('workspace');
      if (!requestedPath) {
        return null;
      }
      return path.resolve(requestedPath);
    } catch (error) {
      console.error('Failed to parse ledit:// URL:', candidate, error);
      return null;
    }
  }

  return path.resolve(candidate);
}

module.exports = {
  registerDesktopProtocol,
  extractWorkspacePathFromOpenTarget,
};
