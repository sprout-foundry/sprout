/**
 * bootstrapAdapter.ts — Install cloud adapter before component tree loads.
 *
 * Must be the first import in index.tsx so that config/mode.ts feature flags
 * read the correct adapter state when they are first evaluated.
 */

import { installAdapter } from './services/apiAdapter';
import { CloudAdapter } from './services/cloudAdapter';

if (process.env.REACT_APP_SPROUT_MODE === 'cloud') {
  const apiBase = process.env.REACT_APP_FOUNDRY_API_URL || window.location.origin;
  const wsUrl = process.env.REACT_APP_FOUNDRY_WS_URL ||
    `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`;

  installAdapter(new CloudAdapter({ apiBase, wsUrl }));
}
