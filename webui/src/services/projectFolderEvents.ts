/**
 * The window event that announces a project folder change. Sprout Studio
 * builds from before the rename fire it as `sprout:workspace-changed` with
 * the root in `workspaceRoot`; both are read here, so a listener sees one
 * shape either way.
 */
export const PROJECT_FOLDER_CHANGED_EVENT = 'sprout:project-folder-changed';
const EARLIER_PROJECT_FOLDER_CHANGED_EVENT = 'sprout:workspace-changed';

export interface ProjectFolderChangedDetail {
  projectFolder: string;
  daemonRoot: string;
}

function detailOf(event: Event): ProjectFolderChangedDetail {
  const d = (event as CustomEvent<Record<string, unknown> | undefined>).detail ?? {};
  const root = typeof d.projectFolder === 'string' ? d.projectFolder : d.workspaceRoot;
  return {
    projectFolder: typeof root === 'string' ? root : '',
    daemonRoot: typeof d.daemonRoot === 'string' ? d.daemonRoot : '',
  };
}

/** Announce a project folder change to this page. */
export function dispatchProjectFolderChanged(detail: ProjectFolderChangedDetail): void {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new CustomEvent(PROJECT_FOLDER_CHANGED_EVENT, { detail }));
}

/** Listen for a project folder change under either name; returns the unsubscribe. */
export function onProjectFolderChanged(handler: (detail: ProjectFolderChangedDetail) => void): () => void {
  if (typeof window === 'undefined') return () => {};
  const listener = (event: Event) => handler(detailOf(event));
  window.addEventListener(PROJECT_FOLDER_CHANGED_EVENT, listener);
  window.addEventListener(EARLIER_PROJECT_FOLDER_CHANGED_EVENT, listener);
  return () => {
    window.removeEventListener(PROJECT_FOLDER_CHANGED_EVENT, listener);
    window.removeEventListener(EARLIER_PROJECT_FOLDER_CHANGED_EVENT, listener);
  };
}
