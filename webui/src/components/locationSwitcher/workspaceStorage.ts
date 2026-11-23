import { normalizePath } from './pathUtils';
import {
  RECENT_WORKSPACES_KEY,
  REMOTE_RECENT_WORKSPACES_KEY,
  SSH_FAVORITE_WORKSPACES_KEY,
  MAX_RECENT_WORKSPACES,
} from './types';

export const readRecentProjectFolders = (): string[] => {
  if (typeof window === 'undefined') {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(RECENT_WORKSPACES_KEY);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .map((value) => (typeof value === 'string' ? normalizePath(value) : ''))
      .filter(Boolean)
      .slice(0, MAX_RECENT_WORKSPACES);
  } catch {
    return [];
  }
};

export const writeRecentProjectFolders = (paths: string[]) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(RECENT_WORKSPACES_KEY, JSON.stringify(paths.slice(0, MAX_RECENT_WORKSPACES)));
};

export const readRemoteRecentProjectFolders = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(REMOTE_RECENT_WORKSPACES_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).map(([hostAlias, value]) => [
        hostAlias,
        Array.isArray(value)
          ? value
              .map((entry) => (typeof entry === 'string' ? normalizePath(entry) : ''))
              .filter(Boolean)
              .slice(0, MAX_RECENT_WORKSPACES)
          : [],
      ]),
    );
  } catch {
    return {};
  }
};

export const writeRemoteRecentProjectFolders = (value: Record<string, string[]>) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify(value));
};

export const readSSHFavoriteProjectFolders = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(SSH_FAVORITE_WORKSPACES_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).map(([hostAlias, value]) => [
        hostAlias,
        Array.isArray(value)
          ? value
              .map((entry) => (typeof entry === 'string' ? normalizePath(entry) : ''))
              .filter(Boolean)
              .slice(0, MAX_RECENT_WORKSPACES)
          : [],
      ]),
    );
  } catch {
    return {};
  }
};

export const writeSSHFavoriteProjectFolders = (value: Record<string, string[]>) => {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    window.localStorage.setItem(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify(value));
  } catch {
    // QuotaExceededError: storage is full; the favorites won't persist this session
    // but shouldn't crash the UI.
  }
};
