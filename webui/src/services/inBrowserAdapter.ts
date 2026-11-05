/**
 * The in-browser adapter: the CloudAdapter that serves the browser-local file
 * ops and the in-browser (WASM) agent query.
 *
 * Two independent installers exist — the page bootstrap and a composed
 * workspace's backend wiring — so either can be the live singleton when a host
 * swaps in a running workspace's daemon backend. "Retire it" is therefore one
 * operation keyed on the adapter's type (the CloudAdapter), not on who
 * installed it: an adapter of another kind belongs to the host and stays.
 */

import { getAdapter, uninstallAdapter } from './apiAdapter';

/**
 * Uninstall the in-browser adapter, if one is live. Resolves once the check is
 * done; a no-op when no adapter is installed or the installed one is not the
 * CloudAdapter.
 *
 * The type check imports the adapter module lazily, so a local-mode build
 * never pulls the cloud adapter in.
 */
export async function retireInBrowserAdapter(): Promise<void> {
  if (getAdapter() === null) return;
  const { CloudAdapter } = await import('./cloudAdapter');
  // Re-read after the import yields: another installer may have replaced the
  // singleton while this retire was waiting for the module.
  const live = getAdapter();
  if (live !== null && live instanceof CloudAdapter) uninstallAdapter(live);
}
