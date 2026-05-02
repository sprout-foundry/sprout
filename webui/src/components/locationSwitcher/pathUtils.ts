import { SSHBrowseQuery } from './types';

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
  const withoutTrailingSlash =
    normalized.length > 1 && endsWithSlash ? normalized.replace(/\/+$/, '') : normalized;

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