import type { HostTransport } from '../../host';

/**
 * Whether git requests on this transport are served by the browser's own git
 * (isomorphic-git over the browser file store) rather than a backend. The
 * workspace installs the in-browser adapter, which answers every `/api/git/*`
 * call itself, for the hosted platform's bearer transport and for any host
 * that selects the in-browser agent backend.
 */
export function transportUsesBrowserGit(transport: HostTransport | null | undefined): boolean {
  return transport?.authMode === 'bearer' || transport?.agent?.kind === 'wasm';
}
