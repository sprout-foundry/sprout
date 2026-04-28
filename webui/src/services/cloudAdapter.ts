/**
 * CloudAdapter — routes sprout webui API calls to the Foundry platform backend.
 *
 * In cloud mode, the webui is served from Foundry (or Cloudflare Pages) and
 * talks to the Foundry Go API server for chat, git, credentials, etc.
 * File operations are handled client-side by the WASM shell.
 *
 * This adapter is installed at app startup when REACT_APP_SPROUT_MODE=cloud.
 */

import type { APIAdapter, PlatformNavItem } from './apiAdapter';

export interface CloudAdapterConfig {
  /** Base URL for the Foundry API (e.g., 'https://api.sprout.dev') */
  apiBase: string;
  /** WebSocket URL for real-time events (e.g., 'wss://api.sprout.dev/ws') */
  wsUrl: string;
  /** Platform nav items (tasks, billing, etc.) injected at runtime */
  navItems?: PlatformNavItem[];
}

export class CloudAdapter implements APIAdapter {
  readonly name = 'foundry-cloud';
  readonly requiresBackendHealthCheck = true;
  readonly fileOpsViaAPI = false; // WASM handles files locally
  readonly showOnboarding = false; // Cloud is pre-configured
  readonly platformNavItems?: PlatformNavItem[];

  private config: CloudAdapterConfig;

  constructor(config: CloudAdapterConfig) {
    this.config = config;
    this.platformNavItems = config.navItems;
  }

  async fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    let url: string;
    if (typeof input === 'string') {
      url = input.startsWith('/') ? `${this.config.apiBase}${input}` : input;
    } else if (input instanceof URL) {
      url = input.toString();
    } else {
      // Request object
      url = input.url.startsWith('/') ? `${this.config.apiBase}${input.url}` : input.url;
    }

    return fetch(url, {
      ...init,
      credentials: 'include', // Send cookies for auth
    });
  }

  getWebSocketURL(): string | null {
    return this.config.wsUrl;
  }
}
