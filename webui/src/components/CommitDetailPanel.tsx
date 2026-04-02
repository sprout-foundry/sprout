import React, { useCallback, useEffect, useState } from 'react';
import {
  ArrowLeft,
  FileText,
  GitCompareArrows,
  Loader2,
  Clock,
  ExternalLink,
  FolderOpen,
} from 'lucide-react';
import type { ApiService } from '../services/api';

interface CommitInfo {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  message: string;
  ref_names?: string;
}

interface CommitFileEntry {
  path: string;
  status: string;
}

interface CommitDetail {
  message: string;
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  ref_names?: string;
  subject: string;
  files: CommitFileEntry[];
  diff: string;
  stats: string;
}

interface CommitDetailPanelProps {
  apiService: ApiService;
  commit: CommitInfo;
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

type ViewMode = 'file-list' | 'individual-diff' | 'all-diffs';

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

/** Determine the display label and CSS class for a git file status. */
function getStatusInfo(status: string): { label: string; className: string } {
  const s = status.charAt(0).toUpperCase();
  switch (s) {
    case 'A': return { label: 'A', className: 'status-a' };
    case 'M': return { label: 'M', className: 'status-m' };
    case 'D': return { label: 'D', className: 'status-d' };
    case 'R': return { label: 'R', className: 'status-r' };
    case 'C': return { label: 'C', className: 'status-c' };
    default: return { label: status || '?', className: 'status-unknown' };
  }
}

const CommitDetailPanel: React.FC<CommitDetailPanelProps> = ({
  apiService,
  commit,
  onBack,
  openWorkspaceBuffer,
}) => {
  const [viewMode, setViewMode] = useState<ViewMode>('file-list');
  const [detail, setDetail] = useState<CommitDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Individual diff state
  const [individualDiffPath, setIndividualDiffPath] = useState<string | null>(null);
  const [individualDiff, setIndividualDiff] = useState<string | null>(null);
  const [isDiffLoading, setIsDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);

  // Fetch commit detail on mount
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);

