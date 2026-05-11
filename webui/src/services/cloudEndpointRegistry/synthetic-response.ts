import { classifyEndpoint } from './classify';

/**
 * Get a synthetic Response object for an endpoint if it should be intercepted.
 *
 * @param path - The API path
 * @param method - HTTP method
 * @returns A Response object if synthetic, null otherwise
 */
export function getSyntheticResponse(path: string, method: string): Response | null {
  const endpoint = classifyEndpoint(path, method);

  if (!endpoint || (endpoint.category !== 'synthetic' && endpoint.category !== 'no-op')) {
    return null;
  }

  // Determine status code based on response shape
  const hasError =
    endpoint.syntheticResponse &&
    typeof endpoint.syntheticResponse === 'object' &&
    'error' in endpoint.syntheticResponse;

  const status = hasError ? 400 : 200;

  const body = endpoint.syntheticResponse != null ? JSON.stringify(endpoint.syntheticResponse) : '{}';

  return new Response(body, {
    status,
    headers: {
      'Content-Type': 'application/json',
    },
  });
}
