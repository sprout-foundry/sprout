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

export type { EndpointCategory, CloudEndpoint } from './types';
export { CLOUD_ENDPOINTS } from './endpoints';
export { classifyEndpoint } from './classify';
export { getSyntheticResponse } from './synthetic-response';
export { getEndpointsByCategory, isWasmLocalEndpoint, isFoundryBackendEndpoint } from './queries';
