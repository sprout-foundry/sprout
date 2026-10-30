/**
 * The host's agent backend, as the module-level services read it.
 *
 * A host states where the agent runs through `transport.agent`. Two questions
 * are asked of that declaration throughout the services layer, and they must
 * be answered the same way everywhere:
 *
 * - Does the agent run in this page (the in-browser WASM agent)? A daemon
 *   backend says no, whatever the transport's auth mode is: the hosted
 *   platform's transport is `bearer` and its workspace proxy is where the
 *   daemon lives, so auth mode alone would wrongly call it in-browser.
 * - Which URLs do the calls belong to? A daemon backend's own `apiBaseURL` /
 *   `wsURL` are the backend. The transport's own URLs name the host around it
 *   (the platform), which does not serve the workspace's `/api/query` or its
 *   event stream.
 *
 * Reading the contract here keeps the page bootstrap, the workspace
 * composition and the run-status poll from disagreeing about which backend
 * answers a turn.
 */
import type { SproutHost } from './types';

export interface BackendURLs {
  /** Base for relative API paths. `''` is the same-origin sentinel. */
  apiBaseURL: string;
  /** The agent event stream's URL. `''` is the same-origin sentinel. */
  wsURL: string;
}

/**
 * Whether this host's agent runs in the browser.
 *
 * An explicit backend is authoritative: `daemon` runs the agent, so nothing in
 * the page answers a turn; `wasm` runs in the page. With no explicit backend,
 * the hosted build's bearer transport keeps its in-browser default.
 */
export function hostRunsInBrowserAgent(host: SproutHost | null | undefined): boolean {
  const agent = host?.transport.agent;
  if (agent) return agent.kind === 'wasm';
  return host?.transport.authMode === 'bearer';
}

/**
 * The URLs the active backend's calls belong to: the daemon's own when the
 * host named one, else the transport's. `''` stays the same-origin sentinel
 * for both fields (a daemon the host reaches at its own origin).
 */
export function backendURLs(host: SproutHost | null | undefined): BackendURLs {
  const transport = host?.transport;
  const agent = transport?.agent;
  if (agent?.kind === 'daemon') {
    return { apiBaseURL: agent.apiBaseURL ?? '', wsURL: agent.wsURL ?? '' };
  }
  return { apiBaseURL: transport?.apiBaseURL ?? '', wsURL: transport?.wsURL ?? '' };
}
