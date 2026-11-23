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
// consulted, unless the active host declares the `chatSessions` capability,
// in which case they are forwarded to the host's store. The worktree-only
// sub-endpoints stay synthetic either way (synthetic.ts).

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

// --- Fly dev spaces & ETH-2 transactions ---
// /dev-space/fly is the LEGACY prefix: the live client (cloudTxn.ts) addresses
// the host-agnostic /dev-space/txn surface (SP-BUILDER-12) and only falls back
// to /dev-space/fly for in-flight sessions that resolved a dev space before
// that change. /dev-space/fly remains platform-only surface (never a local
// sprout route); both prefixes are relative so the CloudAdapter proxies them
// with session credentials.
const flyDevSpaceEndpoints: CloudEndpoint[] = [
  {
    path: '/dev-space/txn',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description:
      'Resolve/create the caller’s dev space for a repo, any backend (runner when attached, else Fly) — the escalation entry point',
  },
  {
    path: '/dev-space/txn/',
    methods: ['POST', 'GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Backend-agnostic txn lifecycle (open/status/push/run/pull/finish)',
  },
  {
    path: '/dev-space',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List the caller’s dev spaces (backend per row) — picks the dev space on the chosen escalation host',
  },
  {
    path: '/dev-space/',
    methods: ['GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Get one dev space row (carries runner_id) — confirms a runner-hosted escalation dev space',
  },
  {
    path: '/dev-space/fly',
    methods: ['GET', 'POST'],
    category: 'foundry-backend',
    description: 'Legacy: list/create Fly dev spaces (in-flight sessions only)',
  },
  {
    path: '/dev-space/fly/',
    methods: ['POST', 'GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Legacy: Fly dev space txn lifecycle (in-flight sessions only)',
  },
];

// --- Runners (SP-159) ---
const runnerEndpoints: CloudEndpoint[] = [
  {
    path: '/runners',
    methods: ['GET'],
    category: 'foundry-backend',
    description: "List the caller's runners — the escalation host picker",
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

// --- Dev space ---
// Intentionally empty: /api/workspace/symbols is not available in browser
// mode and returns a synthetic safe-default response (see synthetic.ts).
const devSpaceEndpoints: CloudEndpoint[] = [];

/**
 * All foundry-backend endpoints combined (non-git + git from separate module).
 */
export const foundryBackendEndpoints: CloudEndpoint[] = [
  ...chatAndQueryEndpoints,
  ...terminalEndpoints,
  ...gitEndpoints,
  ...diagnosticsEndpoints,
  ...historyEndpoints,
  ...sessionEndpoints,
  ...taskEndpoints,
  ...flyDevSpaceEndpoints,
  ...runnerEndpoints,
  ...settingsEndpoints,
  ...providerEndpoints,
  ...statsEndpoints,
  ...devSpaceEndpoints,
];
