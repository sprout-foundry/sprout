/**
 * CloudEndpointRegistry — classification and synthetic response registry for cloud mode.
 *
 * In cloud mode, the webui routes API calls through CloudAdapter. This registry
 * defines which endpoints are handled locally by WASM, which need the Foundry
 * backend, and which should return synthetic/empty responses.
 *
 * This is the single source of truth for ALL webui API endpoints and their
 * cloud-mode behavior.
 */

/**
 * Endpoint classification categories for cloud mode.
 */
export type EndpointCategory = 'wasm-local' | 'foundry-backend' | 'synthetic' | 'no-op';

/**
 * Cloud endpoint metadata.
 */
export interface CloudEndpoint {
  /** The API path pattern (exact match or prefix with trailing /) */
  path: string;
  /** HTTP methods this endpoint handles (e.g., ['GET', 'POST']) */
  methods: string[];
  /** Classification category */
  category: EndpointCategory;
  /** For synthetic endpoints, the response body to return */
  syntheticResponse?: unknown;
  /** Description of what this endpoint does */
  description: string;
  /** If true, path is a prefix and matches any path starting with this string */
  isPrefix?: boolean;
}

/**
 * All webui API endpoints with their cloud-mode classification.
 *
 * Category (a) — wasm-local: Handled client-side by the WASM filesystem/shell.
 * The CloudAdapter should NOT intercept these - they fall through to WASM handlers.
 *
 * Category (b) — foundry-backend: Must be proxied to the Foundry backend.
 *
 * Category (c) — synthetic: Should return pre-defined synthetic responses.
 *
 * Category (d) — no-op: Endpoints that are not applicable in cloud mode.
 * They silently return a success response to avoid breaking callers.
 */
