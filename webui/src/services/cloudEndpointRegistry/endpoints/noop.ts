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
  // Pin/unpin succeed silently in cloud mode: the chat list is browser-local
  // (see cloudChatSessions, which also serves delete-all) and has no pins.
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
];
