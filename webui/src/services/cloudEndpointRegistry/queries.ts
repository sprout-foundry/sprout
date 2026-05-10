import type { CloudEndpoint, EndpointCategory } from './types';
import { classifyEndpoint } from './classify';
import { CLOUD_ENDPOINTS } from './endpoints';

/**
 * Get all endpoints by category.
 *
 * @param category - The category to filter by
 * @returns Array of endpoints in the category
 */
export function getEndpointsByCategory(category: EndpointCategory): CloudEndpoint[] {
  return CLOUD_ENDPOINTS.filter((e) => e.category === category);
}

/**
 * Check if an endpoint should be handled locally by WASM.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns true if WASM-local, false otherwise
 */
export function isWasmLocalEndpoint(path: string, method: string): boolean {
  const endpoint = classifyEndpoint(path, method);
  return endpoint?.category === 'wasm-local' || false;
}

/**
 * Check if an endpoint should be proxied to the Foundry backend.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns true if needs Foundry backend, false otherwise
 */
export function isFoundryBackendEndpoint(path: string, method: string): boolean {
  const endpoint = classifyEndpoint(path, method);
  return endpoint?.category === 'foundry-backend' || false;
}
