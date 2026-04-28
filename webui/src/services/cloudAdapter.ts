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
import { WEBUI_CLIENT_ID_HEADER, getWebUIClientId } from './clientSession';
import { getSyntheticResponse } from './cloudEndpointRegistry';

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
  readonly supportsSSH = true;
  readonly supportsInstances = true;
  readonly supportsLocalTerminal = false;
  readonly supportsSettings = false;
  readonly platformNavItems?: PlatformNavItem[];

  private config: CloudAdapterConfig;

  constructor(config: CloudAdapterConfig) {
    this.config = config;
    this.platformNavItems = config.navItems;
  }

  async fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    let url: string;
    let method: string = 'GET';

    // Extract URL and method from the input
    if (typeof input === 'string') {
      url = input;
    } else if (input instanceof URL) {
      url = input.toString();
    } else {
      // Request object
      url = input.url;
      method = input.method || 'GET';
    }

    // Get method from init if not in Request object
    method = (init?.method || method).toUpperCase();

    // Extract the pathname for synthetic response matching.
    // For absolute URLs (e.g. from URL or Request objects), pull out just the
    // path so that synthetic interception works regardless of how the caller
    // constructed the input.
    const urlPath = this.extractPath(url);

    // Check for synthetic response interception BEFORE URL rewriting
    if (urlPath.startsWith('/api/')) {
      const synthetic = getSyntheticResponse(urlPath, method);
      if (synthetic) {
        return synthetic;
      }
    }

    // Rewrite URL to Foundry backend if it's a relative path
    if (typeof input === 'string') {
      url = input.startsWith('/') ? `${this.config.apiBase}${input}` : input;
    } else if (input instanceof URL) {
      url = input.toString();
    } else {
      // Request object - need to handle carefully
      url = input.url.startsWith('/') ? `${this.config.apiBase}${input.url}` : input.url;
    }

    const headers = new Headers(init?.headers);
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());

    return fetch(url, {
      ...init,
      headers,
      credentials: 'include', // Send cookies for auth
    });
  }

  /**
   * Extract the pathname from a URL string.
   * For relative URLs (e.g. '/api/stats?foo=bar'), returns the path as-is.
   * For absolute URLs (e.g. 'https://api.sprout.dev/api/stats'), returns '/api/stats'.
   */
  private extractPath(url: string): string {
    if (url.startsWith('/')) {
      return url;
    }
    try {
      const parsed = new URL(url);
      return parsed.pathname + parsed.search;
    } catch {
      // Not a valid URL — return as-is and let matching fail gracefully
      return url;
    }
  }

  getWebSocketURL(): string | null {
    return this.config.wsUrl;
  }
}
