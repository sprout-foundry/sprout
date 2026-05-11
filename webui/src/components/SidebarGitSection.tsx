import { useState, useEffect, useCallback } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { GitBranch, GitFork, History } from 'lucide-react';
import GitSidebarPanel from './GitSidebarPanel';
import type { GitSidebarPanelProps } from './GitSidebarPanel';
import GitHistoryPanel from './GitHistoryPanel';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import WorktreePanel from './WorktreePanel';
import { type SectionTab } from '../hooks/useSidebarState';

type GitSubTab = 'changes' | 'history' | 'worktrees';

interface ExtendedGitSidebarPanelProps extends GitSidebarPanelProps {
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
  onLoadCommits: (
    limit: number,
    offset: number,
    opts?: { signal?: AbortSignal },
  ) => Promise<{
    commits: GitCommitSummary[];
    total: number;
  }>;
  onLoadCommitDetail: (hash: string) => Promise<GitCommitDetail>;
  onLoadCommitFileDiff: (
    hash: string,
    filePath: string,
  ) => Promise<{
    message: string;
    hash: string;
    path: string;
    diff: string;
  }>;
  onCheckoutCommit: (commitHash: string) => Promise<{ message: string }>;
  onRevertCommit: (commitHash: string) => Promise<{ message: string }>;
}

interface SidebarGitSectionProps {
  gitPanel?: ExtendedGitSidebarPanelProps;
  currentView?: string;
  onSectionChange?: (section: SectionTab) => void;
}

export default function SidebarGitSection({
  gitPanel,
  currentView,
  onSectionChange,
}: SidebarGitSectionProps): JSX.Element {
  const [gitSubTab, setGitSubTab] = useState<GitSubTab>('changes');

  // Auto-switch to git tab and changes sub-tab when currentView === 'git'
  useEffect(() => {
    if (currentView === 'git') {
      onSectionChange?.('git');
      setGitSubTab('changes');
    }
  }, [currentView, onSectionChange]);

  // Keyboard navigation for tab bar (arrow keys + Home/End)
  const handleTabBarKeyDown = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
    const tabs = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]:not([disabled])'));
    const currentIndex = tabs.indexOf(document.activeElement as HTMLButtonElement);
    if (currentIndex === -1) return;
    let nextIndex = currentIndex;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') nextIndex = (currentIndex + 1) % tabs.length;
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') nextIndex = 0;
    else if (e.key === 'End') nextIndex = tabs.length - 1;
    else return;
    e.preventDefault();
    tabs[nextIndex].focus();
    const tab = tabs[nextIndex].dataset.tab as GitSubTab;
    if (tab) setGitSubTab(tab);
  }, []);

  if (!gitPanel) {
    return <div className="empty">Git unavailable</div>;
  }

  return (
    <>
      {/* Sub-tab bar: Changes / History / Worktrees */}
      <div className="git-sidebar-tab-bar" role="tablist" aria-label="Git sub-sections" onKeyDown={handleTabBarKeyDown}>
        <button
          type="button"
          role="tab"
          data-tab="changes"
          id="git-tab-current-changes"
          aria-controls="git-panel-current-changes"
          aria-selected={gitSubTab === 'changes'}
          className={`git-sidebar-tab ${gitSubTab === 'changes' ? 'active' : ''}`}
          onClick={() => setGitSubTab('changes')}
        >
          <GitBranch size={14} />
          <span>Changes</span>
        </button>
        <button
          type="button"
          role="tab"
          data-tab="history"
          id="git-tab-commit-history"
          aria-controls="git-panel-commit-history"
          aria-selected={gitSubTab === 'history'}
          className={`git-sidebar-tab ${gitSubTab === 'history' ? 'active' : ''}`}
          onClick={() => setGitSubTab('history')}
        >
          <History size={14} />
          <span>History</span>
        </button>
        <button
          type="button"
          role="tab"
          data-tab="worktrees"
          id="git-tab-worktrees"
          aria-controls="git-panel-worktrees"
          aria-selected={gitSubTab === 'worktrees'}
          className={`git-sidebar-tab ${gitSubTab === 'worktrees' ? 'active' : ''}`}
          onClick={() => setGitSubTab('worktrees')}
        >
          <GitFork size={14} />
          <span>Worktrees</span>
        </button>
      </div>

      {/* Changes sub-tab: working tree panel */}
      {gitSubTab === 'changes' && (
        <div id="git-panel-current-changes" role="tabpanel" aria-labelledby="git-tab-current-changes">
          <GitSidebarPanel {...gitPanel} />
        </div>
      )}

      {/* History sub-tab: GitHistoryPanel */}
      {gitSubTab === 'history' && (
        <div
          id="git-panel-commit-history"
          role="tabpanel"
          aria-labelledby="git-tab-commit-history"
          className="history-pane"
        >
          <GitHistoryPanel
            onLoadCommits={gitPanel.onLoadCommits}
            onLoadCommitDetail={gitPanel.onLoadCommitDetail}
            onLoadCommitFileDiff={gitPanel.onLoadCommitFileDiff}
            onCheckoutCommit={gitPanel.onCheckoutCommit}
            onRevertCommit={gitPanel.onRevertCommit}
            isActing={gitPanel.isActing}
            openWorkspaceBuffer={gitPanel.openWorkspaceBuffer}
          />
        </div>
      )}

      {/* Worktrees sub-tab: WorktreePanel */}
      {gitSubTab === 'worktrees' && (
        <div id="git-panel-worktrees" role="tabpanel" aria-labelledby="git-tab-worktrees">
          <WorktreePanel onClose={() => setGitSubTab('changes')} />
        </div>
      )}
    </>
  );
}
