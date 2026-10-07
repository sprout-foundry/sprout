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
