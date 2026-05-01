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
import { classifyEndpoint, getSyntheticResponse } from './cloudEndpointRegistry';
import { initWasmShell, type WasmShell } from './wasmShell';

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

/**
 * Shell-escape an argument for use in a command string.
 * Wraps in single quotes and escapes any embedded single quotes.
 */
function shellEscapeArg(arg: string): string {
  return `'${arg.replace(/'/g, "'\\''")}'`;
}

export class CloudAdapter implements APIAdapter {
  readonly name = 'foundry-cloud';
  readonly requiresBackendHealthCheck = true;
  readonly fileOpsViaAPI = false; // WASM handles files locally
  readonly showOnboarding = false; // Cloud is pre-configured
  readonly supportsSSH = false;
  readonly supportsInstances = true;
  readonly supportsLocalTerminal = false;
  readonly supportsSettings = false;
  readonly platformNavItems?: PlatformNavItem[];

  private config: CloudAdapterConfig;
  private wasmShell: WasmShell | null = null;
  private wasmInitPromise: Promise<WasmShell> | null = null;

  constructor(config: CloudAdapterConfig) {
    this.config = config;
    this.platformNavItems = config.navItems;
  }

  /**
   * Lazily initialize the WASM shell on first wasm-local request.
   * Returns a singleton promise so concurrent requests don't race.
   */
  private ensureWasmShell(): Promise<WasmShell> {
    if (this.wasmShell) return Promise.resolve(this.wasmShell);
    if (!this.wasmInitPromise) {
      this.wasmInitPromise = initWasmShell().then((shell) => {
        this.wasmShell = shell;
        return shell;
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

    // ── Stats endpoint translation ────────────────────────────────────
    // Rewrite /api/stats to /api/proxy/stats
    // Stats endpoints don't need body translation — only URL rewriting.
    if (urlPath === '/api/stats') {
      // When input is a Request object, pre-read the body for forwarding
      const requestBody = await this.extractRequestBody(input);
      return this.proxyStatsRequest(url, method, init, requestBody);
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

    // ── WASM-local endpoint handling ────────────────────────────────
    if (urlPath.startsWith('/api/')) {
      const endpoint = classifyEndpoint(urlPath, method);
      if (endpoint && endpoint.category === 'wasm-local') {
        const requestBody = await this.extractRequestBody(input);
        const bodyStr = this.extractBody(init) ?? requestBody ?? undefined;
        return this.handleWasmLocal(urlPath, method, url, bodyStr);
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

  /**
   * Proxy a stats request to the Foundry backend with URL path rewriting.
   * Stats endpoints don't need body translation — only URL rewriting.
   *
   * Example: /api/stats → /api/proxy/stats
   */
  private proxyStatsRequest(
    url: string, method: string, init?: RequestInit, requestBody?: string | null,
  ): Promise<Response> {
    let rewrittenPath: string;
    if (url.startsWith('/api/stats')) {
      rewrittenPath = url.replace('/api/stats', '/api/proxy/stats');
    } else {
      try {
        const parsed = new URL(url);
        const pathname = parsed.pathname.replace('/api/stats', '/api/proxy/stats');
        rewrittenPath = pathname + (parsed.search || '');
      } catch {
        rewrittenPath = url;
      }
    }
    return this.proxyToFoundry(rewrittenPath, method, init, requestBody);
  }

  // ──────────────────────────────────────────────────────────────────
  // WASM-local endpoint handling
  // ──────────────────────────────────────────────────────────────────

  /**
   * Handle wasm-local endpoints by routing them to the WASM shell.
   * These endpoints would normally go to the Go backend, but in cloud mode
   * the WASM shell owns the virtual filesystem.
   */
  private async handleWasmLocal(
    urlPath: string,
    method: string,
    fullUrl: string,
    bodyStr?: string,
  ): Promise<Response> {
    let shell: WasmShell;
    try {
      shell = await this.ensureWasmShell();
    } catch (err) {
      console.error('[CloudAdapter] WASM shell init failed:', err);
      return this.jsonError('WASM shell unavailable', 503);
    }

    try {
      switch (urlPath) {
        // ── File listing ──────────────────────────────────────────
        case '/api/files':
          return this.handleWasmFileList(shell);
        case '/api/browse':
        case '/api/workspace/browse':
          return this.handleWasmBrowse(shell, fullUrl);

        // ── File read/write ──────────────────────────────────────
        case '/api/file':
          return this.handleWasmFile(shell, method, fullUrl, bodyStr);

        // ── File CRUD ────────────────────────────────────────────
        case '/api/create':
          return this.handleWasmCreate(shell, bodyStr);
        case '/api/delete':
          return this.handleWasmDelete(shell, bodyStr);
        case '/api/rename':
          return this.handleWasmRename(shell, bodyStr);

        // ── Search ──────────────────────────────────────────────
        case '/api/search':
          return this.handleWasmSearch(shell, fullUrl);
        case '/api/search/replace':
          return this.handleWasmSearchReplace(shell, bodyStr);

        // ── File metadata ──────────────────────────────────────
        case '/api/file/check-modified':
          return this.handleWasmCheckModified(shell, bodyStr);
        case '/api/file/consent':
          // No consent flow needed in cloud/WASM mode
          return this.jsonOk({ token: 'wasm-local', path: '/', operation: 'read', expires_at: '' });
        case '/api/files/prettier-config':
          return this.jsonOk({ prettier: null });

        // ── Terminal stubs ──────────────────────────────────────
        case '/api/terminal/sessions':
          return this.jsonOk({ active_count: 0, count: 0 });
        case '/api/terminal/shells':
          return this.jsonOk({ shells: [{ name: 'wasm', path: '/bin/wasm', default: true }] });
        case '/api/terminal/history':
          if (method === 'POST') {
            // Accept but ignore — WASM terminal manages its own history
            return this.jsonOk({ success: true });
          }
          return this.jsonOk({ entries: [] });

        default:
          // Unrecognized wasm-local endpoint — return empty OK
          console.warn('[CloudAdapter] Unhandled wasm-local endpoint:', urlPath);
          return this.jsonOk({});
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      console.error(`[CloudAdapter] WASM handler error for ${urlPath}:`, err);
      return this.jsonError(message, 500);
    }
  }

  // ── Individual wasm-local route handlers ─────────────────────────

  /**
   * GET /api/files — Returns all files in the workspace.
   * The webui expects { message: string, files: Array<{path, modified}> }
   */
  private handleWasmFileList(shell: WasmShell): Response {
    const cwd = shell.getCwd();
    const result = shell.listDir(cwd);
    if (result.error) {
      return this.jsonError(result.error, 500);
    }
    // Build a flat recursive file list from the WASM directory tree.
    // The webui getFiles() expects { message, files: [{path, modified}] }
    const files = this.flattenEntries(shell, cwd);
    return this.jsonOk({ message: 'ok', files });
  }

  /**
   * Recursively flatten WASM directory entries into a flat file list.
   */
  private flattenEntries(shell: WasmShell, dir: string): Array<{ path: string; modified: boolean }> {
    const result: Array<{ path: string; modified: boolean }> = [];
    const listResult = shell.listDir(dir);
    if (listResult.error) return result;

    for (const entry of listResult.entries) {
      const fullPath = dir === '/' ? `/${entry.name}` : `${dir}/${entry.name}`;
      if (entry.type === 'dir') {
        result.push(...this.flattenEntries(shell, fullPath));
      } else {
        result.push({ path: fullPath, modified: false });
      }
    }
    return result;
  }

  /**
   * GET /api/browse?path=... — Browse a directory.
   * Expected: { files: [{name, path, type, size, modified}] }
   */
  private handleWasmBrowse(shell: WasmShell, fullUrl: string): Response {
    const path = this.getQueryParam(fullUrl, 'path') || '/';
    const safePath = this.sanitizePath(path);
    const result = shell.listDir(safePath);
    if (result.error) {
      return this.jsonError(result.error, 500);
    }
    const files = result.entries.map((entry) => ({
      name: entry.name,
      path: safePath === '/' ? `/${entry.name}` : `${safePath}/${entry.name}`,
      type: entry.type === 'dir' ? 'directory' : 'file',
      size: entry.size,
      modified: 0,
    }));
    return this.jsonOk({ files });
  }

  /**
   * GET /api/file?path=...  — Read file content.
   * POST /api/file?path=... — Write file content.
   */
  private handleWasmFile(shell: WasmShell, method: string, fullUrl: string, bodyStr?: string): Response {
    const path = this.getQueryParam(fullUrl, 'path');
    if (!path) {
      return this.jsonError('Missing path parameter', 400);
    }
    const safePath = this.sanitizePath(path);

    if (method === 'GET') {
      const result = shell.readFile(safePath);
      if (result.error) {
        return this.jsonError(result.error, 404);
      }
      // Return raw content with text content-type
      return new Response(result.content, {
        status: 200,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' },
      });
    }

    // POST — write
    if (!bodyStr) {
      return this.jsonError('Missing request body', 400);
    }
    let content: string;
    try {
      const parsed = JSON.parse(bodyStr);
      content = typeof parsed.content === 'string' ? parsed.content : bodyStr;
    } catch {
      content = bodyStr;
    }
    const err = shell.writeFile(safePath, content);
    if (err) {
      return this.jsonError(err, 500);
    }
    return this.jsonOk({ message: 'ok' });
  }

  /**
   * POST /api/create — Create a file or directory.
   * Body: { path, directory?: boolean } or { directory, path }
   */
  private handleWasmCreate(shell: WasmShell, bodyStr?: string): Response {
    if (!bodyStr) return this.jsonError('Missing request body', 400);
    const parsed = this.safeParseJson(bodyStr);
    const path = parsed?.path as string | undefined;
    const isDir = !!(parsed?.directory || parsed?.is_directory);
    if (!path) return this.jsonError('Missing path', 400);

    const safePath = this.sanitizePath(path);

    if (isDir) {
      const result = shell.executeCommand(`mkdir -p ${shellEscapeArg(safePath)}`);
      if (result.exitCode !== 0) {
        return this.jsonError(result.stderr || 'mkdir failed', 500);
      }
    } else {
      // Create an empty file
      const err = shell.writeFile(safePath, '');
      if (err) {
        return this.jsonError(err, 500);
      }
    }
    return this.jsonOk({ message: 'ok', path: safePath });
  }

  /**
   * DELETE /api/delete — Delete a file.
   * Body: { path }
   */
  private handleWasmDelete(shell: WasmShell, bodyStr?: string): Response {
    if (!bodyStr) return this.jsonError('Missing request body', 400);
    const parsed = this.safeParseJson(bodyStr);
    const path = parsed?.path as string | undefined;
    if (!path) return this.jsonError('Missing path', 400);

    const safePath = this.sanitizePath(path);
    const err = shell.deleteFile(safePath);
    if (err) {
      // If file delete fails, try rm via command (handles directories)
      const result = shell.executeCommand(`rm -rf ${shellEscapeArg(safePath)}`);
      if (result.exitCode !== 0) {
        return this.jsonError(result.stderr || err, 500);
      }
    }
    return this.jsonOk({ message: 'ok', path: safePath });
  }

  /**
   * POST /api/rename — Rename a file or directory.
   * Body: { old_path, new_path }
   */
  private handleWasmRename(shell: WasmShell, bodyStr?: string): Response {
    if (!bodyStr) return this.jsonError('Missing request body', 400);
    const parsed = this.safeParseJson(bodyStr);
    const oldPath = parsed?.old_path as string | undefined;
    const newPath = parsed?.new_path as string | undefined;
    if (!oldPath || !newPath) return this.jsonError('Missing old_path or new_path', 400);

    const safeOld = this.sanitizePath(oldPath);
    const safeNew = this.sanitizePath(newPath);
    const result = shell.executeCommand(`mv ${shellEscapeArg(safeOld)} ${shellEscapeArg(safeNew)}`);
    if (result.exitCode !== 0) {
      return this.jsonError(result.stderr || 'rename failed', 500);
    }
    return this.jsonOk({ message: 'ok', old_path: safeOld, new_path: safeNew });
  }

  /**
   * GET /api/search?query=...&case_sensitive=...&regex=...&include=...
   * Uses WASM shell grep command.
   */
  private handleWasmSearch(shell: WasmShell, fullUrl: string): Response {
    const query = this.getQueryParam(fullUrl, 'query') || '';
    const caseSensitive = this.getQueryParam(fullUrl, 'case_sensitive') === 'true';
    const regex = this.getQueryParam(fullUrl, 'regex') === 'true';
    const include = this.getQueryParam(fullUrl, 'include') || '';

    if (!query) return this.jsonOk({ results: [], total_matches: 0, total_files: 0, truncated: false, query: '' });

    const cwd = shell.getCwd();
    // Build grep command
    let grepCmd = 'grep';
    if (!caseSensitive) grepCmd += ' -i';
    if (regex) grepCmd += ' -E';
    grepCmd += ` -rn --include=${shellEscapeArg(include || '*')} ${shellEscapeArg(query)} ${shellEscapeArg(cwd)}`;

    const result = shell.executeCommand(grepCmd);
    // grep returns exit code 1 when no matches — that's not an error
    if (result.exitCode !== 0 && result.exitCode !== 1) {
      return this.jsonError(result.stderr || 'search failed', 500);
    }

    // Parse grep output into structured results
    const results = this.parseGrepOutput(result.stdout);
    const totalMatches = results.reduce((sum, r) => sum + r.match_count, 0);
    return this.jsonOk({
      results,
      total_matches: totalMatches,
      total_files: results.length,
      truncated: false,
      query,
    });
  }

  /**
   * POST /api/search/replace — Search and replace across files.
   * Body: { search, replace, files, case_sensitive?, whole_word?, regex?, preview }
   */
  private handleWasmSearchReplace(shell: WasmShell, bodyStr?: string): Response {
    if (!bodyStr) return this.jsonError('Missing request body', 400);
    const parsed = this.safeParseJson(bodyStr);
    const search = parsed?.search as string | undefined;
    const replace = parsed?.replace as string | undefined;
    const files = parsed?.files as string[] | undefined;
    const preview = !!parsed?.preview;

    if (!search || !replace || !files?.length) {
      return this.jsonError('Missing search, replace, or files', 400);
    }

    const changes: Array<{
      file: string;
      matches: Array<{ line_number: number; old_line: string; new_line: string; column_start: number; column_end: number }>;
      changed_lines: number;
    }> = [];

    for (const filePath of files) {
      const safePath = this.sanitizePath(filePath);
      const readResult = shell.readFile(safePath);
      if (readResult.error) continue;

      const content = readResult.content;
      const lines = content.split('\n');
      const matches: Array<{ line_number: number; old_line: string; new_line: string; column_start: number; column_end: number }> = [];
      let changedLines = 0;

      for (let i = 0; i < lines.length; i++) {
        const idx = lines[i].indexOf(search);
        if (idx !== -1) {
          const oldLine = lines[i];
          const newLine = lines[i].replace(search, replace);
          if (oldLine !== newLine) {
            matches.push({
              line_number: i + 1,
              old_line: oldLine,
              new_line: newLine,
              column_start: idx,
              column_end: idx + search.length,
            });
            if (!preview) {
              lines[i] = newLine;
            }
            changedLines++;
          }
        }
      }

      if (matches.length > 0) {
        if (!preview) {
          const err = shell.writeFile(safePath, lines.join('\n'));
          if (err) {
            console.warn(`[CloudAdapter] searchReplace: failed to write ${safePath}:`, err);
          }
        }
        changes.push({ file: safePath, matches, changed_lines: changedLines });
      }
    }

    const totalChanges = changes.reduce((sum, c) => sum + c.changed_lines, 0);
    return this.jsonOk({ changes, total_changes: totalChanges, preview });
  }

  /**
   * POST /api/file/check-modified — Check if files have been modified.
   * In WASM mode, files are only modified through the WASM shell, so always
   * return empty (no external modifications detected).
   */
  private handleWasmCheckModified(_shell: WasmShell, bodyStr?: string): Response {
    // Parse the request to acknowledge it, but always return no modifications
    // since WASM files can only change through the shell itself.
    void bodyStr;
    return this.jsonOk({ modified: [] });
  }

  // ── WASM helper utilities ────────────────────────────────────────

  /**
   * Parse grep -rn output into structured search results.
   * Format: "filename:linenum:matched line text"
   */
  private parseGrepOutput(output: string): Array<{
    file: string;
    matches: Array<{ line_number: number; line: string; column_start: number; column_end: number; context_before: string[]; context_after: string[] }>;
    match_count: number;
  }> {
    const fileMap = new Map<string, Array<{ line_number: number; line: string; column_start: number; column_end: number; context_before: string[]; context_after: string[] }>>();

    for (const line of output.split('\n')) {
      if (!line) continue;
      // Parse "path:linenum:content" or "path:linenum:content"
      const firstColon = line.indexOf(':');
      if (firstColon === -1) continue;
      const secondColon = line.indexOf(':', firstColon + 1);
      if (secondColon === -1) continue;

      const file = line.substring(0, firstColon);
      const lineNum = parseInt(line.substring(firstColon + 1, secondColon), 10);
      const content = line.substring(secondColon + 1);

      if (isNaN(lineNum)) continue;

      let matches = fileMap.get(file);
      if (!matches) {
        matches = [];
        fileMap.set(file, matches);
      }
      matches.push({
        line_number: lineNum,
        line: content,
        column_start: 0,
        column_end: content.length,
        context_before: [],
        context_after: [],
      });
    }

    return Array.from(fileMap.entries()).map(([file, matches]) => ({
      file,
      matches,
      match_count: matches.length,
    }));
  }

  /**
   * Normalize and validate a file path for WASM operations.
   * Rejects path traversal attempts.
   */
  private sanitizePath(path: string): string {
    // Remove null bytes
    let clean = path.replace(/\0/g, '');
    // Normalize slashes
    clean = clean.replace(/\\/g, '/');
    // Collapse multiple slashes
    clean = clean.replace(/\/+/g, '/');
    // Remove trailing slash (unless root)
    if (clean.length > 1 && clean.endsWith('/')) {
      clean = clean.slice(0, -1);
    }
    // Reject path traversal
    const parts = clean.split('/');
    const resolved: string[] = [];
    for (const part of parts) {
      if (part === '..') {
        if (resolved.length > 0) resolved.pop();
        // Allow traversal at root — just clamp
      } else if (part !== '.' && part !== '') {
        resolved.push(part);
      }
    }
    return '/' + resolved.join('/');
  }

  /**
   * Extract a query parameter value from a URL string.
   */
  private getQueryParam(url: string, name: string): string | null {
    try {
      let search: string;
      if (url.startsWith('/')) {
        const qIdx = url.indexOf('?');
        search = qIdx === -1 ? '' : url.substring(qIdx);
      } else {
        search = new URL(url).search;
      }
      const params = new URLSearchParams(search);
      return params.get(name);
    } catch {
      return null;
    }
  }

  /**
   * Safely parse JSON, returning null on failure.
   */
  private safeParseJson(str: string): Record<string, unknown> | null {
    try {
      return JSON.parse(str);
    } catch {
      return null;
    }
  }

  /** Create a JSON success response. */
  private jsonOk(data: unknown): Response {
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  /** Create a JSON error response. */
  private jsonError(message: string, status: number): Response {
    return new Response(JSON.stringify({ error: message, message }), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
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
