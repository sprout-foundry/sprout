import { FileTree } from '@sprout/ui';
import type { FileTreeHandle, FileTreeProps } from '@sprout/ui';
import { useEffect, useRef } from 'react';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';

/** Fired by the startup repo import once the repository is in the workspace. */
const REPO_IMPORTED_EVENT = 'sprout:repo-imported';

/**
 * The file tree an own-mode `SproutWorkspace` renders for an arranged
 * `fileTree`: the `FileTree` primitive with the props it is handed, reloaded
 * when the workspace's contents change underneath it.
 *
 * The tree lists once, on mount. On a host whose agent runs in the browser
 * that first listing can run before the agent's adapter is installed (so it
 * goes to the host's backend rather than the in-browser file store) and
 * before the host project's repository has been imported (so the store is
 * still empty). Nothing would list it again, leaving the tree empty or stale
 * for the session. It therefore reloads, in place, when an adapter is
 * installed and when the startup repo import completes.
 */
export function WorkspaceFileTreeView(props: FileTreeProps): JSX.Element {
  const treeRef = useRef<FileTreeHandle>(null);

  useEffect(() => {
    const reload = () => treeRef.current?.refresh({ quiet: true });
    window.addEventListener(ADAPTER_INSTALLED_EVENT, reload);
    window.addEventListener(REPO_IMPORTED_EVENT, reload);
    return () => {
      window.removeEventListener(ADAPTER_INSTALLED_EVENT, reload);
      window.removeEventListener(REPO_IMPORTED_EVENT, reload);
    };
  }, []);

  return <FileTree ref={treeRef} {...props} />;
}

export default WorkspaceFileTreeView;
