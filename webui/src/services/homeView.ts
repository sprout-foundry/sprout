/**
 * The layered layout's Home place: platform pages (dashboard, tasks, account)
 * shown inside the editor's shell. Open/closed plus the platform route; the
 * embedded page itself stays loaded once opened so switching back and forth
 * never reloads either side.
 */

import { useSyncExternalStore } from 'react';
import { isLayeredLayout } from '../config/layout';
import { isCloud } from '../config/mode';

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

// Entering or leaving Home is a browser history entry, so Back and Forward
// move between the editor and Home. Moving between Home pages is not — the
// embedded page's own history already records those.
function writeUrl(next: HomeViewState, entry: 'push' | 'replace'): void {
  if (typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (next.open) url.searchParams.set(HOME_PARAM, next.path);
  else url.searchParams.delete(HOME_PARAM);
  if (url.href === window.location.href) return;
  if (entry === 'push') window.history.pushState(window.history.state, '', url);
  else window.history.replaceState(window.history.state, '', url);
}

let state: HomeViewState = readInitial();
const listeners = new Set<() => void>();

function set(next: HomeViewState, fromHistory = false): void {
  if (next.open === state.open && next.path === state.path) return;
  const entry = next.open !== state.open ? 'push' : 'replace';
  state = next;
  if (!fromHistory) writeUrl(next, entry);
  for (const l of listeners) l();
}

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => set(readInitial(), true));
}

/**
 * The editor's query string for opening another repository: Home is left
 * behind so the editor comes up on the new project, not back in Home.
 */
export function searchForRepo(repo: string): string {
  const params = new URLSearchParams(window.location.search);
  params.set('repo', repo);
  params.delete(HOME_PARAM);
  return params.toString();
}

/** Show a platform page ("/" for the dashboard). */
export function openHome(path = '/'): void {
  set({ open: true, path: normalizeHomePath(path) });
}

/**
 * A platform page the editor links to ("/#/tasks/42"). The layered layout
 * shows it in Home, keeping the editor loaded; returns false when the caller
 * should navigate instead.
 */
export function openPlatformPage(path: string): boolean {
  if (!isLayeredLayout || !isCloud) return false;
  openHome(path);
  return true;
}

/** onClick for an <a> to a platform page: a plain click opens it in Home. */
export function onPlatformLinkClick(path: string) {
  return (e: { metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; button: number; preventDefault(): void }) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
    if (openPlatformPage(path)) e.preventDefault();
  };
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
