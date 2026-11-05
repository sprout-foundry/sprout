/**
 * APIAdapter — abstraction layer for sprout webui backend communication.
 *
 * In local mode, the default adapter uses clientFetch (same-origin fetch to Go backend).
 * In cloud mode, a CloudAdapter is installed that routes to the Foundry platform API.
 *
 * Adapter and PlatformNavItem types are imported from @sprout/ui (canonical source).
 */

// Import and use APIAdapter locally
import type { APIAdapter } from '@sprout/ui';

// Re-export for downstream consumers
export type { APIAdapter, PlatformNavItem } from '@sprout/ui';

// Singleton adapter instance
let activeAdapter: APIAdapter | null = null;

/**
 * Fired after the active adapter singleton changes (installAdapter or
 * uninstallAdapter).
 *
 * The adapter is installed asynchronously (after the bootstrap fetch resolves),
 * so providers that cache the adapter at mount time listen for this event to
 * re-read it.
 */
export const ADAPTER_INSTALLED_EVENT = 'sprout:adapter-installed';

/**
 * Install an API adapter. Called once at app startup.
 * If never called, clientFetch uses the default local behavior.
 */
export function installAdapter(adapter: APIAdapter): void {
  activeAdapter = adapter;
  // Log first so console order matches: install log, then provider updates.
  console.warn(`[apiAdapter] Installed: ${adapter.name}`);
  // Notify React providers (PlatformNavProvider, SproutAdapterProvider) that
  // the adapter is available so they re-read the singleton.
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(ADAPTER_INSTALLED_EVENT));
  }
}

/**
 * Uninstall the active adapter, clearing the singleton.
 *
 * Pass the adapter instance to clear only if it is the one currently
 * installed — this is how a mounted workspace uninstalls the adapter its own
 * wiring installed without touching one the host (or the app's bootstrap)
 * installed for the page. Omit it to clear whatever is installed. Like
 * installAdapter, it notifies adapter-aware providers so they re-read the
 * (now null) singleton.
 */
export function uninstallAdapter(adapter?: APIAdapter): void {
  if (activeAdapter === null) return;
  if (adapter !== undefined && activeAdapter !== adapter) return;
  activeAdapter = null;
  console.warn('[apiAdapter] Uninstalled');
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(ADAPTER_INSTALLED_EVENT));
  }
}

/**
 * Get the currently installed adapter, or null for default local mode.
 */
export function getAdapter(): APIAdapter | null {
  return activeAdapter;
}

/**
 * Check if an adapter has been installed (cloud mode).
 */
export function hasAdapter(): boolean {
  return activeAdapter !== null;
}

/**
 * Returns true if the installed adapter requires backend health checks.
 * Returns false when no adapter is installed (local mode) or when the
 * adapter's requiresBackendHealthCheck property is not explicitly true.
 *
 * Read at call time: the singleton normally changes once at startup, but a
 * mounted workspace that swaps its agent backend in place uninstalls the
 * in-browser adapter when it moves to a daemon, so a cached answer can go
 * stale.
 */
export function requiresBackendHealthCheck(): boolean {
  return activeAdapter?.requiresBackendHealthCheck === true;
}
