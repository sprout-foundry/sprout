/**
 * The host's agent backend, wired for a composed workspace.
 *
 * A host states where the agent runs through its transport's `agent` field:
 * a daemon reachable through the host (the preferred backend — full tools, the
 * daemon is the source of truth) or the in-browser agent (the wasm backend).
 * This module turns that declaration into the wiring a mounted workspace needs,
 * so a host that mounts `SproutWorkspace` gets the backend it asked for with no
 * extra setup.
 *
 * For the wasm backend it does the two things the hosted build's own
 * initialization does:
 *
 * - installs the cloud adapter from the host (the browser-local file ops and
 *   the in-browser agent query live there), so the adapter is present even when
 *   the host's transport is not the platform's bearer transport;
 * - routes the in-browser agent's events into the same event bus WebSocket
 *   events use (`setAgentEventDispatcher` → `WebSocketService.deliverLocal`),
 *   so the chat reducer renders them.
 *
 * The repository the workspace opens comes from the host project's `repoUrl`
 * (carried on the adapter config), rather than the `?repo=` URL parameter. The
 * in-browser agent's model endpoint is read from the host's transport by the
 * wasm-local handlers when they write the provider config, so it is not
 * threaded here.
 *
 * The daemon backend needs none of that wiring here: the host's transport URLs
 * already drive `clientFetch` and the WebSocket URL (the composition registers
 * the host for the module-level services), so the agent is reached through the
 * ordinary transport.
 *
 * A host may switch backends when a daemon becomes available. The supported
 * switch is a re-mount with a new transport: the effect below keys on the
 * backend, so a re-mount with a different `agent` re-runs the wiring against
 * the new backend. In-place switching without a re-mount is not supported.
 *
 * Nothing here is torn down on unmount. The cloud adapter singleton and the
 * agent-event dispatcher are process-lifetime, like the events transport the
 * composition deliberately leaves open across remounts: clearing the dispatcher
 * would kill in-browser agent events for anything mounted next (including the
 * hosted app root, which installs the same dispatcher).
 */

import type { SproutHost } from '../host';
import { installAdapter } from '../services/apiAdapter';
import { getBootstrapConfig } from '../bootstrapAdapter';
import { claimHostProjectRepo, importStartupRepo, releaseHostProjectRepo } from '../services/startupRepoImport';
import { WebSocketService } from '../services/websocket';
import type { WsEvent } from '@sprout/events';

/**
 * Install the cloud adapter for a host's wasm backend, with the host's repo so
 * its startup import uses `project.repoUrl` rather than the `?repo=` parameter.
 * Re-running is safe: `installAdapter` replaces the singleton.
 *
 * The host's transport URLs are authoritative: `''` is the same-origin
 * sentinel, so an embedding host that names no URL stays on its own origin
 * rather than inheriting the daemon bootstrap's absolute URLs (which belong to
 * the daemon the page was served from, not to the host that mounted the
 * workspace). Only the bootstrap config's non-URL settings (nav items, egress
 * proxy) are inherited.
 */
async function installWasmAdapter(host: SproutHost, repoUrl: string | undefined) {
  const { CloudAdapter } = await import('../services/cloudAdapter');
  const config = getBootstrapConfig();
  const adapter = new CloudAdapter({
    apiBase: host.transport.apiBaseURL,
    wsUrl: host.transport.wsURL,
    navItems: host.navigation.navItems ?? config.navItems ?? [],
    egressProxy: config.egressProxy,
    repoUrl,
    gitAuthor: host.user?.displayName ? { name: host.user.displayName } : undefined,
  });
  installAdapter(adapter);
  return adapter;
}

/**
 * Route the in-browser agent's events into the same bus WebSocket events use.
 * The dispatcher is installed once and left in place: the events transport is a
 * process-lifetime singleton, and the dispatcher reads the bus at delivery
 * time, so a later mount does not need to re-install it.
 */
async function installAgentEventDispatcher(): Promise<void> {
  const { setAgentEventDispatcher } = await import('../services/cloudWasmHandlers');
  setAgentEventDispatcher((event) => {
    WebSocketService.getInstance().deliverLocal(event as WsEvent);
  });
}

/**
 * Wire the host's agent backend. A no-op for a host that named no backend or
 * named the daemon (the transport URLs already drive the calls, and the events
 * socket's state is the connection state).
 *
 * For the wasm backend the agent is reachable once the adapter and the event
 * dispatcher are installed, not when an events socket opens (a host may serve
 * none). So the events transport reports not connected while they install and
 * connected once they are, and work that waits for a connection (the chat
 * session load, the provider catalog) cannot run ahead of the adapter. If the
 * install fails, the connection state falls back to the socket's.
 *
 * Resolves when the wiring is done.
 */
export async function wireHostAgentBackend(host: SproutHost, repoUrl: string | undefined): Promise<void> {
  const events = WebSocketService.getInstance();
  if (host.transport.agent?.kind !== 'wasm') {
    resetHostAgentBackend();
    return;
  }
  // Claimed before anything awaits, so the bootstrap's own install (which
  // the host registration sets off) does not import its repo over this one.
  if (repoUrl) claimHostProjectRepo(repoUrl);
  events.setLocalAgentState('starting');
  try {
    const [adapter] = await Promise.all([installWasmAdapter(host, repoUrl), installAgentEventDispatcher()]);
    events.setLocalAgentState('ready');
    // Once per repo per page: a re-mount or reconnect does not import again.
    if (repoUrl) void importStartupRepo(adapter, repoUrl, 'host');
  } catch (err) {
    console.warn('[SproutWorkspace] in-browser agent wiring failed:', err);
    events.setLocalAgentState('none');
  }
}

/**
 * Undo what a mounted workspace's wiring declared for the page: the events
 * transport goes back to the socket's connection state and the host project
 * repo claim is released, so the next mount (or a host that names no backend)
 * starts clean. The adapter and the event dispatcher stay installed (see
 * above); a later wasm mount re-wires them.
 */
export function resetHostAgentBackend(): void {
  WebSocketService.getInstance().setLocalAgentState('none');
  releaseHostProjectRepo();
}
