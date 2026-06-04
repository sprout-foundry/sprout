import type { CloudEndpoint } from '../types';
import { gitEndpoints } from './foundry-backend-git';

/**
 * Category (b) — foundry-backend: Must be proxied to the Foundry backend.
 */
// --- Chat & Query ---
const chatAndQueryEndpoints: CloudEndpoint[] = [
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
];

// --- Embedding & Semantic Search ---
const embeddingEndpoints: CloudEndpoint[] = [
  {
    path: '/api/embedding-index',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Embedding index status (needs backend vector DB)',
  },
  {
    path: '/api/providers/models',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List available models for providers',
  },
  {
    path: '/api/search/semantic/status',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Semantic search index status (needs backend)',
  },
  {
    path: '/api/search/semantic/build',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Build semantic search index (needs backend)',
  },
  {
    path: '/api/search/semantic/preview',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Preview semantic search index (needs backend)',
  },
  {
    path: '/api/search/semantic',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Semantic/vector search (needs backend AI infrastructure)',
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
const diagnosticsEndpoints: CloudEndpoint[] = [
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
];

// --- Chat Sessions ---
const chatSessionEndpoints: CloudEndpoint[] = [
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
];

// --- History ---
const historyEndpoints: CloudEndpoint[] = [
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
];

// --- Sessions ---
const sessionEndpoints: CloudEndpoint[] = [
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
];

// --- Tasks ---
const taskEndpoints: CloudEndpoint[] = [
  {
    path: '/api/tasks',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'List user tasks (webui compatibility)',
  },
  {
    path: '/api/tasks',
    methods: ['POST'],
    category: 'foundry-backend',
    description: 'Create a new agent task (webui compatibility)',
  },
];

// --- Settings & Configuration ---
const settingsEndpoints: CloudEndpoint[] = [
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
    methods: ['GET'],
    category: 'foundry-backend',
    isPrefix: true,
    description: 'Subagent type read-only access (catalog is fixed; PUT/DELETE removed)',
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
];

// --- Costs ---
const costEndpoints: CloudEndpoint[] = [
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
const workspaceEndpoints: CloudEndpoint[] = [
  {
    path: '/api/workspace/symbols',
    methods: ['GET'],
    category: 'foundry-backend',
    description: 'Workspace symbols (LSP)',
  },
];

/**
 * All foundry-backend endpoints combined (non-git + git from separate module).
 */
export const foundryBackendEndpoints: CloudEndpoint[] = [
  ...chatAndQueryEndpoints,
  ...embeddingEndpoints,
  ...terminalEndpoints,
  ...gitEndpoints,
  ...diagnosticsEndpoints,
  ...chatSessionEndpoints,
  ...historyEndpoints,
  ...sessionEndpoints,
  ...taskEndpoints,
  ...settingsEndpoints,
  ...costEndpoints,
  ...providerEndpoints,
  ...statsEndpoints,
  ...workspaceEndpoints,
];
