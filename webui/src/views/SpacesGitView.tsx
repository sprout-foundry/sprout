import { FileCode, GitBranch, RefreshCw, X } from 'lucide-react';
import { useCallback, useState } from 'react';
import { DiffView } from '../components/DiffView';
import type { OpenProjectFolderBuffer, SidebarGitPanelProps } from '../components/git/gitPanelProps';
import { buildGitPanelProps } from '../components/git/gitPanelProps';
import SidebarGitSection from '../components/SidebarGitSection';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import { useGitHandlers } from '../hooks/useGitHandlers';
import { useGitProjectFolder } from '../hooks/useGitProjectFolder';
import type { GitDiffResponse, UseGitProjectFolderResult } from '../hooks/useGitProjectFolder';
import { useHostCapabilities } from '../host';
import { requestOpenInEditor } from './openInEditor';
import '../components/Sidebar.css';
import './SpacesGitView.css';

export type { SidebarGitPanelProps as SpacesGitPanelProps } from '../components/git/gitPanelProps';

export interface SpacesGitViewProps {
  /** Additional class on the view root, alongside `sprout-spaces-git`. */
  className?: string;
  /**
   * Open a working-tree file in the editor. Omitted, the view dispatches a
   * `sprout:open-in-editor` request, which the composed spaces and the
   * standalone app both handle.
   */
  onOpenFile?: (path: string, line?: number) => void;
  /**
   * A host-driven panel. Supplied, the view renders it as-is and runs no git
   * state of its own; the host owns status, actions and diffs.
   */
  gitPanel?: SidebarGitPanelProps;
}

interface ShownDiff {
  title: string;
  /** The working-tree path the diff belongs to; absent for a commit diff. */
  path?: string;
  text: string;
}

const noop = (): void => undefined;

/**
 * The source control view: working-tree status, staging, commit, branches,
 * pull/push and history — the app's git sidebar section, run against the
 * spaces' transport.
 *
 * Git requests go through the spaces fetch, so a daemon host serves them
 * from its backend and an in-browser (wasm) host from the browser's own git.
 * Features that need server-side git (worktrees) follow the host's
 * `serverGit` capability; a host without `git` gets the empty state and no
 * requests. A failed push is a response on the spaces' request channel,
 * so the escalation triggers see it as they do in the app.
 *
 * Diffs render inside the view: the app opens them as editor tabs, which the
 * assembled editor does not show (it holds file buffers only). Opening a
 * changed file goes to the editor unit.
 */
export function SpacesGitView({ className, onOpenFile, gitPanel }: SpacesGitViewProps): JSX.Element {
  const { git } = useHostCapabilities();
  const rootClass = className ? `sprout-spaces-git ${className}` : 'sprout-spaces-git';
  if (gitPanel) {
    return (
      <div className={rootClass} data-testid="spaces-git">
        <SidebarGitSection gitPanel={gitPanel} />
      </div>
    );
  }
  if (!git) {
    return (
      <div className={rootClass} data-testid="spaces-git">
        <GitEmptyState title="Source control isn't available" detail="This project folder has no git backend." />
      </div>
    );
  }
  return <ConnectedGitView rootClass={rootClass} onOpenFile={onOpenFile ?? requestOpenInEditor} />;
}

