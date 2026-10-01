import type { CloudEndpoint } from '../types';
import { gitEndpoints } from './foundry-backend-git';

/**
 * Category (b) — foundry-backend: Must be proxied to the Foundry backend.
 */
// --- Chat & Query ---
const chatAndQueryEndpoints: CloudEndpoint[] = [
  // /api/query is intentionally NOT here — it routes through the WASM shell's
  // in-browser agent loop (see cloudEndpointRegistry/endpoints/wasm-local.ts).
  // /api/query/stop and /api/query/steer are also wasm-local — they interact
  // with the in-browser agent directly rather than going through the backend.
  // Only /api/query/status needs the platform backend.
  {
    path: '/api/query/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Query execution status',
  },
];

// --- Embedding & Semantic Search ---
// Intentionally empty: these endpoints are not available in browser mode and
// return synthetic safe-default responses (see synthetic.ts). The earlier
// foundry-backend duplicates were removed because they caused 401/404 errors
// in cloud mode and triggered error toasts in the UI.
const embeddingEndpoints: CloudEndpoint[] = [];

// --- Agent Terminal Sessions ---
const terminalEndpoints: CloudEndpoint[] = [
  {
    path: '/api/terminal/agent-sessions',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List background agent terminal sessions (needs backend)',
  },
  {
    path: '/api/terminal/agent-sessions/',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Agent session actions (output, attach, kill) — needs backend',
  },
];

// --- Diagnostics & LSP ---
// Intentionally empty: these endpoints are not available in browser mode and
// return synthetic safe-default responses (see synthetic.ts).
const diagnosticsEndpoints: CloudEndpoint[] = [];

// --- Chat Sessions ---
// Not listed: the chat list is browser-local in cloud mode (each chat owns an
// in-page agent and a localStorage transcript). CloudAdapter serves
// /api/chat-sessions* from cloudChatSessions before this registry is
// consulted; the worktree-only sub-endpoints stay synthetic (synthetic.ts).

// --- History ---
// Intentionally empty: these endpoints are not available in browser mode and
// return synthetic safe-default responses (see synthetic.ts).
const historyEndpoints: CloudEndpoint[] = [];

// --- Sessions ---
// Intentionally empty: these endpoints are not available in browser mode and
// return synthetic safe-default responses (see synthetic.ts).
const sessionEndpoints: CloudEndpoint[] = [];

// --- Tasks ---
const taskEndpoints: CloudEndpoint[] = [
  {
    path: '/api/tasks',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description: 'List/create user tasks (webui compatibility)',
  },
  {
    path: '/api/tasks/',
    methods: ['GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Get task status/details by id',
  },
];

// --- Fly workspaces & ETH-2 transactions ---
// /workspace/fly is platform-only surface (never a local sprout route), used
// by the escalation paths: list/create workspaces and the txn lifecycle the
// "Run in cloud container" action drives (push deltas → run → pull deltas →
// finish). Relative URLs so the CloudAdapter proxies them with session
// credentials.
const flyWorkspaceEndpoints: CloudEndpoint[] = [
  {
    path: '/workspace/fly',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description: 'List/create Fly workspaces (escalation + txn workspace resolve)',
  },
  {
    path: '/workspace/fly/',
    methods: ['POST', 'GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Fly workspace txn lifecycle (open/status/push/run/pull/finish)',
  },
];

// --- Settings & Configuration ---
// The worktree/availability-flag subagent-types, MCP-related, skills, and
// hotkey endpoints are intercepted as synthetic in browser mode (see
// synthetic.ts). The core user settings, credentials, and provider CRUD
// operations remain foundry-backend so the platform owns them.
const settingsEndpoints: CloudEndpoint[] = [
  {
    path: '/api/settings',
    methods: ['GET', 'PUT'],
    category: 'foundry-backend',
    description: 'User settings (Foundry manages)',
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
];

// --- Providers ---
const providerEndpoints: CloudEndpoint[] = [
  {
    path: '/api/providers',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List available providers',
  },
];

// --- Stats ---
const statsEndpoints: CloudEndpoint[] = [
  {
    path: '/api/stats',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Execution stats',
  },
];

// --- Workspace ---
// Intentionally empty: /api/workspace/symbols is not available in browser
// mode and returns a synthetic safe-default response (see synthetic.ts).
const workspaceEndpoints: CloudEndpoint[] = [];

/**
 * All foundry-backend endpoints combined (non-git + git from separate module).
 */
export const foundryBackendEndpoints: CloudEndpoint[] = [
  ...chatAndQueryEndpoints,
  ...embeddingEndpoints,
  ...terminalEndpoints,
  ...gitEndpoints,
  ...diagnosticsEndpoints,
  ...historyEndpoints,
  ...sessionEndpoints,
  ...taskEndpoints,
  ...flyWorkspaceEndpoints,
  ...settingsEndpoints,
  ...providerEndpoints,
  ...statsEndpoints,
  ...workspaceEndpoints,
];
