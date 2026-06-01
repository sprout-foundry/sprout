import { Loader2, ChevronRight, Clock, GitCommitHorizontal, Search, X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import { formatRelativeDate, formatAbsoluteDate, firstLine } from '../utils/format';
import { debugLog } from '../utils/log';
import CommitDetailPanel from './CommitDetailPanel';
import GitHistoryContextMenu from './GitHistoryContextMenu';
import './GitHistoryPanel.css';

interface GitHistoryPanelProps {
  onLoadCommits: (
    limit: number,
    offset: number,
    opts?: { signal?: AbortSignal },
  ) => Promise<{ commits: GitCommitSummary[]; total: number }>;
  onLoadCommitDetail: (hash: string) => Promise<GitCommitDetail>;
  onLoadCommitFileDiff: (
    hash: string,
    filePath: string,
  ) => Promise<{ message: string; hash: string; path: string; diff: string }>;
  onCheckoutCommit: (commitHash: string) => Promise<{ message: string }>;
  onRevertCommit: (commitHash: string) => Promise<{ message: string }>;
  isActing: boolean;
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
}

const PAGE_SIZE = 30;

function GitHistoryPanel({
  onLoadCommits,
  onLoadCommitDetail,
  onLoadCommitFileDiff,
  onCheckoutCommit,
  onRevertCommit,
  isActing,
  openWorkspaceBuffer,
}: GitHistoryPanelProps): JSX.Element {
  const [commits, setCommits] = useState<GitCommitSummary[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [selectedCommit, setSelectedCommit] = useState<GitCommitSummary | null>(null);
  const [commitFilter, setCommitFilter] = useState('');
  const loadMoreAbortRef = useRef<AbortController | null>(null);

  // Filter commits by SHA, subject, or author (case-insensitive substring).
  const filteredCommits = useMemo(() => {
    const q = commitFilter.trim().toLowerCase();
    if (!q) return commits;
    return commits.filter(
      (c) =>
        c.hash.toLowerCase().includes(q) ||
        c.short_hash.toLowerCase().includes(q) ||
        c.author.toLowerCase().includes(q) ||
        c.message.toLowerCase().includes(q),
    );
  }, [commits, commitFilter]);

  // Sibling lookup for prev/next nav from detail view. Operates on the full
  // loaded list (not the filter), since filtering shouldn't change navigation.
  const selectedIndex = useMemo(
    () => (selectedCommit ? commits.findIndex((c) => c.hash === selectedCommit.hash) : -1),
    [commits, selectedCommit],
  );
  const prevCommit = selectedIndex > 0 ? commits[selectedIndex - 1] : null;
  const nextCommit =
    selectedIndex >= 0 && selectedIndex < commits.length - 1 ? commits[selectedIndex + 1] : null;

  const fetchCommits = useCallback(
    async (offset: number, append: boolean, signal?: AbortSignal) => {
      if (append) {
        setIsLoadingMore(true);
      } else {
        setIsLoading(true);
      }
      setError(null);

      try {
        const response = await onLoadCommits(PAGE_SIZE, offset, { signal });
        if (signal?.aborted) return;
        const newCommits = response.commits || [];
        const total = response.total || 0;

        if (append) {
          setCommits((prev) => [...prev, ...newCommits]);
        } else {
          setCommits(newCommits);
        }
        setHasMore(offset + newCommits.length < total);
      } catch (err) {
        if (signal?.aborted) return;
        debugLog('Failed to load commit history:', err);
        setError(err instanceof Error ? err.message : 'Failed to load commit history');
      } finally {
        if (!signal?.aborted) {
          setIsLoading(false);
          setIsLoadingMore(false);
        }
      }
    },
    [onLoadCommits],
  );

  // Fetch initial commits on mount; re-fetch when refresh is signaled
  useEffect(() => {
    const controller = new AbortController();
    setCommits([]);
    setHasMore(false);
    fetchCommits(0, false, controller.signal);
    return () => controller.abort();
  }, [fetchCommits]);

  // Abort any in-flight "load more" fetch on unmount
  useEffect(() => {
    return () => {
      loadMoreAbortRef.current?.abort();
    };
  }, []);

  const handleLoadMore = useCallback(() => {
    loadMoreAbortRef.current?.abort();
    const controller = new AbortController();
    loadMoreAbortRef.current = controller;
    fetchCommits(commits.length, true, controller.signal);
  }, [fetchCommits, commits.length]);

  const handleCommitClick = useCallback(
    (commit: GitCommitSummary) => {
      if (isActing) return;
      setSelectedCommit(commit);
    },
    [isActing],
  );

  if (isLoading && commits.length === 0) {
    return (
      <div className="git-history-panel">
        <div className="git-history-loading">
          <Loader2 size={16} className="spinner" />
          <span>Loading commit history…</span>
        </div>
      </div>
    );
  }

  if (error && commits.length === 0) {
    return (
      <div className="git-history-panel">
        <div className="git-history-error">
          <span>{error}</span>
          <button
            type="button"
            className="sidebar-action-btn compact"
            onClick={() => fetchCommits(0, false)}
            disabled={isActing}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (commits.length === 0) {
    return (
      <div className="git-history-panel">
        <div className="git-history-empty">
          <GitCommitHorizontal size={16} />
          <span>No commits found</span>
        </div>
      </div>
    );
  }

  // If a commit is selected, show the detail panel
  if (selectedCommit) {
    return (
      <div className="git-history-panel">
        <GitHistoryContextMenu
          onCheckoutCommit={onCheckoutCommit}
          onRevertCommit={onRevertCommit}
          isActing={isActing}
        />
        <CommitDetailPanel
          onLoadCommitDetail={onLoadCommitDetail}
          onLoadCommitFileDiff={onLoadCommitFileDiff}
          commit={selectedCommit}
          onBack={() => setSelectedCommit(null)}
          prevCommit={prevCommit}
          nextCommit={nextCommit}
          onSelectCommit={(c) => setSelectedCommit(c)}
          openWorkspaceBuffer={openWorkspaceBuffer}
        />
      </div>
    );
  }

  return (
    <div className="git-history-panel">
      <GitHistoryContextMenu onCheckoutCommit={onCheckoutCommit} onRevertCommit={onRevertCommit} isActing={isActing} />
      {error && commits.length > 0 && (
        <div className="git-history-error">
          <span>{error}</span>
        </div>
      )}
      <div className="git-history-filter">
        <Search size={12} className="git-history-filter-icon" aria-hidden="true" />
        <input
          type="text"
          className="git-history-filter-input"
          value={commitFilter}
          onChange={(e) => setCommitFilter(e.target.value)}
          placeholder="Filter commits (subject, SHA, author)…"
          aria-label="Filter commits"
        />
        {commitFilter && (
          <button
            type="button"
            className="git-history-filter-clear"
            onClick={() => setCommitFilter('')}
            title="Clear filter"
            aria-label="Clear filter"
          >
            <X size={12} />
          </button>
        )}
      </div>
      {commitFilter.trim() && filteredCommits.length === 0 ? (
        <div className="git-history-empty">
          <Search size={16} />
          <span>No commits match “{commitFilter}”</span>
        </div>
      ) : null}
      <div className="git-history-commit-list thin-scrollbar">
        {filteredCommits.map((commit) => (
          <button
            key={commit.hash}
            type="button"
            className="git-history-commit-row"
            onClick={() => handleCommitClick(commit)}
            disabled={isActing}
            title={commit.message}
            data-commit-hash={commit.hash}
            data-commit-short-hash={commit.short_hash}
            data-commit-message={commit.message}
          >
            <div className="git-history-commit-top">
              <span className="git-history-commit-hash">{commit.short_hash}</span>
              <span className="git-history-commit-author">{commit.author}</span>
              <span className="git-history-commit-date" title={formatAbsoluteDate(commit.date)}>
                <Clock size={11} />
                {formatRelativeDate(commit.date)}
              </span>
            </div>
            <div className="git-history-commit-message">{firstLine(commit.message)}</div>
            {commit.ref_names && <div className="git-history-commit-refs">{commit.ref_names}</div>}
          </button>
        ))}
      </div>
      {hasMore && (
        <button
          type="button"
          className="git-history-load-more"
          onClick={handleLoadMore}
          disabled={isLoadingMore || isActing}
        >
          {isLoadingMore ? (
            <>
              <Loader2 size={14} className="spinner" />
              <span>Loading…</span>
            </>
          ) : (
            <>
              <ChevronRight size={14} />
              <span>Load more commits</span>
            </>
          )}
        </button>
      )}
    </div>
  );
}

export default GitHistoryPanel;
