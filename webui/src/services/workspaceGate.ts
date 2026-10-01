import { useSyncExternalStore } from 'react';

/**
 * Whether the workspace gate is showing. Other first-run dialogs (provider
 * setup) wait for it, so a first open asks one thing at a time instead of
 * stacking dialogs that cover each other.
 */
let gateOpen = false;
const listeners = new Set<() => void>();

export function setWorkspaceGateOpen(open: boolean): void {
  if (gateOpen === open) return;
  gateOpen = open;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useWorkspaceGateOpen(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => gateOpen,
    () => false,
  );
}
