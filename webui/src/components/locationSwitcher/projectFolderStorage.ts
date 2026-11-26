import { normalizePath } from './pathUtils';
import {
  RECENT_PROJECT_FOLDERS_KEY,
  REMOTE_RECENT_PROJECT_FOLDERS_KEY,
  SSH_FAVORITE_PROJECT_FOLDERS_KEY,
  EARLIER_RECENT_PROJECT_FOLDERS_KEY,
  EARLIER_REMOTE_RECENT_PROJECT_FOLDERS_KEY,
  EARLIER_SSH_FAVORITE_PROJECT_FOLDERS_KEY,
  MAX_RECENT_PROJECT_FOLDERS,
} from './types';
import { readMovedItem } from '../../services/movedStorageKey';

export const readRecentProjectFolders = (): string[] => {
  if (typeof window === 'undefined') {
    return [];
  }
  try {
    const raw = readMovedItem(window.localStorage, RECENT_PROJECT_FOLDERS_KEY, EARLIER_RECENT_PROJECT_FOLDERS_KEY);
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
      .slice(0, MAX_RECENT_PROJECT_FOLDERS);
  } catch {
    return [];
  }
};

export const writeRecentProjectFolders = (paths: string[]) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(RECENT_PROJECT_FOLDERS_KEY, JSON.stringify(paths.slice(0, MAX_RECENT_PROJECT_FOLDERS)));
};

export const readRemoteRecentProjectFolders = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = readMovedItem(
      window.localStorage,
      REMOTE_RECENT_PROJECT_FOLDERS_KEY,
      EARLIER_REMOTE_RECENT_PROJECT_FOLDERS_KEY,
    );
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
              .slice(0, MAX_RECENT_PROJECT_FOLDERS)
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
  window.localStorage.setItem(REMOTE_RECENT_PROJECT_FOLDERS_KEY, JSON.stringify(value));
};

export const readSSHFavoriteProjectFolders = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = readMovedItem(
      window.localStorage,
      SSH_FAVORITE_PROJECT_FOLDERS_KEY,
      EARLIER_SSH_FAVORITE_PROJECT_FOLDERS_KEY,
    );
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
              .slice(0, MAX_RECENT_PROJECT_FOLDERS)
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
    window.localStorage.setItem(SSH_FAVORITE_PROJECT_FOLDERS_KEY, JSON.stringify(value));
  } catch {
    // QuotaExceededError: storage is full; the favorites won't persist this session
    // but shouldn't crash the UI.
  }
};
