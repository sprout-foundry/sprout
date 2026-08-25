/**
 * Adapter types for @sprout/ui.
 *
 * This is the canonical source for APIAdapter and PlatformNavItem interfaces.
 * These types are re-exported from webui/src/services/apiAdapter to maintain
 * backward compatibility with existing imports.
 */

/** Platform-specific navigation item */
export interface PlatformNavItem {
  readonly id: string;
  readonly label: string;
  readonly href: string;
  readonly icon?: string;
  readonly order?: number;
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
  /** Whether git clone/status/commit operations are available */
  readonly supportsGit: boolean;
  /** Whether AI chat with LLM proxy is available */
  readonly supportsChat: boolean;
  /** Whether workspace switcher/selection is available */
  readonly supportsWorkspaceSwitching: boolean;
  /** Whether "Export all" button should be shown */
  readonly supportsExport: boolean;
  /** Whether instance management is supported */
  readonly supportsInstances: boolean;
  /** Whether local PTY terminal is supported */
  readonly supportsLocalTerminal: boolean;
  /** Whether settings panel should be shown */
  readonly supportsSettings: boolean;
  /** Platform-specific routes to inject into the sidebar (e.g., billing, tasks) */
  readonly platformNavItems?: readonly PlatformNavItem[];
}
