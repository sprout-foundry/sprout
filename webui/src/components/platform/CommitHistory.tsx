/**
 * CommitHistory — Paginated commit log for a cloned repo.
 *
 * Fetches commits via gitClient.log, computes changed-file counts,
 * and renders expandable commit cards with "View diff" action.
 */

import React, { useState, useEffect, useCallback } from 'react';
import {
  GitCommit,
  GitFork,
  Calendar,
  User,
  ChevronDown,
  ChevronRight,
  Eye,
  Loader2,
} from 'lucide-react';
import { gitClient, type GitLogEntry } from '../../services/gitClient';
import './CommitHistory.css';

interface CommitHistoryProps {
  repoDir: string;
  onViewDiff: (sha: string) => void;
}

const PAGE_SIZE = 30;

function formatRelativeDate(timestamp: number): string {
  const now = Date.now();
  const diffMs = now - timestamp * 1000;
  const diffMin = Math.floor(diffMs / 60000);
  if (diffMin < 1) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHrs = Math.floor(diffMin / 60);
  if (diffHrs < 24) return `${diffHrs}h ago`;
  const diffDays = Math.floor(diffHrs / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return new Date(timestamp * 1000).toLocaleDateString();
}

async function getChangedFilesCount(repoDir: string, commit: GitLogEntry): Promise<number> {
  try {
    const files = await gitClient.getChangedFiles(repoDir, commit.oid, commit.commit.parent[0]);
    return files.length;
  } catch {
    return 0;
  }
}

const CommitHistory: React.FC<CommitHistoryProps> = ({ repoDir, onViewDiff }) => {
  const [commits, setCommits] = useState<GitLogEntry[]>([]);
  const [fileCounts, setFileCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedSha, setExpandedSha] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);

  const loadInitial = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const log = await gitClient.log(repoDir, { depth: PAGE_SIZE });
      setCommits(log);
      setHasMore(log.length >= PAGE_SIZE);

      // Compute changed-file counts in parallel (limit to first 30)
      const counts: Record<string, number> = {};
      await Promise.all(
        log.slice(0, 30).map(async (c) => {
          counts[c.oid] = await getChangedFilesCount(repoDir, c);
        }),
      );
      setFileCounts(counts);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load history');
    } finally {
      setLoading(false);
    }
  }, [repoDir]);

  useEffect(() => {
    loadInitial();
  }, [loadInitial]);

  const loadMore = useCallback(async () => {
    if (loadingMore || commits.length === 0) return;
    setLoadingMore(true);
    try {
      const lastCommit = commits[commits.length - 1];
      const moreLog = await gitClient.log(repoDir, {
        depth: PAGE_SIZE + 1,
        ref: lastCommit.oid,
      });
      // Skip the first entry (it's the last entry we already have)
      const newCommits = moreLog.slice(1);
      if (newCommits.length === 0) {
        setHasMore(false);
      } else {
        setCommits((prev) => [...prev, ...newCommits]);
        setHasMore(newCommits.length >= PAGE_SIZE);

        // Compute file counts for new commits
        const counts: Record<string, number> = { ...fileCounts };
        await Promise.all(
          newCommits.map(async (c) => {
            counts[c.oid] = await getChangedFilesCount(repoDir, c);
          }),
        );
        setFileCounts(counts);
      }
    } catch {
      // failed to load more — keep what we have
    } finally {
      setLoadingMore(false);
    }
  }, [repoDir, commits, loadingMore, fileCounts]);

  if (loading) {
    return (
      <div className="commit-history-loading">
        <Loader2 size={16} className="spinner" /> Loading history…
      </div>
    );
  }

  if (error) {
    return <div className="commit-history-error">{error}</div>;
  }

  if (commits.length === 0) {
    return <div className="commit-history-empty">No commits found.</div>;
  }

  return (
    <div className="commit-history">
      {commits.map((commit) => {
        const isExpanded = expandedSha === commit.oid;
        const shortSha = commit.oid.slice(0, 7);
        const firstLine = commit.commit.message.split('\n')[0];
        const body = commit.commit.message.slice(firstLine.length).trim();

        return (
          <div key={commit.oid} className={`commit-card ${isExpanded ? 'expanded' : ''}`}>
            <div
              className="commit-card-header"
              onClick={() => setExpandedSha(isExpanded ? null : commit.oid)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  setExpandedSha(isExpanded ? null : commit.oid);
                }
              }}
            >
              <div className="commit-card-icon">
                <GitCommit size={14} />
              </div>
              <div className="commit-card-main">
                <span className="commit-card-message">{firstLine}</span>
                <div className="commit-card-meta">
                  <span className="commit-card-sha">
                    <code>{shortSha}</code>
                  </span>
                  <span className="commit-card-changed">
                    {fileCounts[commit.oid] ?? '…'} files changed
                  </span>
                  {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                </div>
              </div>
            </div>

            {isExpanded && (
              <div className="commit-card-body">
                {body && <pre className="commit-card-message-body">{body}</pre>}
                <div className="commit-card-details">
                  <div className="commit-card-detail">
                    <User size={12} /> {commit.commit.author.name} &lt;{commit.commit.author.email}&gt;
                  </div>
                  <div className="commit-card-detail">
                    <Calendar size={12} /> {formatRelativeDate(commit.commit.author.timestamp)}
                  </div>
                  <div className="commit-card-detail">
                    <GitFork size={12} /> {shortSha}
                  </div>
                </div>
                <button
                  className="commit-card-diff-btn btn btn-sm btn-ghost"
                  onClick={() => onViewDiff(commit.oid)}
                >
                  <Eye size={12} /> View diff
                </button>
              </div>
            )}
          </div>
        );
      })}

      {hasMore && (
        <div className="commit-history-load-more">
          <button
            className="btn btn-sm btn-ghost"
            onClick={loadMore}
            disabled={loadingMore}
          >
            {loadingMore ? <Loader2 size={14} className="spinner" /> : null}
            {loadingMore ? ' Loading…' : 'Load more'}
          </button>
        </div>
      )}
    </div>
  );
};

export default CommitHistory;