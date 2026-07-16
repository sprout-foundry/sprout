/**
 * Runtime configuration provided by the server via GET /api/bootstrap.
 * Falls back to import.meta.env.VITE_* vars, then localhost defaults.
 */
import type { PlatformNavItem } from '../services/apiAdapter';

export interface RuntimeConfig {
  /** Base URL for API requests (e.g., "http://localhost:56000") */
  apiBaseURL: string;

  /** WebSocket URL for real-time updates */
  wsURL: string;

  /** Authentication mode: "none" (local) or "bearer" (cloud/token) */
  authMode: 'none' | 'bearer';

  /** Application mode: "local" (desktop/self-hosted) or "cloud" (managed) */
  appMode: 'local' | 'cloud';

  /** Version string embedded at build time */
  buildVersion: string;

  /** True when the server shares the CLI's agent (non-daemon interactive mode).
   * The frontend hides multi-chat UI and shows "coupled with terminal" messaging. */
  sharedMode?: boolean;

  /** Platform navigation items (tasks, billing, team, etc.) injected at runtime.
   * Falls back to CLOUD_NAV_ITEMS when the platform doesn't provide them. */
  navItems?: PlatformNavItem[];

  /** Authenticated user identity, injected by the platform in cloud mode.
   * Undefined when there is no session (local mode or unauthenticated). */
  user?: {
    id: string;
    email: string;
    tier: string;
  };
}
