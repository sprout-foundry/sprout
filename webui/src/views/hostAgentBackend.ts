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
 * A host may switch backends when a daemon becomes available, in place: the
 * composition re-runs its wiring on the new transport (a new `host` with a
 * different `agent`), and this module re-wires the backend against it —
 * retiring the in-browser adapter (the cloud adapter the composition's wasm
 * wiring or the page's bootstrap installed, so the daemon's transport URLs
 * drive the calls again) and handing the connection state back to the events
 * socket. Re-mounting the workspace with a new transport also works and is
 * equivalent. An adapter of any other kind (one the host installed itself) is
 * never retired — the host owns it — and a superseded async wasm install (a
 * slower import that resolves after the switch) is discarded by generation,
 * so it cannot re-install its adapter or flip the connection state the newer
 * wiring already set.
 *
 * Nothing here is torn down on unmount. The cloud adapter singleton and the
 * agent-event dispatcher are process-lifetime, like the events transport the
 * composition deliberately leaves open across remounts: clearing the dispatcher
 * would kill in-browser agent events for anything mounted next (including the
 * hosted app root, which installs the same dispatcher).
 */

import type { WsEvent } from '@sprout/events';
import { getBootstrapConfig } from '../bootstrapAdapter';
import type { SproutHost } from '../host';
import { getAdapter, installAdapter, uninstallAdapter } from '../services/apiAdapter';
import type { CloudAdapter } from '../services/cloudAdapter';
import { claimHostProjectRepo, importStartupRepo, releaseHostProjectRepo } from '../services/startupRepoImport';
import { WebSocketService } from '../services/websocket';

/**
 * Bumped on every wire call. A superseded async continuation (a slower wasm
 * install that resolves after a newer wire) reads it to tell it is stale and
 * must not touch the shared state the newer wire already set.
 */
let wireGeneration = 0;

/**
 * Retire the in-browser adapter so a switched-to daemon's transport URLs drive
 * the calls again.
 *
 * The in-browser adapter is recognised by its type (the cloud adapter), not by
 * an ownership slot: the composition's own wasm wiring and the page's
 * bootstrap host-change hook can each install one (and the bootstrap's can win
 * the race), so either may be the live singleton at the switch — both must
 * go. An adapter the host installed itself (an ambient caller's non-cloud
 * adapter) is not of that type and is left in place — the host owns it.
 */
async function retireInBrowserAdapter(): Promise<void> {
  const installed = getAdapter();
  if (installed === null) return;
  const { CloudAdapter: CloudAdapterImpl } = await import('../services/cloudAdapter');
  // Re-read after the import yields: the bootstrap hook may have installed
  // while this retire was waiting to import the adapter module.
  const live = getAdapter();
  if (live !== null && live instanceof CloudAdapterImpl) {
    uninstallAdapter(live);
  }
}

/**
 * Build (do not install) the cloud adapter for a host's wasm backend, with the
 * host's repo so its startup import uses `project.repoUrl` rather than the
 * `?repo=` URL parameter. The install happens in the caller, once the
 * generation check has confirmed this wiring was not superseded.
 *
 * The host's transport URLs are authoritative: `''` is the same-origin
 * sentinel, so an embedding host that names no URL stays on its own origin
 * rather than inheriting the daemon bootstrap's absolute URLs (which belong to
 * the daemon the page was served from, not to the host that mounted the
 * workspace). Only the bootstrap config's non-URL settings (nav items, egress
 * proxy) are inherited.
 */
async function buildWasmAdapter(host: SproutHost, repoUrl: string | undefined): Promise<CloudAdapter> {
  const { CloudAdapter: CloudAdapterImpl } = await import('../services/cloudAdapter');
  const config = getBootstrapConfig();
  return new CloudAdapterImpl({
    apiBase: host.transport.apiBaseURL,
    wsUrl: host.transport.wsURL,
    navItems: host.navigation.navItems ?? config.navItems ?? [],
    egressProxy: config.egressProxy,
    repoUrl,
    gitAuthor: host.user?.displayName ? { name: host.user.displayName } : undefined,
  });
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
 * socket's state is the connection state) — except it retires the in-browser
 * adapter this wiring installed, so a host that switches backends in place
 * (a new `host` with a new `agent` on the mounted workspace) hands its calls
 * back to the daemon's transport.
 *
 * For the wasm backend the agent is reachable once the adapter and the event
 * dispatcher are installed, not when an events socket opens (a host may serve
 * none). So the events transport reports not connected while they install and
 * connected once they are, and work that waits for a connection (the chat
 * session load, the provider catalog) cannot run ahead of the adapter. If the
 * install fails, the connection state falls back to the socket's.
 *
 * Every call bumps the generation, so a superseded wasm install that resolves
 * late (a backend switched to the daemon while its import was in flight)
 * neither installs its adapter nor flips the connection state the newer wire
 * set.
 *
 * Resolves when the wiring is done.
 */
export async function wireHostAgentBackend(host: SproutHost, repoUrl: string | undefined): Promise<void> {
  const generation = ++wireGeneration;
  const events = WebSocketService.getInstance();
  if (host.transport.agent?.kind !== 'wasm') {
    // The daemon (or no backend) reaches the agent through the transport
    // URLs, so an in-browser adapter (the composition's wasm wiring or the
    // page's bootstrap installed it) must go; an adapter of another kind is
    // the host's own and stays. The retire is async (it imports the adapter
    // module for the type check), so the reset follows it; a newer wire's
    // own state would override this one.
    await retireInBrowserAdapter();
    resetHostAgentBackend();
    return;
  }
  // Claimed before anything awaits, so the bootstrap's own install (which
  // the host registration sets off) does not import its repo over this one.
  if (repoUrl) claimHostProjectRepo(repoUrl);
  events.setLocalAgentState('starting');
  try {
    const [adapter] = await Promise.all([buildWasmAdapter(host, repoUrl), installAgentEventDispatcher()]);
    // A newer wire (or a daemon switch) ran while the install was in flight:
    // it owns the backend now, so this one must not install its adapter or
    // flip the connection state it already set.
    if (generation !== wireGeneration) return;
    // Re-running is safe: installAdapter replaces the singleton.
    installAdapter(adapter);
    events.setLocalAgentState('ready');
    // Once per repo per page: a re-mount or reconnect does not import again.
    if (repoUrl) void importStartupRepo(adapter, repoUrl, 'host');
  } catch (err) {
    if (generation !== wireGeneration) return;
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

/** Clear the wiring's module state (tests only). */
export function __resetHostAgentBackendForTests(): void {
  wireGeneration = 0;
}
