/**
 * The repository the cloud workspace is working on.
 *
 * Everything that leaves the browser for a container (transactional runs,
 * agent escalation, full workspaces) needs a repo URL. It comes from ?repo=
 * when the editor was opened for a repository, otherwise from the most
 * recent import — including clones started inside the editor, which never
 * touch the URL.
 */

import { useEffect, useSyncExternalStore } from 'react';
import { getLastRepo, setLastRepo } from './repoImportCache';

type Listener = () => void;

let activeRepoURL: string | null = readQueryRepo();
let hydration: Promise<void> | null = null;
const listeners = new Set<Listener>();

function readQueryRepo(): string | null {
  if (typeof window === 'undefined') return null;
  const repo = new URLSearchParams(window.location.search).get('repo');
  return repo && repo.trim() ? repo.trim() : null;
}

function notify(): void {
  for (const listener of listeners) listener();
}

export function getActiveRepoURL(): string | null {
  return activeRepoURL;
}

/** Record a newly imported or cloned repo as the active one and persist it. */
export function setActiveRepoURL(url: string): void {
  const next = url.trim();
  if (!next || next === activeRepoURL) return;
  activeRepoURL = next;
  void setLastRepo(next);
  notify();
}

/** Fill the active repo from the last import when the URL named none. */
export function hydrateActiveRepoURL(): Promise<void> {
  if (!hydration) {
    hydration = (async () => {
      if (activeRepoURL) return;
      const last = await getLastRepo();
      if (last && !activeRepoURL) {
        activeRepoURL = last;
        notify();
      }
    })();
  }
  return hydration;
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useActiveRepoURL(): string | undefined {
  useEffect(() => {
    void hydrateActiveRepoURL();
  }, []);
  return useSyncExternalStore(subscribe, getActiveRepoURL, getActiveRepoURL) ?? undefined;
}

export function __resetActiveRepoForTests(): void {
  activeRepoURL = readQueryRepo();
  hydration = null;
  listeners.clear();
}
