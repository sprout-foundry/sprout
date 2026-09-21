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
  /**
   * Optional ambient signal rendered on the rail icon (SP-016): a positive
   * number or non-empty string shows a small dot/count (e.g. the pending
   * task count on the Tasks item, an overage state on Billing). Absent (or
   * zero/empty) means no indicator.
   */
  readonly badge?: number | string;
  /**
   * Whether clicking this item exits the editor to the platform surface at
   * `href` (SP-016). When true the host navigates the top-level frame; when
   * absent/false the host keeps the item in-editor (e.g. a registered plugin
   * view for the same id). The platform serves this explicitly so the host
   * no longer has to guess from the plugin-view registry.
   */
  readonly external?: boolean;
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
  /**
   * Whether the shell provides a native folder picker + project-create flow
   * (studio shells, bridge `files` channel). Optional: omitted by adapters
   * that never offer it, so `capability()` falls back to its mode default.
   */
  readonly supportsFolderPicker?: boolean;
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
