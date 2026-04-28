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

/**
 * Mapping of webui chat endpoints to their Foundry proxy equivalents.
 * The webui sends { query, chat_id } while Foundry expects
 * { provider, model, messages, stream }.
 */
const CHAT_ENDPOINT_MAP: Record<string, string> = {
  '/api/query': '/api/proxy/chat',
  '/api/query/steer': '/api/proxy/chat',
  '/api/query/stop': '/api/proxy/chat/stop',
  '/api/query/status': '/api/proxy/chat/status',
};

/** Paths that require request body translation (query → messages format). */
const TRANSLATE_BODY_PATHS = new Set(['/api/query', '/api/query/steer']);

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

    // Extract the pathname for matching (strip query params for lookup).
    const urlPath = this.extractPathname(url);

    // ── Chat endpoint translation ──────────────────────────────────
    // The webui sends POST /api/query with { query, chat_id }.
    // Foundry expects POST /api/proxy/chat with { provider, model, messages, stream }.
    const foundryPath = CHAT_ENDPOINT_MAP[urlPath];
    if (foundryPath) {
      return this.translateAndProxyChat(urlPath, foundryPath, method, init);
    }

    // ── Synthetic response interception ────────────────────────────
    if (urlPath.startsWith('/api/')) {
      const synthetic = getSyntheticResponse(urlPath, method);
      if (synthetic) {
        return synthetic;
      }
    }

    // ── Standard Foundry backend proxy ─────────────────────────────
    const rewrittenUrl = this.rewriteUrl(url);

    const headers = new Headers(init?.headers);
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());

    return fetch(rewrittenUrl, {
      ...init,
      headers,
      credentials: 'include',
    });
  }

  // ──────────────────────────────────────────────────────────────────
  // Chat endpoint translation
  // ──────────────────────────────────────────────────────────────────

  /**
   * Translate a webui chat request to the Foundry proxy/chat format and
   * forward it to the Foundry backend.
   */
  private async translateAndProxyChat(
    webuiPath: string,
    foundryPath: string,
    method: string,
    init?: RequestInit,
  ): Promise<Response> {
    const targetUrl = `${this.config.apiBase}${foundryPath}`;
    const headers = new Headers(init?.headers);
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
    headers.set('Content-Type', 'application/json');

    let body: string | undefined;

    if (TRANSLATE_BODY_PATHS.has(webuiPath) && method === 'POST') {
      // Parse the webui request body and translate to Foundry format
      const raw = this.extractBody(init);
      if (raw) {
        try {
          const parsed: Record<string, unknown> = JSON.parse(raw);
          const translated = this.translateRequestBody(webuiPath, parsed);
          body = JSON.stringify(translated);
        } catch {
          // If body is not valid JSON, pass through as-is
          body = raw;
        }
      }
    } else {
      // For stop/status, forward any existing body unchanged
      body = this.extractBody(init) || undefined;
    }

    return fetch(targetUrl, {
      ...init,
      method,
      headers,
      body,
      credentials: 'include',
    });
  }

  /**
   * Translate a webui chat request body to the Foundry proxy/chat format.
   *
   * Webui sends: { query, chat_id?, provider?, model?, workspace_root?, system_prompt? }
   * Foundry expects: { provider?, model?, messages, stream, chat_id?, steer?, workspace_root?, system_prompt? }
   */
  private translateRequestBody(
    webuiPath: string,
    parsed: Record<string, unknown>,
  ): Record<string, unknown> {
    const query = typeof parsed.query === 'string' ? parsed.query : '';
    const isSteer = webuiPath === '/api/query/steer';

    // Build the Foundry-compatible request body
    const translated: Record<string, unknown> = {
      messages: [{ role: 'user', content: query }],
      stream: true,
    };

    // Pass through optional fields if present
    if (parsed.provider) translated.provider = parsed.provider;
    if (parsed.model) translated.model = parsed.model;
    if (parsed.chat_id) translated.chat_id = parsed.chat_id;
    if (parsed.workspace_root) translated.workspace_root = parsed.workspace_root;
    if (parsed.system_prompt) translated.system_prompt = parsed.system_prompt;
    if (isSteer) translated.steer = true;

    return translated;
  }

  /**
   * Extract the body string from a RequestInit object.
   */
  private extractBody(init?: RequestInit): string | null {
    if (!init?.body) return null;
    if (typeof init.body === 'string') return init.body;
    // ReadableStream or other body types — not supported for translation
    console.warn('[CloudAdapter] Non-string body cannot be translated for chat endpoint');
    return null;
  }

  // ──────────────────────────────────────────────────────────────────
  // URL helpers
  // ──────────────────────────────────────────────────────────────────

  /**
   * Extract the pathname from a URL string.
   * For relative URLs (e.g. '/api/stats?foo=bar'), returns '/api/stats'.
   * For absolute URLs (e.g. 'https://api.sprout.dev/api/stats'), returns '/api/stats'.
   */
  private extractPathname(url: string): string {
    if (url.startsWith('/')) {
      // Strip query parameters
      const qIdx = url.indexOf('?');
      return qIdx === -1 ? url : url.substring(0, qIdx);
    }
    try {
      return new URL(url).pathname;
    } catch {
      return url;
    }
  }

  /**
   * Rewrite a relative URL to the Foundry backend base URL.
   * Absolute URLs are returned as-is.
   */
  private rewriteUrl(url: string): string {
    if (url.startsWith('/')) {
      return `${this.config.apiBase}${url}`;
    }
    return url;
  }

  getWebSocketURL(): string | null {
    return this.config.wsUrl;
  }
}
