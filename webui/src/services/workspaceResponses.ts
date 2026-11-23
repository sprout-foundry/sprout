/**
 * Channel for responses to the project folder's own API requests.
 *
 * clientFetch and useSproutFetch publish every response they receive here,
 * so features that react to request outcomes (such as the escalation
 * triggers) can observe the project folder's traffic without wrapping the global
 * fetch, which belongs to the page and, in an embed, to the host.
 */

export type ProjectFolderResponseListener = (url: string, response: Response, init?: RequestInit) => void;

const listeners = new Set<ProjectFolderResponseListener>();

export function requestURL(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

/** Subscribe to project folder responses. Returns an unsubscribe function. */
export function onProjectFolderResponse(listener: ProjectFolderResponseListener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Publish a project folder response. Listener errors never affect the request. */
export function publishProjectFolderResponse(input: RequestInfo | URL, response: Response, init?: RequestInit): void {
  if (listeners.size === 0) return;
  const url = requestURL(input);
  for (const listener of listeners) {
    try {
      listener(url, response, init);
    } catch {
      // A misbehaving observer must not break the caller's request.
    }
  }
}
