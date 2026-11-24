/**
 * projectFolderSync — keeps this tab's project folder and the server's in agreement.
 *
 * The tab remembers the project folder it is on (getTabProjectFolderPath). The server
 * can lose that choice: the client context is evicted after idle, or the
 * daemon restarts and comes back at its default root. When that happens the
 * tab re-selects its project folder instead of silently following the server to
 * the default. A server that moved somewhere else on purpose (a worktree
 * switch, another surface selecting a project) is followed instead.
 */

import type { EventsProvider } from '@sprout/events';
import { debugLog } from '../utils/log';
import { ApiService } from './api';
import type { ProjectFolderResponse } from './api/types';
import { getTabProjectFolderPath, persistTabProjectFolderPath } from './clientSession';
import { announceProjectFolderChanged, applyProjectFolder, refreshProjectFolder } from './projectFolderStore';

export type ProjectFolderReconcileOutcome =
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
 * Reconcile the tab's remembered project folder with `workspace` (a fresh GET).
 * `canSwitch` is false where the host does not allow selecting a project folder;
 * the tab then only follows the server.
 */
export async function reconcileTabProjectFolder(
  projectFolder: ProjectFolderResponse,
  canSwitch = true,
): Promise<ProjectFolderReconcileOutcome> {
  const serverRoot = trimSlashes(projectFolder.project_folder);
  const daemonRoot = trimSlashes(projectFolder.daemon_root);
  const remembered = trimSlashes(getTabProjectFolderPath());
  if (!serverRoot) return 'unresolved';
  if (remembered && remembered === serverRoot) return 'unchanged';

  if (daemonRoot && serverRoot === daemonRoot) {
    if (!remembered || !canSwitch) return 'unresolved';
    try {
      const response = await ApiService.getInstance().setProjectFolder(remembered);
      const restoredRoot = trimSlashes(response.project_folder) || remembered;
      persistTabProjectFolderPath(restoredRoot);
      // Replace the store's snapshot, which still shows the default root,
      // before telling the other workspace-scoped surfaces.
      void applyProjectFolder(restoredRoot, response.daemon_root);
      announceProjectFolderChanged(restoredRoot, response.daemon_root);
      return 'restored';
    } catch (err) {
      debugLog('[projectFolderSync] failed to restore the tab workspace:', err);
      return 'unresolved';
    }
  }

  if (remembered) {
    // Announce before persisting: listeners compare the new root with the
    // tab's remembered one to decide whether workspace-scoped UI is stale.
    announceProjectFolderChanged(serverRoot, daemonRoot);
  }
  persistTabProjectFolderPath(serverRoot);
  return 'adopted';
}

/**
 * Re-read the project folder before consenting to the home directory: the gate's
 * snapshot may predate a restore that already moved this tab to a project,
 * and consenting with it would put the tab back on home. Returns false when
 * consent is no longer needed.
 */
export async function consentToHomeProjectFolder(
  setProjectFolder: (path: string, consentHome?: boolean) => Promise<unknown> = (path, consentHome) =>
    ApiService.getInstance().setProjectFolder(path, consentHome),
): Promise<boolean> {
  const fresh = await refreshProjectFolder({ force: true });
  if (!fresh.project_folder_is_home || !fresh.needs_project_folder_selection) return false;
  await setProjectFolder(fresh.project_folder, true);
  // The gate reads the store; refresh it so the recorded consent closes it.
  await refreshProjectFolder({ force: true }).catch(() => undefined);
  return true;
}

type ReconnectSource = Pick<EventsProvider, 'onEvent' | 'removeEvent'>;

/**
 * After the event stream reconnects, refetch the project folder and reconcile it:
 * the server may have restarted or evicted this tab's context while the
 * stream was down. Returns the uninstall function.
 */
const installedSyncs = new WeakMap<ReconnectSource, () => void>();

export function installProjectFolderReconnectSync(source: ReconnectSource, canSwitch = true): () => void {
  // Idempotent per source: a remounted initializer must not stack listeners
  // that would each re-POST the project folder on reconnect.
  const existing = installedSyncs.get(source);
  if (existing) return existing;
  let disconnected = false;
  let syncing = false;

  const sync = async (): Promise<void> => {
    if (syncing) return;
    syncing = true;
    try {
      const projectFolder = await refreshProjectFolder({ force: true });
      await reconcileTabProjectFolder(projectFolder, canSwitch);
    } catch (err) {
      debugLog('[projectFolderSync] reconnect workspace sync failed:', err);
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