function ConnectedGitView({
  rootClass,
  onOpenFile,
}: {
  rootClass: string;
  onOpenFile: (path: string, line?: number) => void;
}): JSX.Element {
  const fetchFn = useSproutFetch();
  const [refreshToken, setRefreshToken] = useState(0);
  const handlers = useGitHandlers({ setGitRefreshToken: setRefreshToken });
  const [diff, setDiff] = useState<ShownDiff | null>(null);

  // Diff tabs become the view's inline diff; the other tab kinds (review,
  // compare) have no surface here and are not offered.
  const openProjectFolderBuffer = useCallback<OpenProjectFolderBuffer>((options) => {
    if (options.kind === 'diff') {
      const metadata = options.metadata ?? {};
      const response = metadata.diff as Partial<GitDiffResponse> | undefined;
      const source = typeof metadata.sourcePath === 'string' ? metadata.sourcePath : undefined;
      setDiff({
        title: options.title,
        path: source && !source.startsWith('commit:') ? source : undefined,
        text: response?.diff || options.content || '',
      });
    }
    return options.path;
  }, []);

  const gitProjectFolder = useGitProjectFolder({
    fetchFn,
    gitRefreshToken: refreshToken,
    onViewChange: noop,
    onGitCommit: handlers.handleGitCommit,
    onGitAICommit: handlers.handleGitAICommit,
    onGitStage: handlers.handleGitStage,
    onGitUnstage: handlers.handleGitUnstage,
    onGitDiscard: handlers.handleGitDiscard,
    openProjectFolderBuffer,
  });

  const openFile = useCallback((path: string) => onOpenFile(path), [onOpenFile]);
  // The review opens as an editor tab the assembled editor does not show, so
  // the view leaves it out.
  const panel: SidebarGitPanelProps = {
    ...buildGitPanelProps(gitProjectFolder, { openProjectFolderBuffer, onOpenFile: openFile }),
    onRunReview: undefined,
  };

  if (isMissingRepository(gitProjectFolder)) {
    return (
      <div className={rootClass} data-testid="spaces-git">
        <MissingRepositoryState projectFolder={gitProjectFolder} />
      </div>
    );
  }

  return (
    <div className={rootClass} data-testid="spaces-git">
      <div className="sprout-spaces-git__panel">
        <SidebarGitSection gitPanel={panel} />
      </div>
      {diff && (
        <section className="sprout-spaces-git__diff" data-testid="spaces-git-diff" aria-label={diff.title}>
          <header className="sprout-spaces-git__diff-header">
            <span className="sprout-spaces-git__diff-title" title={diff.title}>
              {diff.title}
            </span>
            {diff.path && (
              <button
                type="button"
                className="git-toolbar-icon-btn"
                title="Open file"
                aria-label="Open file"
                data-testid="spaces-git-diff-open"
                onClick={() => diff.path && onOpenFile(diff.path)}
              >
                <FileCode size={14} />
              </button>
            )}
            <button
              type="button"
              className="git-toolbar-icon-btn"
              title="Close diff"
              aria-label="Close diff"
              onClick={() => setDiff(null)}
            >
              <X size={14} />
            </button>
          </header>
          <div className="sprout-spaces-git__diff-body">
            {diff.text ? <DiffView diff={diff.text} /> : <div className="empty">No textual changes</div>}
          </div>
        </section>
      )}
    </div>
  );
}

/** Whether the git backend answered and reported no repository (no history to show). */
function isMissingRepository(projectFolder: UseGitProjectFolderResult): boolean {
  return projectFolder.hasLoadedGitStatus && !projectFolder.isGitLoading && !projectFolder.gitStatus;
}

function MissingRepositoryState({ projectFolder }: { projectFolder: UseGitProjectFolderResult }): JSX.Element {
  return (
    <GitEmptyState
      title="No git repository"
      detail={projectFolder.gitActionError ?? 'This project folder is not a git repository.'}
      onRefresh={() => void projectFolder.refreshGitStatus()}
    />
  );
}

/**
 * The Source control section for git state the caller owns (the composed
 * space shell runs one `useGitProjectFolder` for the section, the status bar and
 * the editor's diff tabs): the app's git section, or a clear empty state
 * once the backend reports no repository.
 */
export function SpacesGitStatusPanel({
  projectFolder,
  panel,
}: {
  projectFolder: UseGitProjectFolderResult;
  panel: SidebarGitPanelProps;
}): JSX.Element {
  if (isMissingRepository(projectFolder)) {
    return (
      <div className="sprout-spaces-git" data-testid="spaces-git">
        <MissingRepositoryState projectFolder={projectFolder} />
      </div>
    );
  }
  return (
    <div className="sprout-spaces-git" data-testid="spaces-git">
      <div className="sprout-spaces-git__panel">
        <SidebarGitSection gitPanel={panel} />
      </div>
    </div>
  );
}

function GitEmptyState({
  title,
  detail,
  onRefresh,
}: {
  title: string;
  detail: string;
  onRefresh?: () => void;
}): JSX.Element {
  return (
    <div className="sprout-spaces-git__empty" data-testid="spaces-git-empty">
      <GitBranch size={24} aria-hidden="true" />
      <div className="sprout-spaces-git__empty-title">{title}</div>
      <div className="sprout-spaces-git__empty-detail">{detail}</div>
      {onRefresh && (
        <button type="button" className="sidebar-action-btn" onClick={onRefresh}>
          <RefreshCw size={13} />
          Refresh
        </button>
      )}
    </div>
  );
}

export default SpacesGitView;
