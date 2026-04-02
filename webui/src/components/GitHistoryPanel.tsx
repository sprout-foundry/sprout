import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Loader2, ChevronRight, Clock, GitCommitHorizontal } from 'lucide-react';
import type { ApiService } from '../services/api';
import CommitDetailPanel from './CommitDetailPanel';

interface GitCommitEntry {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  message: string;
  ref_names?: string;
}

interface GitHistoryPanelProps {
  apiService: ApiService;
  isActing: boolean;
  onRefresh?: () => void;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, any>;
  }) => string;
}

/** Simple relative date formatter (e.g. "3 days ago", "2 hours ago") */
function formatRelativeDate(dateStr: string): string {
  try {
    const date = new Date(dateStr);
    if (isNaN(date.getTime())) {
      return dateStr;
    }
    const now = Date.now();
    const diffMs = now - date.getTime();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHours = Math.floor(diffMin / 60);
    const diffDays = Math.floor(diffHours / 24);
    const diffWeeks = Math.floor(diffDays / 7);
    const diffMonths = Math.floor(diffDays / 30);
    const diffYears = Math.floor(diffDays / 365);

    if (diffSec < 60) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;
    if (diffWeeks < 5) return `${diffWeeks}w ago`;
    if (diffMonths < 12) return `${diffMonths}mo ago`;
    return `${diffYears}y ago`;
  } catch {
    return dateStr;
  }
}

/** Extract the first line of a commit message (subject line). */
function firstLine(message: string): string {
  const idx = message.indexOf('\n');
  return idx >= 0 ? message.slice(0, idx) : message;
}

const PAGE_SIZE = 30;

const GitHistoryPanel: React.FC<GitHistoryPanelProps> = ({
  apiService,
  isActing,
  onRefresh,
  openWorkspaceBuffer,
}) => {
  const [commits, setCommits] = useState<GitCommitEntry[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [selectedCommit, setSelectedCommit] = useState<GitCommitEntry | null>(null);
  const loadMoreAbortRef = useRef<AbortController | null>(null);

  const fetchCommits = useCallback(async (offset: number, append: boolean, signal?: AbortSignal) => {
    if (append) {
      setIsLoadingMore(true);
    } else {
      setIsLoading(true);
    }
    setError(null);

    try {
      const response = await apiService.getGitLog(PAGE_SIZE, offset, { signal });
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
      setError(err instanceof Error ? err.message : 'Failed to load commit history');
    } finally {
      if (!signal?.aborted) {
        setIsLoading(false);
        setIsLoadingMore(false);
      }
    }
  }, [apiService]);

  // Fetch initial commits on mount and when refresh changes
  useEffect(() => {
    const controller = new AbortController();
    setCommits([]);
    setHasMore(false);
    fetchCommits(0, false, controller.signal);
    return () => controller.abort();
  }, [fetchCommits, onRefresh]);

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

  const handleCommitClick = useCallback((commit: GitCommitEntry) => {
    if (isActing) return;
    setSelectedCommit(commit);
  }, [isActing]);

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
        <CommitDetailPanel
          apiService={apiService}
          commit={selectedCommit}
          onBack={() => setSelectedCommit(null)}
          openWorkspaceBuffer={openWorkspaceBuffer}
        />
      </div>
    );
  }

  return (
    <div className="git-history-panel">
      {error && commits.length > 0 && (
        <div className="git-history-error">
          <span>{error}</span>
        </div>
      )}
      <div className="git-history-commit-list">
        {commits.map((commit) => (
            <button
              key={commit.hash}
              type="button"
              className="git-history-commit-row"
              onClick={() => handleCommitClick(commit)}
              disabled={isActing}
              title={commit.message}
            >
              <div className="git-history-commit-top">
                <span className="git-history-commit-hash">{commit.short_hash}</span>
                <span className="git-history-commit-author">{commit.author}</span>
                <span className="git-history-commit-date">
                  <Clock size={11} />
                  {formatRelativeDate(commit.date)}
                </span>
              </div>
              <div className="git-history-commit-message">
                {firstLine(commit.message)}
              </div>
              {commit.ref_names && (
                <div className="git-history-commit-refs">
                  {commit.ref_names}
                </div>
              )}
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
};

export default GitHistoryPanel;
