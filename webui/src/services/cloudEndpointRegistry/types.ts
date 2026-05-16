/**
 * Type definitions for the cloud endpoint registry.
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
