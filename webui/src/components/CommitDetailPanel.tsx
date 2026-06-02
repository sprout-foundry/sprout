import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  FileText,
  GitCompareArrows,
  Loader2,
  Clock,
  FolderOpen,
} from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import type { MutableRefObject } from 'react';
import type { GitCommitSummary, GitCommitDetail } from '../types/git-types';
import { formatRelativeDate, formatAbsoluteDate, firstLine } from '../utils/format';
import { getStatusInfo } from '../utils/git';
import { useLog } from '../utils/log';
import './CommitDetailPanel.css';

interface CommitDetailPanelProps {
  onLoadCommitDetail: (hash: string) => Promise<GitCommitDetail>;
  onLoadCommitFileDiff: (
    hash: string,
    filePath: string,
  ) => Promise<{ message: string; hash: string; path: string; diff: string }>;
  commit: GitCommitSummary;
  onBack: () => void;
  /** Optional siblings for prev/next navigation. */
  prevCommit?: GitCommitSummary | null;
  nextCommit?: GitCommitSummary | null;
  onSelectCommit?: (commit: GitCommitSummary) => void;
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

function CommitDetailPanel({
  onLoadCommitDetail,
  onLoadCommitFileDiff,
  commit,
  onBack,
  prevCommit,
  nextCommit,
  onSelectCommit,
  openWorkspaceBuffer,
}: CommitDetailPanelProps): JSX.Element | null {
  const log = useLog();
  const [detail, setDetail] = useState<GitCommitDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const fetchCommitDetail = useCallback(
    (cancelledRef?: MutableRefObject<boolean>) => {
      setIsLoading(true);
      setError(null);

      onLoadCommitDetail(commit.hash)
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
    [onLoadCommitDetail, commit.hash],
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
        const result = await onLoadCommitFileDiff(commit.hash, filePath);
        // Open the workspace buffer even when the diff comes back empty.
        // `git show -- <path>` returns no patch for binary files, no-op
        // entries (renames with no content change), and a few edge cases
        // around merge commits. Logging an error and bailing out made the
        // file row look broken — the user clicked, nothing opened, and an
        // "error" toast appeared with no explanation. Surfacing the empty
        // state inside the buffer is clearer and still lets the user
        // inspect the buffer header to confirm the file/commit pair.
        const diff = result?.diff ?? '';
        const content =
          diff.trim().length > 0
            ? diff
            : `# No textual diff available for this file in this commit.\n# Common reasons:\n#   - The file is binary (image, archive, compiled blob)\n#   - The change is a pure rename with identical content\n#   - The commit is a merge that did not modify this path\n`;
        openWorkspaceBuffer({
          kind: 'diff',
          path: `__workspace/commit/${commit.short_hash}/${filePath}`,
          title: `${commit.short_hash}: ${filePath}`,
          ext: '.diff',
          content,
          metadata: {
            sourcePath: `commit:${commit.hash}:${filePath}`,
            diffContent: content,
            modeOptions: ['combined'],
          },
        });
      } catch (err) {
        log.error(`Failed to load file diff: ${err instanceof Error ? err.message : 'Unknown error'}`, {
          title: 'Git Error',
        });
      }
    },
    [commit.hash, commit.short_hash, onLoadCommitFileDiff, openWorkspaceBuffer, log],
  );

  const handleViewAllDiffs = useCallback(() => {
    if (!detail?.diff || detail.diff.trim() === '') {
      log.error('No diff content available for this commit', { title: 'Git Error' });
      return;
    }
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
        modeOptions: ['combined'],
      },
    });
  }, [detail, commit, openWorkspaceBuffer, log]);

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
  const fullMessage = detail.subject || commit.message || '';
  const subject = firstLine(fullMessage);
  const bodyStart = fullMessage.indexOf('\n');
  const body = bodyStart >= 0 ? fullMessage.slice(bodyStart + 1).replace(/^\n+/, '').trimEnd() : '';

  return (
    <div className="commit-detail-panel">
      {/* Header */}
      <div className="commit-detail-header">
        <div className="commit-detail-nav-row">
          <button type="button" className="commit-detail-back-btn" onClick={onBack}>
            <ArrowLeft size={14} />
            <span>Back to history</span>
          </button>
          {(prevCommit || nextCommit) && onSelectCommit && (
            <div className="commit-detail-prev-next">
              <button
                type="button"
                className="commit-detail-nav-btn"
                onClick={() => prevCommit && onSelectCommit(prevCommit)}
                disabled={!prevCommit}
                title={prevCommit ? `Newer: ${prevCommit.short_hash} ${firstLine(prevCommit.message)}` : 'Newest commit'}
                aria-label="Previous (newer) commit"
              >
                <ChevronLeft size={14} />
              </button>
              <button
                type="button"
                className="commit-detail-nav-btn"
                onClick={() => nextCommit && onSelectCommit(nextCommit)}
                disabled={!nextCommit}
                title={nextCommit ? `Older: ${nextCommit.short_hash} ${firstLine(nextCommit.message)}` : 'Oldest commit'}
                aria-label="Next (older) commit"
              >
                <ChevronRight size={14} />
              </button>
            </div>
          )}
        </div>

        <div className="commit-detail-commit-info">
          <span className="commit-detail-hash">{detail.short_hash}</span>
          <span className="commit-detail-author">{detail.author}</span>
          <span className="commit-detail-date" title={formatAbsoluteDate(detail.date)}>
            <Clock size={11} />
            {formatRelativeDate(detail.date)}
          </span>
        </div>

        <div className="commit-detail-subject">{subject}</div>

        {body && <pre className="commit-detail-body">{body}</pre>}

        {detail.ref_names && <div className="commit-detail-refs">{detail.ref_names}</div>}

        {detail.stats && <div className="commit-detail-stats">{detail.stats}</div>}
      </div>

      {/* Actions */}
      <div className="commit-detail-actions">
        <button
          type="button"
          className="commit-detail-action-btn"
          onClick={handleViewAllDiffs}
          disabled={!detail?.diff || detail.diff.trim() === ''}
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
}

export default CommitDetailPanel;