export const CLOUD_ENDPOINTS: CloudEndpoint[] = [
  // ==================== CATEGORY (a): WASM-LOCAL ====================
  {
    path: '/api/files',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'File listing (WASM handles locally)',
  },
  {
    path: '/api/create',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'File creation (WASM handles locally)',
  },
  {
    path: '/api/delete',
    methods: ['DELETE'],
    category: 'wasm-local',
    description: 'File deletion (WASM handles locally)',
  },
  {
    path: '/api/rename',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'File rename (WASM handles locally)',
  },
  {
    path: '/api/browse',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Directory browsing (WASM handles locally)',
  },
  {
    path: '/api/file/check-modified',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'Check if file modified (WASM handles locally)',
  },
  {
    path: '/api/file/consent',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'File consent (no security prompts in cloud mode)',
  },
  {
    path: '/api/terminal/sessions',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Terminal sessions (WASM terminal manages)',
  },
  {
    path: '/api/terminal/shells',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Available shells (WASM terminal)',
  },
  {
    path: '/api/terminal/history',
    methods: ['GET', 'POST'],
    category: 'wasm-local',
    description: 'Terminal history (WASM terminal)',
  },
  {
    path: '/api/search/replace',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'Search and replace in files (WASM handles)',
  },
  {
    path: '/api/file',
    methods: ['GET', 'POST'],
    category: 'wasm-local',
    description: 'Read/write file content (WASM handles locally)',
  },
  {
    path: '/api/files/prettier-config',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Prettier config for a file (WASM filesystem)',
  },
  {
    path: '/api/workspace/browse',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Workspace directory browsing (WASM filesystem)',
  },
  {
    path: '/api/search',
    methods: ['GET'],
    category: 'wasm-local',
    description: 'Search files (WASM handles locally)',
  },

  // ==================== CATEGORY (b): FOUNDRY-BACKEND ====================
  {
    path: '/api/query',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Main chat/query endpoint (needs Foundry proxy)',
  },
  {
    path: '/api/query/steer',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Steer agent mid-conversation',
  },
  {
    path: '/api/query/stop',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Stop agent execution',
  },
  {
    path: '/api/query/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Query execution status',
  },
  {
    path: '/api/upload/image',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Image upload for vision',
  },
  {
    path: '/api/git/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git status',
  },
  {
    path: '/api/git/branches',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List git branches',
  },
  {
    path: '/api/git/checkout',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Checkout branch/commit',
  },
  {
    path: '/api/git/branch/create',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create branch',
  },
  {
    path: '/api/git/pull',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git pull',
  },
  {
    path: '/api/git/push',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git push',
  },
  {
    path: '/api/git/stage',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Stage files',
  },
  {
    path: '/api/git/unstage',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Unstage files',
  },
  {
    path: '/api/git/discard',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Discard changes',
  },
  {
    path: '/api/git/stage-all',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Stage all files',
  },
  {
    path: '/api/git/unstage-all',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Unstage all files',
  },
  {
    path: '/api/git/commit',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Git commit',
  },
  {
    path: '/api/git/commit-message',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Generate commit message',
  },
  {
    path: '/api/git/revert',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Revert commit',
  },
  {
    path: '/api/git/deep-review',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Deep review',
  },
  {
    path: '/api/git/deep-review/fix',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Fix review items',
  },
  {
    path: '/api/git/deep-review/fix/start',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Start fix process',
  },
  {
    path: '/api/git/deep-review/fix/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Fix process status',
  },
  {
    path: '/api/git/diff',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git diff',
  },
  {
    path: '/api/git/log',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Git log',
  },
  {
    path: '/api/git/confirm',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Confirm git commit',
  },
  {
    path: '/api/git/commit/show',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Show commit details',
  },
  {
    path: '/api/git/commit/show/file',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Show file diff in commit',
  },
  {
    path: '/api/git/worktrees',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List worktrees',
  },
  {
    path: '/api/git/worktree/create',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create worktree',
  },
  {
    path: '/api/git/worktree/remove',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Remove worktree',
  },
  {
    path: '/api/git/worktree/checkout',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Checkout worktree',
  },
  {
    path: '/api/diagnostics',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Language diagnostics',
  },
  {
    path: '/api/semantic',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Semantic operations (go-to-def, hover, references, rename, completion)',
  },
  {
    path: '/api/lsp/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'LSP server status',
  },
  {
    path: '/api/lsp/ws',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'LSP WebSocket bridge',
  },
  {
    path: '/api/chat-sessions',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description: 'Chat session management',
  },
  {
    path: '/api/chat-sessions/create',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create chat session',
  },
  {
    path: '/api/chat-sessions/delete',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Delete chat session',
  },
  {
    path: '/api/chat-sessions/delete-all',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Delete all chat sessions',
  },
  {
    path: '/api/chat-sessions/rename',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Rename chat session',
  },
  {
    path: '/api/chat-sessions/switch',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Switch chat session',
  },
  {
    path: '/api/chat-sessions/create-in-worktree',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create session in worktree',
  },
  {
    path: '/api/chat-sessions/compact',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Compact chat session',
  },
  {
    path: '/api/chat-sessions/pin',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Pin chat session',
  },
  {
    path: '/api/chat-sessions/unpin',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Unpin chat session',
  },
  {
    path: '/api/chat-sessions/worktree-mappings',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List worktree mappings',
  },
  {
    path: '/api/chat-session/',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Worktree sub-endpoints',
  },
  {
    path: '/api/history/changelog',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'History changelog',
  },
  {
    path: '/api/history/revision',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'History revision',
  },
  {
    path: '/api/history/changes',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'History changes',
  },
  {
    path: '/api/history/rollback',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Rollback conversation history',
  },
  {
    path: '/api/sessions/restore',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Restore session state',
  },
  {
    path: '/api/sessions',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List sessions',
  },
  {
    path: '/api/settings',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'User settings (Foundry manages)',
  },
  {
    path: '/api/settings/mcp',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'MCP settings',
  },
  {
    path: '/api/settings/mcp/servers/',
    methods: ['GET', 'POST', 'PUT', 'DELETE'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'MCP server CRUD',
  },
  {
    path: '/api/settings/credentials',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Get credentials (Foundry manages)',
  },
  {
    path: '/api/settings/credentials/',
    methods: ['GET', 'PUT', 'DELETE', 'POST'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Credential CRUD (includes pool and test sub-paths)',
  },
  {
    path: '/api/settings/providers',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'Provider settings',
  },
  {
    path: '/api/settings/providers/',
    methods: ['GET', 'PUT', 'DELETE'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Provider CRUD',
  },
  {
    path: '/api/settings/skills',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'Skill settings',
  },
  {
    path: '/api/settings/subagent-types',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Subagent type configs',
  },
  {
    path: '/api/settings/subagent-types/',
    methods: ['GET', 'PUT', 'DELETE'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Subagent type CRUD',
  },
  {
    path: '/api/hotkeys',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'Hotkey configuration',
  },
  {
    path: '/api/hotkeys/validate',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Validate hotkey binding',
  },
  {
    path: '/api/hotkeys/preset',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Apply hotkey preset',
  },
  {
    path: '/api/costs/summary',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Cost summary',
  },
  {
    path: '/api/costs/history',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Cost history',
  },
  {
    path: '/api/costs/detail',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Cost detail',
  },
  {
    path: '/api/providers',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List available providers',
  },
  {
    path: '/api/stats',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Execution stats',
  },
  {
    path: '/api/workspace',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description: 'Workspace info',
  },
  {
    path: '/api/workspace/symbols',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Workspace symbols (LSP)',
  },

  // ==================== CATEGORY (c): SYNTHETIC ====================
  {
    path: '/api/onboarding/status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { setup_required: false },
    description: 'Onboarding status (cloud is pre-configured)',
  },
  {
    path: '/api/onboarding/complete',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Complete onboarding',
  },
  {
    path: '/api/onboarding/skip',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Skip onboarding',
  },
  {
    path: '/api/instances',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: {
      instances: [],
      current_pid: 0,
      active_host_pid: 0,
      active_host_port: 0,
      desired_host_pid: 0,
    },
    description: 'List instances (no local instances in cloud mode)',
  },
  {
    path: '/api/instances/ssh-hosts',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { hosts: [] },
    description: 'List SSH hosts (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-open',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { error: 'Not available in cloud mode' },
    description: 'Open SSH workspace (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-sessions',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { sessions: [] },
    description: 'List SSH sessions (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-browse',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { error: 'Not available in cloud mode' },
    description: 'Browse SSH directory (not available in cloud mode)',
  },
  {
    path: '/api/instances/ssh-close',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Close SSH session',
  },
  {
    path: '/api/instances/select',
    methods: ['POST'],
    category: 'synthetic',
    syntheticResponse: { success: true },
    description: 'Select instance',
  },
  {
    path: '/api/instances/ssh-launch-status',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { ready: false, status: 'not_available' },
    description: 'SSH launch status',
  },
  {
    path: '/api/support-bundle',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: { message: 'Not available in cloud mode' },
    description: 'Support bundle (not available in cloud mode)',
  },
  {
    path: '/api/config',
    methods: ['GET'],
    category: 'synthetic',
    syntheticResponse: {},
    description: 'App config (empty in cloud mode)',
  },

  // ==================== CATEGORY (d): NO-OP ====================
  // Endpoints that are not applicable in cloud mode. They should
  // silently return a success response to avoid breaking callers.
  {
    path: '/api/open-in-file-browser',
    methods: ['POST'],
    category: 'no-op',
    syntheticResponse: { success: true },
    description: 'Open in OS file browser (not applicable in cloud mode)',
  },
];

/**
 * Extract the path without query parameters from a URL string.
 */
function stripQueryParameters(url: string): string {
  const queryIndex = url.indexOf('?');
  if (queryIndex === -1) {
    return url;
  }
  return url.substring(0, queryIndex);
}

/**
 * Check if a path matches an endpoint definition.
 *
 * @param path - The path to check (without query parameters)
 * @param endpoint - The endpoint definition
 * @returns true if the path matches
 */
function matchesEndpoint(path: string, endpoint: CloudEndpoint): boolean {
  if (endpoint.isPrefix) {
    return path.startsWith(endpoint.path);
  }
  return path === endpoint.path;
}

/**
 * Classify an API endpoint based on path and HTTP method.
 *
 * @param path - The API path (e.g., '/api/stats' or '/api/settings?layer=provenance')
 * @param method - HTTP method (e.g., 'GET', 'POST')
 * @returns The CloudEndpoint entry if found, null otherwise
 */
export function classifyEndpoint(path: string, method: string): CloudEndpoint | null {
  const cleanPath = stripQueryParameters(path);
  const normalizedMethod = method.toUpperCase();

  for (const endpoint of CLOUD_ENDPOINTS) {
    if (
      matchesEndpoint(cleanPath, endpoint) &&
      endpoint.methods.includes(normalizedMethod)
    ) {
      return endpoint;
    }
  }

  return null;
}

/**
 * Get a synthetic Response object for an endpoint if it should be intercepted.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns A Response object if synthetic, null otherwise
 */
export function getSyntheticResponse(path: string, method: string): Response | null {
  const endpoint = classifyEndpoint(path, method);

  if (!endpoint || (endpoint.category !== 'synthetic' && endpoint.category !== 'no-op')) {
    return null;
  }

  // Determine status code based on response shape
  const hasError = endpoint.syntheticResponse &&
    typeof endpoint.syntheticResponse === 'object' &&
    'error' in endpoint.syntheticResponse;

  const status = hasError ? 400 : 200;

  const body = endpoint.syntheticResponse != null
    ? JSON.stringify(endpoint.syntheticResponse)
    : '{}';

  return new Response(body, {
    status,
    headers: {
      'Content-Type': 'application/json',
    },
  });
}

/**
 * Get all endpoints by category.
 *
 * @param category - The category to filter by
 * @returns Array of endpoints in the category
 */
export function getEndpointsByCategory(category: EndpointCategory): CloudEndpoint[] {
  return CLOUD_ENDPOINTS.filter((e) => e.category === category);
}

/**
 * Check if an endpoint should be handled locally by WASM.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns true if WASM-local, false otherwise
 */
export function isWasmLocalEndpoint(path: string, method: string): boolean {
  const endpoint = classifyEndpoint(path, method);
  return endpoint?.category === 'wasm-local' || false;
}

/**
 * Check if an endpoint should be proxied to the Foundry backend.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns true if needs Foundry backend, false otherwise
 */
export function isFoundryBackendEndpoint(path: string, method: string): boolean {
  const endpoint = classifyEndpoint(path, method);
  return endpoint?.category === 'foundry-backend' || false;
}