    apiService
      .getGitCommitDetail(commit.hash)
      .then((data) => {
        if (!cancelled) {
          setDetail(data);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load commit details');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [apiService, commit.hash]);

  const handleRetry = useCallback(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);

    apiService
      .getGitCommitDetail(commit.hash)
      .then((data) => {
        if (!cancelled) {
          setDetail(data);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load commit details');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsLoading(false);
        }
      });
  }, [apiService, commit.hash]);

  const handleFileClick = useCallback(
    async (filePath: string) => {
      setIsDiffLoading(true);
      setDiffError(null);
      setIndividualDiffPath(filePath);

      try {
        const result = await apiService.getGitCommitFileDiff(commit.hash, filePath);
        setIndividualDiff(result.diff);
        setViewMode('individual-diff');
      } catch (err) {
        setDiffError(err instanceof Error ? err.message : 'Failed to load file diff');
      } finally {
        setIsDiffLoading(false);
      }
    },
    [apiService, commit.hash]
  );

  const handleViewAllDiffs = useCallback(() => {
    setViewMode('all-diffs');
  }, []);

  const handleBackToFileList = useCallback(() => {
    setViewMode('file-list');
    setIndividualDiff(null);
    setIndividualDiffPath(null);
    setDiffError(null);
  }, []);

  const handleOpenInWorkspace = useCallback(
    (
      content: string,
      titleSuffix: string,
      pathSuffix: string
    ) => {
      openWorkspaceBuffer({
        kind: 'diff',
        path: `__workspace/commit/${commit.short_hash}/${pathSuffix}`,
        title: `${commit.short_hash}: ${titleSuffix}`,
        ext: '.diff',
        content,
        metadata: {
          sourcePath: `commit:${commit.hash}`,
        },
      });
    },
    [openWorkspaceBuffer, commit.hash, commit.short_hash]
  );

  // ── Loading state ──────────────────────────────────────────────
  if (isLoading) {
    return (
      <div className="commit-detail-panel">
        <div className="commit-detail-loading">
          <Loader2 size={18} className="commit-detail-spinner" />
          <span>Loading commit details…</span>
        </div>
      </div>
    );
  }

  // ── Error state ────────────────────────────────────────────────
  if (error) {
    return (
      <div className="commit-detail-panel">
        <button
          type="button"
          className="commit-detail-back-btn"
          onClick={onBack}
        >
          <ArrowLeft size={14} />
          <span>Back to history</span>
        </button>
        <div className="commit-detail-empty commit-detail-error-state">
          <span>{error}</span>
          <button
            type="button"
            className="sidebar-action-btn compact"
            onClick={handleRetry}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!detail) return null;

  const subject = firstLine(detail.subject || commit.message);

  // ── Individual diff view ──────────────────────────────────────
  if (viewMode === 'individual-diff') {
    const diffText = individualDiff || '';
    const diffLines = diffText.split('\n');

    return (
      <div className="commit-detail-panel">
        {/* Back + navigation header */}
        <div className="commit-detail-header compact">
          <button
            type="button"
            className="commit-detail-back-btn"
            onClick={handleBackToFileList}
          >
            <ArrowLeft size={14} />
            <span>Back to file list</span>
          </button>
        </div>

        {/* File being viewed */}
        <div className="commit-detail-diff-header">
          <FileText size={14} />
          <span className="commit-detail-diff-filename">{individualDiffPath}</span>
        </div>

        {/* Open in workspace button */}
        <div className="commit-detail-actions">
          <button
            type="button"
            className="commit-detail-action-btn"
            onClick={() =>
              handleOpenInWorkspace(
                diffText,
                individualDiffPath || 'diff',
                'diff'
              )
            }
          >
            <ExternalLink size={12} />
            Open in workspace
          </button>
        </div>

        {/* Diff content */}
        {isDiffLoading ? (
          <div className="commit-detail-empty">
            <Loader2 size={18} className="commit-detail-spinner" />
            <span>Loading diff…</span>
          </div>
        ) : diffError ? (
          <div className="commit-detail-empty commit-detail-error-state">
            <span>{diffError}</span>
          </div>
        ) : diffText ? (
          <div className="commit-detail-diff-surface">
            {diffLines.map((line, index) => {
              const lineClass =
                line.startsWith('+++') || line.startsWith('---')
                  ? 'file'
                  : line.startsWith('@@')
                    ? 'hunk'
                    : line.startsWith('+')
                      ? 'add'
                      : line.startsWith('-')
                        ? 'del'
                        : 'context';

              return (
                <div
                  key={`${index}-${line}`}
                  className={`commit-detail-diff-line ${lineClass}`}
                >
                  <span className="commit-detail-diff-line-number">
                    {index + 1}
                  </span>
                  <span className="commit-detail-diff-line-text">
                    {line || ' '}
                  </span>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="commit-detail-empty">
            <span>No diff available</span>
          </div>
        )}
      </div>
    );
  }

  // ── All diffs view ────────────────────────────────────────────
  if (viewMode === 'all-diffs') {
    const diffText = detail.diff || '';
    const diffLines = diffText.split('\n');

    return (
      <div className="commit-detail-panel">
        {/* Back + navigation header */}
        <div className="commit-detail-header compact">
          <button
            type="button"
            className="commit-detail-back-btn"
            onClick={handleBackToFileList}
          >
            <ArrowLeft size={14} />
            <span>Back to file list</span>
          </button>
        </div>

        {/* Open in workspace button */}
        <div className="commit-detail-actions">
          <button
            type="button"
            className="commit-detail-action-btn"
            onClick={() =>
              handleOpenInWorkspace(diffText, subject, 'full-diff')
            }
          >
            <ExternalLink size={12} />
            Open in workspace
          </button>
        </div>

        {/* Full diff content */}
        {diffText ? (
          <div className="commit-detail-diff-surface">
            {diffLines.map((line, index) => {
              const lineClass =
                line.startsWith('+++') || line.startsWith('---')
                  ? 'file'
                  : line.startsWith('@@')
                    ? 'hunk'
                    : line.startsWith('+')
                      ? 'add'
                      : line.startsWith('-')
                        ? 'del'
                        : 'context';

              return (
                <div
                  key={`${index}-${line}`}
                  className={`commit-detail-diff-line ${lineClass}`}
                >
                  <span className="commit-detail-diff-line-number">
                    {index + 1}
                  </span>
                  <span className="commit-detail-diff-line-text">
                    {line || ' '}
                  </span>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="commit-detail-empty">
            <span>No diff available</span>
          </div>
        )}
      </div>
    );
  }

  // ── File list view (default) ──────────────────────────────────
  const fileList = detail.files || [];

  return (
    <div className="commit-detail-panel">
      {/* Header */}
      <div className="commit-detail-header">
        <button
          type="button"
          className="commit-detail-back-btn"
          onClick={onBack}
        >
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

        <div className="commit-detail-subject">{subject}</div>

        {detail.ref_names && (
          <div className="commit-detail-refs">{detail.ref_names}</div>
        )}

        {detail.stats && (
          <div className="commit-detail-stats">{detail.stats}</div>
        )}
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
        <div className="commit-detail-file-list">
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
                <span
                  className={`commit-detail-file-status ${statusInfo.className}`}
                >
                  {statusInfo.label}
                </span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default CommitDetailPanel;
