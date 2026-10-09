import type { SproutHost } from './types';

/**
 * Fired (on window) after setActiveHost() records the active host. config/mode
 * listens for it to re-derive its capability bindings from the host, mirroring
 * how it listens for ADAPTER_INSTALLED_EVENT. Dispatched only in a browser
 * context (guarded by the caller of window access).
 */
export const HOST_UPDATED_EVENT = 'sprout:host-updated';

/**
 * The active host as a module singleton, for code that cannot call useHost()
 * — services, module-scope helpers, and non-React accessors. The entry point
 * sets it once at startup to the same instance it passes to <HostProvider>,
 * so the two channels always agree.
 *
 * Null before the entry sets it and in pure test contexts (tests that import
 * a service without an app entry). Consumers reading a capability fall back
 * to the mode/adapter defaults when the host is null.
 */
let activeHost: SproutHost | null = null;

/**
 * Hooks run after setActiveHost() records a new host. bootstrapAdapter
 * registers one so it can re-resolve its URL fallback and (re)install the
 * adapter against the host — the entry imports bootstrapAdapter (whose
 * auto-run fires immediately) BEFORE it calls setActiveHost, so the adapter
 * install must follow the host, not the other way round. Kept as a registry
 * of plain callbacks rather than a window event so it is deterministically
 * scoped to this module instance (a window listener survives
 * vi.resetModules() in tests and fires against a torn module graph).
 */
type HostChangeHook = () => void;
const hostChangeHooks = new Set<HostChangeHook>();

/** Register a callback invoked after the active host is recorded. */
export function registerHostChangeHook(hook: HostChangeHook): void {
  hostChangeHooks.add(hook);
}

/** Remove a previously registered host-change hook. */
export function unregisterHostChangeHook(hook: HostChangeHook): void {
  hostChangeHooks.delete(hook);
}

/**
 * Hooks run after a host's capabilities are re-synced from the installed
 * adapter (see upsertActiveHostCapabilities): config/mode re-derives its
 * capability bindings so a host shell's declared capabilities reach the
 * `supports*` bindings. A registry of plain callbacks rather than a window
 * event, for the same determinism reason as the host-change hooks.
 */
type HostCapabilityHook = () => void;
const hostCapabilityHooks = new Set<HostCapabilityHook>();

/** Register a callback invoked after a host's capabilities are re-synced. */
export function registerActiveHostCapabilitiesHook(hook: HostCapabilityHook): void {
  hostCapabilityHooks.add(hook);
}

/**
 * Run the registered capability hooks (a host shell's declared capabilities
 * landed on the active host). Each hook is isolated so one failure cannot stop
 * the others or take the app down.
 */
export function notifyActiveHostCapabilities(): void {
  for (const hook of hostCapabilityHooks) {
    try {
      hook();
    } catch {
      // A capability hook must not take the app down.
    }
  }
}

/**
 * Record the active host. Called once by the entry point with the same host
 * instance it passes to <HostProvider>. Re-dispatching with the same instance
 * is safe; the value is what a later getActiveHost() returns.
 *
 * Also fires HOST_UPDATED_EVENT on window so module-scope consumers (config/mode
 * capability bindings) refresh from the host, and runs the registered
 * host-change hooks. A browser is required for the event; the singleton and
 * the hooks run either way.
 */
export function setActiveHost(host: SproutHost): void {
  activeHost = host;
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(HOST_UPDATED_EVENT));
  }
  for (const hook of hostChangeHooks) {
    try {
      hook();
    } catch {
      // A host-change hook must not take the app down.
    }
  }
}

/**
 * Return the active host, or null before the entry sets it. Non-React code
 * (services, module-scope helpers) reads capabilities through this accessor
 * instead of useHost(), which requires a React render context.
 */
export function getActiveHost(): SproutHost | null {
  return activeHost;
}

/**
 * Register a host as the active one for the module-level services, and return
 * a function that restores whatever host was active before. This is the
 * embedding counterpart to the entry point's own registration: the entry
 * selects a host once at startup, and a component that mounts a workspace in
 * its own provider stack registers its host while it is mounted so the
 * non-React services (client session fetch, WebSocket URL) resolve against it.
 *
 * The restore function puts the previous host back, so unmounting a mounted
 * workspace does not leave its host installed for whatever runs next. When no
 * host was active before (the embedding case), the restore keeps the
 * registered host rather than clearing the singleton to null: the accessor has
 * no null setter, and a mounted-then-unmounted workspace leaving a resolvable
 * host is safer than leaving the services with none. The restore is
 * idempotent: calling it more than once restores only on the first call.
 */
export function registerActiveHost(host: SproutHost): () => void {
  const previous = activeHost;
  setActiveHost(host);
  let restored = false;
  return () => {
    if (restored) return;
    restored = true;
    setActiveHost(previous ?? host);
  };
}

/**
 * Like setActiveHost, but for a host ALREADY active: re-dispatch
 * HOST_UPDATED_EVENT and run the capability hooks so a mutation made to the
 * live host (a host shell's declared capabilities landing on it after the
 * adapter installs) reaches the `supports*` bindings. Runs no host-change
 * hooks — the host itself did not change, only what it declares.
 */
export function upsertActiveHostCapabilities(): void {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(HOST_UPDATED_EVENT));
  }
  notifyActiveHostCapabilities();
}
