/**
 * APIAdapter — abstraction layer for sprout webui backend communication.
 *
 * In local mode, the default adapter uses clientFetch (same-origin fetch to Go backend).
 * In cloud mode, a CloudAdapter is installed that routes to the Foundry platform API.
 *
 * This is the foundation for Option A (shared component library): components
 * will eventually accept adapters via context instead of calling clientFetch directly.
 *
 * NOTE: The APIAdapter and PlatformNavItem interfaces are duplicated in
 * packages/ui/src/types/adapter.ts for the @sprout/ui component library.
 * Changes here MUST be mirrored there until the types are extracted to a shared package.
 */

export interface APIAdapter {
  /** Human-readable name for debugging */
  readonly name: string;

  /** Make an HTTP request to the backend */
  fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;

  /** Get the WebSocket URL for real-time events. Return null if WS not supported. */
  getWebSocketURL(): string | null;

  /** Whether this adapter requires backend reachability checks */
  readonly requiresBackendHealthCheck: boolean;

  /** Whether file operations go through the HTTP API (vs handled client-side by WASM) */
  readonly fileOpsViaAPI: boolean;

  /** Whether onboarding flow should be shown */
  readonly showOnboarding: boolean;

  /** Whether SSH connections are supported */
  readonly supportsSSH: boolean;

  /** Whether instance management is supported */
  readonly supportsInstances: boolean;

  /** Whether local PTY terminal is supported */
  readonly supportsLocalTerminal: boolean;

  /** Whether settings panel should be shown */
  readonly supportsSettings: boolean;

  /** Platform-specific routes to inject into the sidebar (e.g., billing, tasks) */
  readonly platformNavItems?: PlatformNavItem[];
}

export interface PlatformNavItem {
  id: string;
  label: string;
  href: string;
  icon?: string;
  order?: number;
}

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
