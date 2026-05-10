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
];
