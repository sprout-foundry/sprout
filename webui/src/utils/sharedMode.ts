/**
 * Shared-agent mode helpers.
 *
 * In shared mode, the CLI REPL and WebUI share the same agent instance.
 * The frontend hides multi-chat UI and shows "coupled with terminal" messaging.
 */
import { getBootstrapConfig } from '../bootstrapAdapter';

/**
 * Returns true when the server is in shared-agent mode (CLI + WebUI coupled).
 * Reads from the cached bootstrap config. Safe to call at any time — returns
 * false before bootstrap completes (localhost default has sharedMode: false).
 */
export function isSharedMode(): boolean {
  return getBootstrapConfig().sharedMode === true;
}
