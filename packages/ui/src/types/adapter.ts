/**
 * Adapter types for @sprout/ui.
 *
 * NOTE: These interfaces are duplicated in webui/src/services/apiAdapter.ts.
 * Changes here MUST be mirrored there until the types are extracted to a shared package.
 */

/** Platform-specific navigation item */
export interface PlatformNavItem {
  id: string;
  label: string;
  href: string;
  icon?: string;
  order?: number;
}

/** Adapter interface for backend communication */
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
