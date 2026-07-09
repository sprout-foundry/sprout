import type { CloudEndpoint } from '../types';

/**
 * Category (d) — no-op: Endpoints that are not applicable in cloud mode.
 * They silently return a success response to avoid breaking callers.
 */
export const noOpEndpoints: CloudEndpoint[] = [
  {
    path: '/api/open-in-file-browser',
    methods: ['POST'],
    category: 'no-op',
    syntheticResponse: { success: true },
    description: 'Open in OS file browser (not applicable in cloud mode)',
  },
  // Pin/unpin/delete-all succeed silently in cloud mode because sessions
  // are managed client-side (no platform-backed session list exists).
  // Returning 200/ok here means callers that delete-all then re-list see
  // a consistent (empty) result without error toasts.
  {
    path: '/api/chat-sessions/pin',
    methods: ['POST'],
    category: 'no-op',
    syntheticResponse: { message: 'ok' },
    description: 'Pin chat session (no-op in cloud mode)',
  },
  {
    path: '/api/chat-sessions/unpin',
    methods: ['POST'],
    category: 'no-op',
    syntheticResponse: { message: 'ok' },
    description: 'Unpin chat session (no-op in cloud mode)',
  },
  {
    path: '/api/chat-sessions/delete-all',
    methods: ['POST'],
    category: 'no-op',
    syntheticResponse: { message: 'ok', deleted: 0 },
    description: 'Delete all chat sessions (no-op in cloud mode)',
  },
];
