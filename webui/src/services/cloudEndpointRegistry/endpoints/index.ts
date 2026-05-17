import type { CloudEndpoint } from '../types';
import { foundryBackendEndpoints } from './foundry-backend';
import { noOpEndpoints } from './noop';
import { syntheticEndpoints } from './synthetic';
import { wasmLocalEndpoints } from './wasm-local';

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
  ...wasmLocalEndpoints,
  ...foundryBackendEndpoints,
  ...syntheticEndpoints,
  ...noOpEndpoints,
];
