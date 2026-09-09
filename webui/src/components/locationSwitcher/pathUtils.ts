import type { SSHBrowseQuery } from './types';

export const normalizePath = (rawPath: string): string => {
  let normalized = rawPath.trim().replace(/\/+/g, '/');
  if (!normalized) {
    return '';
  }
  if (!normalized.startsWith('/')) {
    normalized = `/${normalized}`;
  }
  if (normalized.length > 1 && normalized.endsWith('/')) {
    normalized = normalized.slice(0, -1);
  }
  return normalized;
};

export const getPathDisplayName = (path: string): string => {
  const normalized = normalizePath(path);
  const segments = normalized.split('/').filter(Boolean);
  if (segments.length <= 2) {
    return segments.join('/') || normalized || 'No workspace';
  }
  return segments.slice(-2).join('/');
};

export const collapseHomePath = (path: string, homePath?: string): string => {
  const trimmedPath = (path || '').trim();
  const trimmedHome = (homePath || '').trim();
  if (!trimmedPath) {
    return '';
  }
  if (!trimmedHome) {
    return trimmedPath;
  }
  if (trimmedPath === trimmedHome) {
    return '~';
  }
  if (trimmedPath.startsWith(`${trimmedHome}/`)) {
    return `~${trimmedPath.slice(trimmedHome.length)}`;
  }
  return trimmedPath;
};

export interface BrowseTarget {
  /** Directory to request from the browse endpoint (always within the daemon root). */
  browsePath: string;
  /** In-progress last segment used to filter suggestions; empty for whole-directory browsing. */
  prefix: string;
}

/**
 * Compute the directory the workspace switcher should browse for suggestions,
 * clamped to the daemon root.
 *
 * The switcher pre-fills its input with the current workspace root. When the
 * workspace IS the home directory (and the daemon root is home), the natural
 * "browse the parent of the input" targets a directory ABOVE the daemon root,
 * which the backend rejects with `directory_outside_daemon_root`. The clamp
 * makes a home-workspace input browse the daemon root itself instead.
 *
 * When `daemonRoot` is empty (not yet loaded) the natural parent is returned
 * unchanged, preserving prior behavior. Returns null for empty input.
 */
export function getBrowseTarget(rawInput: string, daemonRoot: string): BrowseTarget | null {
  const normalizedInput = normalizePath(rawInput);
  if (!normalizedInput) return null;

  let browsePath: string;
  let prefix: string;
  if (rawInput.trim().endsWith('/')) {
    browsePath = normalizedInput;
    prefix = '';
  } else {
    const segments = normalizedInput.split('/');
    prefix = segments.filter(Boolean).pop() ?? '';
    browsePath = normalizePath(segments.slice(0, -1).join('/')) || '/';
  }

  const root = daemonRoot ? normalizePath(daemonRoot) : '';
  if (root && !isWithinOrEqual(browsePath, root)) {
    browsePath = root;
    // The input points above the allowed area; show the root's contents
    // unfiltered instead of filtering root entries by a foreign segment.
    prefix = '';
  }

  return { browsePath, prefix };
}

function isWithinOrEqual(path: string, root: string): boolean {
  if (path === root) return true;
  const rootPrefix = root === '/' ? '/' : `${root}/`;
  return path.startsWith(rootPrefix);
}

export const getSSHBrowseQuery = (rawPath: string): SSHBrowseQuery => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return { browsePath: '$HOME', prefix: '' };
  }

  const normalized = trimmed.replace(/^~(?=\/|$)/, '$HOME').replace(/\/+/g, '/');
  if (normalized === '$HOME') {
    return { browsePath: '$HOME', prefix: '' };
  }

  const endsWithSlash = normalized.endsWith('/');
  const withoutTrailingSlash = normalized.length > 1 && endsWithSlash ? normalized.replace(/\/+$/, '') : normalized;

  if (withoutTrailingSlash.startsWith('$HOME/')) {
    const lastSlash = withoutTrailingSlash.lastIndexOf('/');
    if (endsWithSlash) {
      return { browsePath: withoutTrailingSlash, prefix: '' };
    }
    return {
      browsePath: lastSlash > '$HOME'.length ? withoutTrailingSlash.slice(0, lastSlash) : '$HOME',
      prefix: withoutTrailingSlash.slice(lastSlash + 1),
    };
  }

  if (withoutTrailingSlash.startsWith('/')) {
    const lastSlash = withoutTrailingSlash.lastIndexOf('/');
    if (endsWithSlash) {
      return { browsePath: withoutTrailingSlash || '/', prefix: '' };
    }
    return {
      browsePath: lastSlash > 0 ? withoutTrailingSlash.slice(0, lastSlash) : '/',
      prefix: withoutTrailingSlash.slice(lastSlash + 1),
    };
  }

  return { browsePath: '$HOME', prefix: withoutTrailingSlash };
};
