/**
 * Repositories recently opened in this browser, newest first — the layered
 * layout's project rail. Kept in localStorage: a short list of URLs, no
 * repository content.
 */

import { useSyncExternalStore } from 'react';

const STORAGE_KEY = 'sprout-recent-repos';
const MAX_RECENT = 6;
const CHANGED_EVENT = 'sprout:recent-repos-changed';

function read(): string[] {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? '[]');
    return Array.isArray(parsed) ? parsed.filter((u): u is string => typeof u === 'string') : [];
  } catch {
    return [];
  }
}

let cache: string[] | null = null;

export function getRecentRepos(): string[] {
  if (typeof window === 'undefined') return [];
  if (!cache) cache = read();
  return cache;
}

export function recordRecentRepo(url: string): void {
  if (typeof window === 'undefined' || !url.trim()) return;
  const normalized = url.trim().replace(/\.git$/, '');
  const next = [normalized, ...getRecentRepos().filter((u) => u !== normalized)].slice(0, MAX_RECENT);
  cache = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Storage full or unavailable: the in-memory list still serves this tab.
  }
  window.dispatchEvent(new Event(CHANGED_EVENT));
}

function subscribe(listener: () => void): () => void {
  window.addEventListener(CHANGED_EVENT, listener);
  return () => window.removeEventListener(CHANGED_EVENT, listener);
}

export function useRecentRepos(): string[] {
  return useSyncExternalStore(subscribe, getRecentRepos, () => []);
}

export function __resetRecentReposForTests(): void {
  cache = null;
}
