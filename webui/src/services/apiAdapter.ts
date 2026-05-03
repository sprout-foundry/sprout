/**
 * APIAdapter — abstraction layer for sprout webui backend communication.
 *
 * In local mode, the default adapter uses clientFetch (same-origin fetch to Go backend).
 * In cloud mode, a CloudAdapter is installed that routes to the Foundry platform API.
 *
 * Adapter and PlatformNavItem types are imported from @sprout/ui (canonical source).
 */

// Import canonical types from @sprout/ui
import type { APIAdapter, PlatformNavItem } from '@sprout/ui';

// Re-export for downstream consumers
export type { APIAdapter, PlatformNavItem } from '@sprout/ui';

// Singleton adapter instance
let activeAdapter: APIAdapter | null = null;

/**
 * Install an API adapter. Called once at app startup.
 * If never called, clientFetch uses the default local behavior.
 */
export function installAdapter(adapter: APIAdapter): void {
  activeAdapter = adapter;
  console.log(`[apiAdapter] Installed: ${adapter.name}`);
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
 * Adapters are installed once at app startup and never change, so the
 * return value is effectively constant across the application lifecycle.
 */
export function requiresBackendHealthCheck(): boolean {
  return activeAdapter?.requiresBackendHealthCheck === true;
}
