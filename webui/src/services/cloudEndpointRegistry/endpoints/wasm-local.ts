import type { CloudEndpoint } from '../types';

/**
 * Category (a) — wasm-local: Handled client-side by the WASM filesystem/shell.
 * The CloudAdapter should NOT intercept these - they fall through to WASM handlers.
 */
export const wasmLocalEndpoints: CloudEndpoint[] = [
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
    methods: ['DELETE', 'POST'],
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
    methods: ['GET', 'POST'],
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
  {
    path: '/api/query',
    methods: ['POST'],
    category: 'wasm-local',
    description: 'Agent query (WASM shell runs the full agent loop in-browser)',
  },
];
