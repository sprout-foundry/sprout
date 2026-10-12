/**
 * Guardrails for runs of the in-page (WASM) agent, which live and die with
 * the tab: while one is running the tab title carries a running mark and
 * leaving the page asks for confirmation; and a run cut off by a reload, a
 * close or the browser discarding the tab is reported on the next load
 * instead of leaving its question hanging unanswered.
 *
 * Runs are recorded in sessionStorage (which survives a reload and a tab
 * discard, and is per tab) for as long as they are in flight.
 */

import { notificationBus } from './notificationBus';

const MARKER_KEY = 'sprout:in-page-runs';
export const RUNNING_TITLE_PREFIX = '● ';

const running = new Set<string>();
let unloadGuardInstalled = false;

function readMarkers(): Record<string, number> {
  try {
    const raw = window.sessionStorage.getItem(MARKER_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : {};
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, number>) : {};
  } catch {
    return {};
  }
}

function writeMarkers(markers: Record<string, number>): void {
  try {
    if (Object.keys(markers).length === 0) window.sessionStorage.removeItem(MARKER_KEY);
    else window.sessionStorage.setItem(MARKER_KEY, JSON.stringify(markers));
  } catch {
    // Storage unavailable (private mode quota): the guard degrades to the
    // in-memory title and unload prompt.
  }
}

function confirmLeave(event: BeforeUnloadEvent): void {
  event.preventDefault();
  // Chromium still requires returnValue to be set for the prompt to show.
  event.returnValue = '';
}

function syncPageGuards(): void {
  if (typeof document === 'undefined') return;
  const title = document.title.startsWith(RUNNING_TITLE_PREFIX)
    ? document.title.slice(RUNNING_TITLE_PREFIX.length)
    : document.title;
  document.title = running.size > 0 ? RUNNING_TITLE_PREFIX + title : title;
  if (running.size > 0 && !unloadGuardInstalled) {
    window.addEventListener('beforeunload', confirmLeave);
    unloadGuardInstalled = true;
  } else if (running.size === 0 && unloadGuardInstalled) {
    window.removeEventListener('beforeunload', confirmLeave);
    unloadGuardInstalled = false;
  }
}

/** Record that an in-page run started or finished for a chat. */
export function noteInPageRun(chatId: string, isRunning: boolean): void {
  if (typeof window === 'undefined') return;
  const markers = readMarkers();
  if (isRunning) {
    running.add(chatId);
    markers[chatId] = Date.now();
  } else {
    running.delete(chatId);
    delete markers[chatId];
  }
  writeMarkers(markers);
  syncPageGuards();
}

/**
 * The chats whose recorded run is not running in this page: it was cut off
 * when the page went away. Their markers are cleared, so each is reported
 * once.
 */
export function takeInterruptedInPageRuns(): string[] {
  if (typeof window === 'undefined') return [];
  const markers = readMarkers();
  const interrupted = Object.keys(markers).filter((chatId) => !running.has(chatId));
  for (const chatId of interrupted) delete markers[chatId];
  writeMarkers(markers);
  return interrupted;
}

/** Tell the user about runs the last page load cut off. */
export function reportInterruptedInPageRuns(): void {
  const interrupted = takeInterruptedInPageRuns();
  if (interrupted.length === 0) return;
  notificationBus.notify(
    'warning',
    'Run interrupted',
    'The agent was still working when this page was closed or reloaded, so that run stopped. Send your message again to continue.',
    15000,
  );
}

/** Resets the in-memory state. Tests only. */
export function resetInPageRunGuard(): void {
  running.clear();
  syncPageGuards();
}
