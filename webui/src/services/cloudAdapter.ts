/**
 * CloudAdapter — routes sprout webui API calls to the Foundry platform backend.
 *
 * In cloud mode, the webui is served from Foundry (or Cloudflare Pages) and
 * talks to the Foundry Go API server for chat, git, credentials, etc.
 * File operations are handled client-side by the WASM shell.
 *
 * This adapter is installed at app startup when VITE_SPROUT_MODE=cloud.
 *
 * Route handlers are split into focused modules:
 *   cloudProxyRoutes.ts  — Backend proxy handlers (chat, git, stats, settings)
 *   cloudWasmHandlers.ts — WASM-local file operation handlers
 */

import type { APIAdapter, PlatformNavItem } from './apiAdapter';
import { WEBUI_CLIENT_ID_HEADER, getWebUIClientId } from './clientSession';
import { getSyntheticResponse, isWasmLocalEndpoint } from './cloudEndpointRegistry';
import {
  CHAT_ENDPOINT_MAP,
  translateAndProxyChat,
  proxyGitRequest,
  proxyStatsRequest,
  proxySettingsRequest,
} from './cloudProxyRoutes';
import { handleWasmLocal } from './cloudWasmHandlers';
import { initWasmShell, type WasmShell } from './wasmShell';

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
  readonly supportsSSH = false;
  readonly supportsGit = false;
  readonly supportsChat = true;
  readonly supportsWorkspaceSwitching = false;
  readonly supportsExport = false;
  readonly supportsInstances = true;
  readonly supportsLocalTerminal = false;
  readonly supportsSettings = true;
  readonly platformNavItems?: PlatformNavItem[];

  private config: CloudAdapterConfig;
  private wasmShell: WasmShell | null = null;
  private wasmInitPromise: Promise<WasmShell> | null = null;
  private wasmInitFailed: boolean = false;

  constructor(config: CloudAdapterConfig) {
    this.config = config;
    this.platformNavItems = config.navItems;
  }

  /**
   * Eagerly preload the WASM shell before any wasm-local request.
   * Called from useAppInitialization on mount in cloud mode so the
   * shell is ready when file/terminal/search requests fire.  The
   * promise resolves to true on success, false on failure (failure
   * is cached so subsequent ensureWasmShell calls short-circuit).
   */
  preloadWasmShell(): Promise<boolean> {
    console.log('[CloudAdapter] preloadWasmShell called');
    return this.ensureWasmShell()
      .then(() => true)
      .catch((err) => {
        console.warn('[CloudAdapter] WASM shell preload failed:', err);
        return false;
      });
  }

  /**
   * Lazily initialize the WASM shell on first wasm-local request.
   * Returns a singleton promise so concurrent requests don't race.
   * On failure, caches the failure so future calls short-circuit (avoiding
   * infinite retries in browsers that permanently lack WASM support).
   */
  private ensureWasmShell(): Promise<WasmShell> {
    if (this.wasmShell) return Promise.resolve(this.wasmShell);
    if (this.wasmInitFailed) {
      return Promise.reject(new Error('WASM shell init previously failed; not retrying'));
    }
    if (!this.wasmInitPromise) {
      this.wasmInitPromise = initWasmShell()
        .then((shell) => {
          this.wasmShell = shell;
          return shell;
        })
        .catch((err) => {
          // Cache the failure so future calls don't retry indefinitely.
          // This is important for browsers that permanently lack WASM support
          // (e.g., Safari on some iOS versions, or users with JS sandboxing).
          this.wasmInitFailed = true;
          this.wasmInitPromise = null;
          throw err;
        });
    }
    return this.wasmInitPromise;
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

    // Client identity for all proxied requests
    const clientIdHeader = WEBUI_CLIENT_ID_HEADER;
    const clientIdValue = getWebUIClientId();

    // ── Chat endpoint translation (steer, stop, status) ───────────
    // NOTE: Chat endpoint mapping takes priority over the synthetic response
    // registry. No chat-mapped path should be added to the synthetic registry.
    // /api/query POST is handled as wasm-local via the registry below — the
    // WASM shell runs the full agent loop in-browser; steering/stop/status
    // remain proxied because they need platform chat state.
    const foundryPath = CHAT_ENDPOINT_MAP[urlPath];
    if (foundryPath) {
      // When input is a Request object, pre-read the body for translation
      const requestBodyText = await this.extractRequestBody(input);
      return translateAndProxyChat(
        this.config.apiBase,
        urlPath,
        foundryPath,
        method,
        clientIdHeader,
        clientIdValue,
        init,
        requestBodyText,
        (i) => this.extractBody(i),
      );
    }

    // ── Git endpoint translation ────────────────────────────────────
    // Rewrite /api/git/* paths to /api/proxy/git/*
    if (urlPath.startsWith('/api/git/')) {
      const requestBody = await this.extractRequestBody(input);
      return proxyGitRequest(this.config.apiBase, url, method, clientIdHeader, clientIdValue, init, requestBody);
    }

    // ── Stats endpoint translation ────────────────────────────────────
    // Rewrite /api/stats to /api/proxy/stats
    if (urlPath === '/api/stats') {
      const requestBody = await this.extractRequestBody(input);
      return proxyStatsRequest(this.config.apiBase, url, method, clientIdHeader, clientIdValue, init, requestBody);
    }

    // ── Settings endpoint translation ───────────────────────────────
    // Only proxy CORE settings (user prefs, credentials, providers) to the
    // platform backend. Subagent-types, MCP, skills, hotkeys are intercepted
    // as synthetic below — those endpoints are not available in browser mode
    // and returning a safe default is better than triggering a 401/404 error
    // toast from the platform backend.
    const isProxiedSettings =
      urlPath === '/api/settings' ||
      urlPath.startsWith('/api/settings/credentials') ||
      urlPath.startsWith('/api/settings/providers');
    if (isProxiedSettings) {
      const requestBody = await this.extractRequestBody(input);
      return proxySettingsRequest(this.config.apiBase, url, method, clientIdHeader, clientIdValue, init, requestBody);
    }

    // ── Synthetic response interception ────────────────────────────
    if (urlPath.startsWith('/api/')) {
      const synthetic = getSyntheticResponse(urlPath, method);
      if (synthetic) {
        return synthetic;
      }
    }

    // ── WASM-local endpoint handling ────────────────────────────────
    // These endpoints (file CRUD, terminal, search) MUST be handled by the
    // WASM shell in the browser — NOT proxied to the Foundry backend.
    // If WASM shell init fails, fall through to the standard proxy below
    // so the server's safety-net handler returns a compatible response.
    if (isWasmLocalEndpoint(urlPath, method)) {
      const requestBody = await this.extractRequestBody(input);
      const bodyStr = this.extractBody(init) ?? requestBody ?? undefined;
      try {
        const shell = await this.ensureWasmShell();
        return handleWasmLocal(shell, urlPath, method, url, bodyStr);
      } catch (err) {
        console.warn(
          `[CloudAdapter] WASM shell unavailable for wasm-local endpoint "${urlPath}", falling through to server safety-net:`,
          err,
        );
        // Fall through to standard proxy below so the server's safety-net handler
        // returns a compatible response. This avoids a hard 503 error.
      }
    }

    // ── Standard Foundry backend proxy ─────────────────────────────
    const rewrittenUrl = this.rewriteUrl(url);

    const headers = new Headers(init?.headers);
    headers.set(clientIdHeader, clientIdValue);

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
