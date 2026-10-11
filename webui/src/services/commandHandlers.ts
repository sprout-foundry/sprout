/**
 * Who handles the web UI's command events.
 *
 * Shortcuts and header controls talk to their handlers through window events
 * (`sprout:hotkey` with a command id, `sprout:open-command-palette`, ...).
 * Sprout's own app root listens for all of them, but a host that embeds the
 * workspace mounts only the views it composes, so many of those events have
 * no listener there. This registry lets the producers ask first:
 *
 * - The standalone app claims the page with `claimAppShell()` before it
 *   renders. The app shell owns the whole document's shortcuts and handles
 *   every command and app-level event, so everything reads as handled and the
 *   app behaves as it always has.
 * - Anywhere else (an embed), a command counts as handled only while a
 *   mounted listener has registered it (`registerCommandHandler` /
 *   `useCommandHandler`), and an event only while one has registered for it
 *   (`registerEventHandler` / `useEventHandlerRegistration`).
 *
 * Framework-agnostic core plus thin React hooks; module-level state, as the
 * events themselves are window-global.
 */

import { useLayoutEffect, useSyncExternalStore } from 'react';

type CommandMatcher = (commandId: string) => boolean;

let appShell = false;
const commandMatchers = new Set<CommandMatcher>();
const eventHandlers = new Map<string, number>();
const subscribers = new Set<() => void>();
let version = 0;

function changed(): void {
  version += 1;
  for (const notify of subscribers) notify();
}

function subscribe(notify: () => void): () => void {
  subscribers.add(notify);
  return () => subscribers.delete(notify);
}

function getVersion(): number {
  return version;
}

/**
 * Mark this page as Sprout's standalone app. Call once from the app entry
 * before the first render.
 */
export function claimAppShell(): void {
  if (appShell) return;
  appShell = true;
  changed();
}

/** Whether the standalone app owns this page (and its shortcuts). */
export function isAppShell(): boolean {
  return appShell;
}

/**
 * Register a listener for `sprout:hotkey` commands: a list of command ids or a
 * predicate. Returns the unregister function.
 */
export function registerCommandHandler(commands: readonly string[] | CommandMatcher): () => void {
  const matcher: CommandMatcher =
    typeof commands === 'function' ? commands : (id) => (commands as readonly string[]).includes(id);
  commandMatchers.add(matcher);
  changed();
  return () => {
    if (commandMatchers.delete(matcher)) changed();
  };
}

/** Whether something would act on a `sprout:hotkey` event for `commandId`. */
export function hasCommandHandler(commandId: string): boolean {
  if (appShell) return true;
  for (const match of commandMatchers) {
    if (match(commandId)) return true;
  }
  return false;
}

/** Register a listener for a window event; returns the unregister function. */
export function registerEventHandler(eventName: string): () => void {
  eventHandlers.set(eventName, (eventHandlers.get(eventName) ?? 0) + 1);
  changed();
  let registered = true;
  return () => {
    if (!registered) return;
    registered = false;
    const count = (eventHandlers.get(eventName) ?? 1) - 1;
    if (count > 0) eventHandlers.set(eventName, count);
    else eventHandlers.delete(eventName);
    changed();
  };
}

/** Whether something listens for the window event `eventName`. */
export function hasEventHandler(eventName: string): boolean {
  return appShell || (eventHandlers.get(eventName) ?? 0) > 0;
}

/**
 * Register the commands a mounted `sprout:hotkey` listener handles for as
 * long as the component is mounted. A layout effect, so a control gated on
 * the registration appears in the same paint as its handler. Pass a stable
 * array or predicate.
 */
export function useCommandHandler(commands: readonly string[] | CommandMatcher): void {
  useLayoutEffect(() => registerCommandHandler(commands), [commands]);
}

/** Register a window-event listener for as long as the component is mounted. */
export function useEventHandlerRegistration(eventName: string): void {
  useLayoutEffect(() => registerEventHandler(eventName), [eventName]);
}

/**
 * Subscribe a component to registry changes; returns a counter that changes
 * whenever a handler is registered or removed (usable as a memo dependency).
 */
export function useCommandHandlerVersion(): number {
  return useSyncExternalStore(subscribe, getVersion, getVersion);
}

/** True while any of `commandIds` has a handler; re-renders as that changes. */
export function useHasCommandHandler(commandIds: readonly string[]): boolean {
  useSyncExternalStore(subscribe, getVersion, getVersion);
  return commandIds.some(hasCommandHandler);
}

/** True while `eventName` has a listener; re-renders as that changes. */
export function useHasEventHandler(eventName: string): boolean {
  useSyncExternalStore(subscribe, getVersion, getVersion);
  return hasEventHandler(eventName);
}

export function __resetCommandHandlersForTests(): void {
  appShell = false;
  commandMatchers.clear();
  eventHandlers.clear();
  changed();
}
