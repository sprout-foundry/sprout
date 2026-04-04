import { useCallback, useEffect, useState } from 'react';
import type { FC, MutableRefObject } from 'react';
import { ArrowLeft, FileText, GitCompareArrows, Loader2, Clock, FolderOpen } from 'lucide-react';
import type { ApiService } from '../services/api';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import { formatRelativeDate, firstLine } from '../utils/format';
import { getStatusInfo } from '../utils/git';
import { useLog } from '../utils/log';
import './CommitDetailPanel.css';

interface CommitDetailPanelProps {
  apiService: ApiService;
  commit: GitCommitSummary;
  onBack: () => void;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
}

const CommitDetailPanel: FC<CommitDetailPanelProps> = ({ apiService, commit, onBack, openWorkspaceBuffer }) => {
  const log = useLog();
  const [detail, setDetail] = useState<GitCommitDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const fetchCommitDetail = useCallback(
    (cancelledRef?: MutableRefObject<boolean>) => {
      setIsLoading(true);
      setError(null);

      apiService
        .getGitCommitDetail(commit.hash)
        .then((data) => {
          if (!cancelledRef?.current) {
            setDetail(data);
          }
        })
        .catch((err) => {
          if (!cancelledRef?.current) {
            setError(err instanceof Error ? err.message : 'Failed to load commit details');
          }
        })
        .finally(() => {
          if (!cancelledRef?.current) {
            setIsLoading(false);
          }
        });
    },
    [apiService, commit.hash],
  );

  // Fetch commit detail on mount
  useEffect(() => {
    const cancelled = { current: false };
    fetchCommitDetail(cancelled);

    return () => {
      cancelled.current = true;
    };
  }, [fetchCommitDetail]);

  const handleRetry = useCallback(() => {
    // Fire-and-forget: no cancellation needed for user-initiated retry
    fetchCommitDetail();
  }, [fetchCommitDetail]);

  const handleFileClick = useCallback(
    async (filePath: string) => {
      try {
        const result = await apiService.getGitCommitFileDiff(commit.hash, filePath);
        openWorkspaceBuffer({
          kind: 'diff',
          path: `__workspace/commit/${commit.short_hash}/${filePath}`,
          title: `${commit.short_hash}: ${filePath}`,
          ext: '.diff',
          content: result.diff,
          metadata: {
            sourcePath: `commit:${commit.hash}:${filePath}`,
            diffContent: result.diff,
          },
        });
      } catch (err) {
        log.error('Failed to load file diff', { title: 'Git Error' });
      }
    },
    [apiService, commit.hash, commit.short_hash, openWorkspaceBuffer, log],
  );

  const handleViewAllDiffs = useCallback(() => {
    if (!detail) return;
    const subject = firstLine(detail.subject || commit.message);
    openWorkspaceBuffer({
      kind: 'diff',
      path: `__workspace/commit/${commit.short_hash}/full-diff`,
      title: `${commit.short_hash}: ${subject}`,
      ext: '.diff',
      content: detail.diff,
      metadata: {
        sourcePath: `commit:${commit.hash}`,
        diffContent: detail.diff,
      },
    });
  }, [detail, commit, openWorkspaceBuffer]);

  // ── Loading state ──────────────────────────────────────────────
  if (isLoading) {
    return (
      <div className="commit-detail-panel">
        <div className="commit-detail-loading">
          <Loader2 size={18} className="spinner" />
          <span>Loading commit details…</span>
        </div>
      </div>
    );
  }

  // ── Error state ────────────────────────────────────────────────
  if (error) {
    return (
      <div className="commit-detail-panel">
        <button type="button" className="commit-detail-back-btn" onClick={onBack}>
          <ArrowLeft size={14} />
          <span>Back to history</span>
        </button>
        <div className="commit-detail-empty commit-detail-error-state">
          <span>{error}</span>
          <button type="button" className="sidebar-action-btn compact" onClick={handleRetry}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!detail) return null;

  // ── File list view ──────────────────────────────────────────
  const fileList = detail.files || [];

  return (
    <div className="commit-detail-panel">
      {/* Header */}
      <div className="commit-detail-header">
        <button type="button" className="commit-detail-back-btn" onClick={onBack}>
          <ArrowLeft size={14} />
          <span>Back to history</span>
        </button>

        <div className="commit-detail-commit-info">
          <span className="commit-detail-hash">{detail.short_hash}</span>
          <span className="commit-detail-author">{detail.author}</span>
          <span className="commit-detail-date">
            <Clock size={11} />
            {formatRelativeDate(detail.date)}
          </span>
        </div>

        <div className="commit-detail-subject">{firstLine(detail.subject || commit.message)}</div>

        {detail.ref_names && <div className="commit-detail-refs">{detail.ref_names}</div>}

        {detail.stats && <div className="commit-detail-stats">{detail.stats}</div>}
      </div>

      {/* Actions */}
      <div className="commit-detail-actions">
        <button
          type="button"
          className="commit-detail-action-btn"
          onClick={handleViewAllDiffs}
          disabled={fileList.length === 0}
        >
          <GitCompareArrows size={12} />
          View All Diffs
        </button>
      </div>

      {/* File list */}
      {fileList.length === 0 ? (
        <div className="commit-detail-empty">
          <FolderOpen size={18} />
          <span>No files changed in this commit</span>
        </div>
      ) : (
        <div className="commit-detail-file-list thin-scrollbar">
          {fileList.map((file) => {
            const statusInfo = getStatusInfo(file.status);
            return (
              <button
                key={file.path}
                type="button"
                className="commit-detail-file-row"
                onClick={() => handleFileClick(file.path)}
                title={file.path}
              >
                <FileText size={13} className="commit-detail-file-row-icon" />
                <span className="commit-detail-file-path">{file.path}</span>
                <span className={`commit-detail-file-status ${statusInfo.className}`}>{statusInfo.label}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default CommitDetailPanel;
