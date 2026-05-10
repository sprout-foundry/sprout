import type { CloudEndpoint } from './types';
import { CLOUD_ENDPOINTS } from './endpoints';

/**
 * Extract the path without query parameters from a URL string.
 */
function stripQueryParameters(url: string): string {
  const queryIndex = url.indexOf('?');
  if (queryIndex === -1) {
    return url;
  }
  return url.substring(0, queryIndex);
}

/**
 * Check if a path matches an endpoint definition.
 *
 * @param path - The path to check (without query parameters)
 * @param endpoint - The endpoint definition
 * @returns true if the path matches
 */
function matchesEndpoint(path: string, endpoint: CloudEndpoint): boolean {
  if (endpoint.isPrefix) {
    return path.startsWith(endpoint.path);
  }
  return path === endpoint.path;
}

/**
 * Classify an API endpoint based on path and HTTP method.
 *
 * @param path - The API path (e.g., '/api/stats' or '/api/settings?layer=provenance')
 * @param method - HTTP method (e.g., 'GET', 'POST')
 * @returns The CloudEndpoint entry if found, null otherwise
 */
export function classifyEndpoint(path: string, method: string): CloudEndpoint | null {
  const cleanPath = stripQueryParameters(path);
  const normalizedMethod = method.toUpperCase();

  for (const endpoint of CLOUD_ENDPOINTS) {
    if (
      matchesEndpoint(cleanPath, endpoint) &&
      endpoint.methods.includes(normalizedMethod)
    ) {
      return endpoint;
    }
  }

  return null;
}
