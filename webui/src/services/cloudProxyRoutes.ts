/**
 * Proxy route handlers for CloudAdapter.
 *
 * Contains the route-specific proxy handlers that translate and forward
 * requests to the Foundry backend. Each handler deals with a specific
 * endpoint category (chat, git, stats, settings) with its own URL
 * rewriting and body translation rules.
 */

/**
 * Mapping of webui chat endpoints to their Foundry proxy equivalents.
 * The webui sends { query, chat_id } while Foundry expects
 * { provider, model, messages, stream }.
 *
 * Note: The platform hosts chat at /proxy/chat (not /api/proxy/chat).
 */
export const CHAT_ENDPOINT_MAP: Record<string, string> = {
  // /api/query, /api/query/stop, and /api/query/steer are NOT here — they
  // route through the WASM shell's in-browser agent loop (see wasm-local.ts).
  // The platform proxy only handles status.
  '/api/query/status': '/proxy/chat/status',
};

/** Paths that require request body translation (query -> messages format). */
export const TRANSLATE_BODY_PATHS = new Set(['/api/query']);

/**
 * Proxy a request to the Foundry backend with a pre-rewritten path.
 * Handles target path extraction (relative or absolute), header injection,
 * and the actual fetch() call with credentials.
 */
function proxyToFoundry(
  apiBase: string,
  rewrittenPath: string,
  method: string,
  clientIdHeader: string,
  clientId: string,
  init?: RequestInit,
  requestBody?: string | null,
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
  const targetUrl = `${apiBase}${targetPath}`;
  const headers = new Headers(init?.headers);
  headers.set(clientIdHeader, clientId);
  return fetch(targetUrl, {
    ...init,
    method,
    body: init?.body ?? requestBody ?? undefined,
    headers,
    credentials: 'include',
  });
}

/**
 * Translate a webui chat request to the Foundry proxy/chat format and
 * forward it to the Foundry backend.
 */
export function translateAndProxyChat(
  apiBase: string,
  webuiPath: string,
  foundryPath: string,
  method: string,
  clientIdHeader: string,
  clientId: string,
  init?: RequestInit,
  requestBodyText?: string | null,
  extractBodyFn?: (init?: RequestInit) => string | null,
): Promise<Response> {
  const targetUrl = `${apiBase}${foundryPath}`;
  const headers = new Headers(init?.headers);
  headers.set(clientIdHeader, clientId);
  headers.set('Content-Type', 'application/json');

  let body: string | undefined;

  if (TRANSLATE_BODY_PATHS.has(webuiPath) && method === 'POST') {
    // Parse the webui request body and translate to Foundry format
    const raw = extractBodyFn
      ? (extractBodyFn(init) ?? requestBodyText)
      : ((init?.body as string | null) ?? requestBodyText ?? null);
    if (raw) {
      try {
        const parsed: Record<string, unknown> = JSON.parse(raw);
        const translated = translateRequestBody(webuiPath, parsed);
        body = JSON.stringify(translated);
      } catch {
        // If body is not valid JSON, pass through as-is
        body = raw;
      }
    }
  } else {
    // For stop/status, forward any existing body unchanged
    body = (init?.body as string | undefined) ?? requestBodyText ?? undefined;
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
export function translateRequestBody(webuiPath: string, parsed: Record<string, unknown>): Record<string, unknown> {
  const query = typeof parsed.query === 'string' ? parsed.query : '';
  const isSteer = webuiPath === '/api/query/steer';

  // Empty/missing query is intentionally passed through — the Foundry backend validates.

  // Build the Foundry-compatible request body
  const translated: Record<string, unknown> = {
    messages: [{ role: 'user', content: query }],
    stream: true,
  };

  // In cloud mode, default to platform-managed LLM (routes to the
  // platform's local inference server) unless the user has configured
  // their own provider key (BYOK), in which case the provider field
  // is already set by the chat input.
  if (!parsed.provider) {
    translated.provider = 'platform';
  }

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
 * Proxy a git request to the Foundry backend with URL path rewriting.
 * Git endpoints don't need body translation — only URL rewriting.
 *
 * Example: /api/git/status → /api/proxy/git/status
 */
export function proxyGitRequest(
  apiBase: string,
  url: string,
  method: string,
  clientIdHeader: string,
  clientId: string,
  init?: RequestInit,
  requestBody?: string | null,
): Promise<Response> {
  const rewrittenPath = url.replace('/api/git/', '/api/proxy/git/');
  return proxyToFoundry(apiBase, rewrittenPath, method, clientIdHeader, clientId, init, requestBody);
}

/**
 * Proxy a settings request to the Foundry backend with URL path rewriting.
 * Settings endpoints don't need body translation — only URL rewriting.
 *
 * Example: /api/settings → /api/proxy/settings
 *          /api/settings/credentials → /api/proxy/settings/credentials
 */
export function proxySettingsRequest(
  apiBase: string,
  url: string,
  method: string,
  clientIdHeader: string,
  clientId: string,
  init?: RequestInit,
  requestBody?: string | null,
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
  return proxyToFoundry(apiBase, rewrittenPath, method, clientIdHeader, clientId, init, requestBody);
}

/**
 * Proxy a stats request to the Foundry backend with URL path rewriting.
 * Stats endpoints don't need body translation — only URL rewriting.
 *
 * Example: /api/stats → /api/proxy/stats
 */
export function proxyStatsRequest(
  apiBase: string,
  url: string,
  method: string,
  clientIdHeader: string,
  clientId: string,
  init?: RequestInit,
  requestBody?: string | null,
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
  return proxyToFoundry(apiBase, rewrittenPath, method, clientIdHeader, clientId, init, requestBody);
}
