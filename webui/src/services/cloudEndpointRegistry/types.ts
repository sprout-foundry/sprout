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

/**
 * Endpoint classification categories for cloud mode.
 */
export type EndpointCategory = 'wasm-local' | 'foundry-backend' | 'synthetic' | 'no-op';

/**
 * Cloud endpoint metadata.
 */
export interface CloudEndpoint {
  /** The API path pattern (exact match or prefix with trailing /) */
  path: string;
  /** HTTP methods this endpoint handles (e.g., ['GET', 'POST']) */
  methods: string[];
  /** Classification category */
  category: EndpointCategory;
  /** For synthetic endpoints, the response body to return */
  syntheticResponse?: unknown;
  /** Description of what this endpoint does */
  description: string;
  /** If true, path is a prefix and matches any path starting with this string */
  isPrefix?: boolean;
}
