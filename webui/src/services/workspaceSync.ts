/**
 * workspaceSync — keeps this tab's workspace and the server's in agreement.
 *
 * The tab remembers the workspace it is on (getTabWorkspacePath). The server
 * can lose that choice: the client context is evicted after idle, or the
 * daemon restarts and comes back at its default root. When that happens the
 * tab re-selects its workspace instead of silently following the server to
 * the default. A server that moved somewhere else on purpose (a worktree
 * switch, another surface selecting a project) is followed instead.
 */

import type { EventsProvider } from '@sprout/events';
import { debugLog } from '../utils/log';
import { ApiService } from './api';
import type { WorkspaceResponse } from './api/types';
import { getTabWorkspacePath, persistTabWorkspacePath } from './clientSession';
import { announceWorkspaceChanged, applyWorkspaceRoot, refreshWorkspace } from './workspaceStore';

export type WorkspaceReconcileOutcome =
  /** Server and tab agree. */
  | 'unchanged'
  /** The server had lost the tab's workspace; it was selected again. */
  | 'restored'
  /** The server moved to another workspace on purpose; the tab followed. */
  | 'adopted'
  /** Nothing to restore (or restoring failed); the workspace gate decides. */
  | 'unresolved';

function trimSlashes(path: string): string {
  const trimmed = (path || '').trim();
  return trimmed.length > 1 ? trimmed.replace(/\/+$/, '') : trimmed;
}

/**
 * Reconcile the tab's remembered workspace with `workspace` (a fresh GET).
 * `canSwitch` is false where the host does not allow selecting a workspace;
 * the tab then only follows the server.
 */
export async function reconcileTabWorkspace(
  workspace: WorkspaceResponse,
  canSwitch = true,
): Promise<WorkspaceReconcileOutcome> {
  const serverRoot = trimSlashes(workspace.workspace_root);
  const daemonRoot = trimSlashes(workspace.daemon_root);
  const remembered = trimSlashes(getTabWorkspacePath());
  if (!serverRoot) return 'unresolved';
  if (remembered && remembered === serverRoot) return 'unchanged';

  if (daemonRoot && serverRoot === daemonRoot) {
    if (!remembered || !canSwitch) return 'unresolved';
    try {
      const response = await ApiService.getInstance().setWorkspace(remembered);
      const restoredRoot = trimSlashes(response.workspace_root) || remembered;
      persistTabWorkspacePath(restoredRoot);
      // Replace the store's snapshot, which still shows the default root,
      // before telling the other workspace-scoped surfaces.
      void applyWorkspaceRoot(restoredRoot, response.daemon_root);
      announceWorkspaceChanged(restoredRoot, response.daemon_root);
      return 'restored';
    } catch (err) {
      debugLog('[workspaceSync] failed to restore the tab workspace:', err);
      return 'unresolved';
    }
  }

  if (remembered) {
    // Announce before persisting: listeners compare the new root with the
    // tab's remembered one to decide whether workspace-scoped UI is stale.
    announceWorkspaceChanged(serverRoot, daemonRoot);
  }
  persistTabWorkspacePath(serverRoot);
  return 'adopted';
}

/**
 * Re-read the workspace before consenting to the home directory: the gate's
 * snapshot may predate a restore that already moved this tab to a project,
 * and consenting with it would put the tab back on home. Returns false when
 * consent is no longer needed.
 */
export async function consentToHomeWorkspace(
  setWorkspace: (path: string, consentHome?: boolean) => Promise<unknown> = (path, consentHome) =>
    ApiService.getInstance().setWorkspace(path, consentHome),
): Promise<boolean> {
  const fresh = await refreshWorkspace({ force: true });
  if (!fresh.workspace_is_home || !fresh.needs_workspace_selection) return false;
  await setWorkspace(fresh.workspace_root, true);
  // The gate reads the store; refresh it so the recorded consent closes it.
  await refreshWorkspace({ force: true }).catch(() => undefined);
  return true;
}

type ReconnectSource = Pick<EventsProvider, 'onEvent' | 'removeEvent'>;

/**
 * After the event stream reconnects, refetch the workspace and reconcile it:
 * the server may have restarted or evicted this tab's context while the
 * stream was down. Returns the uninstall function.
 */
const installedSyncs = new WeakMap<ReconnectSource, () => void>();

export function installWorkspaceReconnectSync(source: ReconnectSource, canSwitch = true): () => void {
  // Idempotent per source: a remounted initializer must not stack listeners
  // that would each re-POST the workspace on reconnect.
  const existing = installedSyncs.get(source);
  if (existing) return existing;
  let disconnected = false;
  let syncing = false;

  const sync = async (): Promise<void> => {
    if (syncing) return;
    syncing = true;
    try {
      const workspace = await refreshWorkspace({ force: true });
      await reconcileTabWorkspace(workspace, canSwitch);
    } catch (err) {
      debugLog('[workspaceSync] reconnect workspace sync failed:', err);
    } finally {
      syncing = false;
    }
  };

  const onEvent: Parameters<ReconnectSource['onEvent']>[0] = (event) => {
    if (event.type !== 'connection_status') return;
    const data = (event.data ?? {}) as { connected?: boolean; reconnected?: boolean };
    if (data.connected === false) {
      disconnected = true;
      return;
    }
    if (data.connected === true && (disconnected || data.reconnected)) {
      disconnected = false;
      void sync();
    }
  };

  source.onEvent(onEvent);
  const uninstall = () => {
    source.removeEvent(onEvent);
    installedSyncs.delete(source);
  };
  installedSyncs.set(source, uninstall);
  return uninstall;
}
