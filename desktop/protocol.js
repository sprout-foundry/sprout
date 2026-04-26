/**
 * Protocol handling for the desktop app.
 * Handles custom URL scheme (sprout://) registration and parsing.
 */

const { app } = require('electron');
const path = require('node:path');

/**
 * Register the 'sprout://' protocol handler with the OS.
 * This allows users to open workspaces via sprout:// URLs.
 */
function registerDesktopProtocol() {
  if (app.isPackaged) {
    app.setAsDefaultProtocolClient('sprout');
    return;
  }

  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient('sprout', process.execPath, [path.resolve(process.argv[1])]);
    return;
  }

  app.setAsDefaultProtocolClient('sprout');
}

/**
 * Extract workspace path from a variety of open target formats.
 * Supports both file paths and sprout:// protocol URLs.
 *
 * @param {string} candidate - Path or URL to extract workspace from
 * @returns {string|null} Resolved workspace path or null if invalid
 */
function extractWorkspacePathFromOpenTarget(candidate) {
  if (!candidate) {
    return null;
  }

  if (candidate.startsWith('sprout://')) {
    try {
      const parsed = new URL(candidate);
      const requestedPath = parsed.searchParams.get('path') || parsed.searchParams.get('workspace');
      if (!requestedPath) {
        return null;
      }
      return path.resolve(requestedPath);
    } catch (error) {
      console.error('Failed to parse sprout:// URL:', candidate, error);
      return null;
    }
  }

  return path.resolve(candidate);
}

module.exports = {
  registerDesktopProtocol,
  extractWorkspacePathFromOpenTarget,
};
