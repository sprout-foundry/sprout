/**
 * The layered layout's Home place: platform pages (dashboard, tasks, account)
 * shown inside the editor's shell. Open/closed plus the platform route; the
 * embedded page itself stays loaded once opened so switching back and forth
 * never reloads either side.
 */

import { useSyncExternalStore } from 'react';

export interface HomeViewState {
  open: boolean;
  /** Platform SPA route, e.g. "/" or "/tasks". */
  path: string;
}

// Home is part of the editor's URL (?home=/tasks/123): a reload lands back on
// the same page, and links from elsewhere can open the shell on it.
const HOME_PARAM = 'home';

function readInitial(): HomeViewState {
  if (typeof window === 'undefined') return { open: false, path: '/' };
  const param = new URLSearchParams(window.location.search).get(HOME_PARAM);
  return param ? { open: true, path: normalizeHomePath(param) } : { open: false, path: '/' };
}

function writeUrl(next: HomeViewState): void {
  if (typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (next.open) url.searchParams.set(HOME_PARAM, next.path);
  else url.searchParams.delete(HOME_PARAM);
  if (url.href !== window.location.href) window.history.replaceState(window.history.state, '', url);
}

let state: HomeViewState = readInitial();
const listeners = new Set<() => void>();

function set(next: HomeViewState): void {
  if (next.open === state.open && next.path === state.path) return;
  state = next;
  writeUrl(next);
  for (const l of listeners) l();
}

/** Show a platform page ("/" for the dashboard). */
export function openHome(path = '/'): void {
  set({ open: true, path: normalizeHomePath(path) });
}

export function closeHome(): void {
  set({ ...state, open: false });
}

/** The embedded page navigated on its own; follow it without reopening. */
export function syncHomePath(path: string): void {
  set({ ...state, path: normalizeHomePath(path) });
}

export function getHomeView(): HomeViewState {
  return state;
}

/** Accepts "/tasks", "#/tasks" or "/?from=editor#/tasks". */
export function normalizeHomePath(path: string): string {
  const hash = path.includes('#') ? path.slice(path.indexOf('#') + 1) : path;
  const route = hash.split('?')[0] || '/';
  return route.startsWith('/') ? route : `/${route}`;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useHomeView(): HomeViewState {
  return useSyncExternalStore(subscribe, getHomeView, getHomeView);
}

export function __resetHomeViewForTests(): void {
  state = { open: false, path: '/' };
  listeners.clear();
}
