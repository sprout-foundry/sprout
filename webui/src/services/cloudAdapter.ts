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
    // NOTE: Chat endpoint mapping takes priority over the synthetic response
    // registry. No chat-mapped path should be added to the synthetic registry.
    // The webui sends POST /api/query with { query, chat_id }.
    // Foundry expects POST /api/proxy/chat with { provider, model, messages, stream }.
    const foundryPath = CHAT_ENDPOINT_MAP[urlPath];
    if (foundryPath) {
      // When input is a Request object, pre-read the body for translation
      const requestBodyText = await this.extractRequestBody(input);
      return this.translateAndProxyChat(urlPath, foundryPath, method, init, requestBodyText);
    }

    // ── Git endpoint translation ────────────────────────────────────
    // Rewrite /api/git/* paths to /api/proxy/git/*
    // Git endpoints don't need body translation — only URL rewriting.
    if (urlPath.startsWith('/api/git/')) {
      // When input is a Request object, pre-read the body for forwarding
      const requestBody = await this.extractRequestBody(input);
      return this.proxyGitRequest(url, method, init, requestBody);
    }

    // ── Settings endpoint translation ───────────────────────────────
    // Rewrite /api/settings and /api/settings/* paths to /api/proxy/settings/*
    // Settings endpoints don't need body translation — only URL rewriting.
    if (urlPath === '/api/settings' || urlPath.startsWith('/api/settings/')) {
      // When input is a Request object, pre-read the body for forwarding
      const requestBody = await this.extractRequestBody(input);
      return this.proxySettingsRequest(url, method, init, requestBody);
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

    // Extract body from Request object if init doesn't have one
    let body: BodyInit | null | undefined = init?.body;
    if (body == null) {
      body = await this.extractRequestBody(input);
    }

    return fetch(rewrittenUrl, {
      ...init,
      body: body ?? undefined,
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
    requestBodyText?: string | null,
  ): Promise<Response> {
    const targetUrl = `${this.config.apiBase}${foundryPath}`;
    const headers = new Headers(init?.headers);
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
    headers.set('Content-Type', 'application/json');

    let body: string | undefined;

    if (TRANSLATE_BODY_PATHS.has(webuiPath) && method === 'POST') {
      // Parse the webui request body and translate to Foundry format
      // Try init body first, fall back to pre-read Request body
      const raw = this.extractBody(init) ?? requestBodyText ?? null;
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
      body = this.extractBody(init) ?? requestBodyText ?? undefined;
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

    // Empty/missing query is intentionally passed through — the Foundry backend validates.

    // Build the Foundry-compatible request body
    const translated: Record<string, unknown> = {
      messages: [{ role: 'user', content: query }],
      stream: true,
    };

    // Warn if we're overwriting an existing messages field
    if (parsed.messages) {
      console.warn('[CloudAdapter] Overwriting existing messages field in chat request body');
    }

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

  /**
   * Extract the body text from a Request object by cloning it.
   * Returns null if input is not a Request, body is empty, or clone fails.
   */
  private async extractRequestBody(input: RequestInfo | URL): Promise<string | null> {
    if (typeof input === 'string' || input instanceof URL) {
      return null;
    }
    try {
      const cloned = input.clone();
      return await cloned.text();
    } catch {
      // Body may already be consumed or not readable
      return null;
    }
  }

  /**
   * Proxy a request to the Foundry backend with a pre-rewritten path.
   * Handles target path extraction (relative or absolute), header injection,
   * and the actual fetch() call with credentials.
   */
  private proxyToFoundry(
    rewrittenPath: string, method: string, init?: RequestInit, requestBody?: string | null,
  ): Promise<Response> {
    let targetPath: string;
    if (rewrittenPath.startsWith('/')) {
      targetPath = rewrittenPath;
    } else {
      try {
        const parsed = new URL(rewrittenPath);
        targetPath = parsed.pathname;
        if (parsed.search) targetPath += parsed.search;
      } catch {
        targetPath = rewrittenPath;
      }
    }
    const targetUrl = `${this.config.apiBase}${targetPath}`;
    const headers = new Headers(init?.headers);
    headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
    return fetch(targetUrl, {
      ...init,
      method,
      body: init?.body ?? requestBody ?? undefined,
      headers,
      credentials: 'include',
    });
  }

  /**
   * Proxy a git request to the Foundry backend with URL path rewriting.
   * Git endpoints don't need body translation — only URL rewriting.
   *
   * Example: /api/git/status → /api/proxy/git/status
   */
  private proxyGitRequest(
    url: string, method: string, init?: RequestInit, requestBody?: string | null,
  ): Promise<Response> {
    const rewrittenPath = url.replace('/api/git/', '/api/proxy/git/');
    return this.proxyToFoundry(rewrittenPath, method, init, requestBody);
  }

  /**
   * Proxy a settings request to the Foundry backend with URL path rewriting.
   * Settings endpoints don't need body translation — only URL rewriting.
   *
   * Example: /api/settings → /api/proxy/settings
   *          /api/settings/credentials → /api/proxy/settings/credentials
   */
  private proxySettingsRequest(
    url: string, method: string, init?: RequestInit, requestBody?: string | null,
  ): Promise<Response> {
    let rewrittenPath: string;
    if (url.startsWith('/api/settings')) {
      rewrittenPath = url.replace('/api/settings', '/api/proxy/settings');
    } else {
      try {
        const parsed = new URL(url);
        const pathname = parsed.pathname.replace('/api/settings', '/api/proxy/settings');
        rewrittenPath = pathname + (parsed.search || '');
      } catch {
        rewrittenPath = url;
      }
    }
    return this.proxyToFoundry(rewrittenPath, method, init, requestBody);
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
