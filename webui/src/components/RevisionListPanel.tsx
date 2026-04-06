import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown, ChevronRight, RotateCcw } from 'lucide-react';
import { showThemedConfirm } from './ThemedDialog';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';

interface RevisionFile {
  file_revision_hash?: string;
  path: string;
  operation: string;
  lines_added: number;
  lines_deleted: number;
}

interface Revision {
  revision_id: string;
  timestamp: string;
  files: RevisionFile[];
  description: string;
}

interface RevisionDetailFile extends RevisionFile {
  diff: string;
}

interface RevisionListPanelProps {
  mode: 'global' | 'session';
  onOpenDiff: (options: { path: string; diff: string; title: string }) => void;
  allowRollback?: boolean;
}

const normalizeRevision = (raw: unknown): Revision => {
  const rec: Record<string, unknown> = raw != null && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};
  const files = Array.isArray(rec.files)
    ? (rec.files as unknown[]).map((file): RevisionFile => {
        const f = (file as Record<string, unknown>) ?? {};
        return {
          file_revision_hash: typeof f.file_revision_hash === 'string' ? f.file_revision_hash : undefined,
          path: typeof f.path === 'string' ? f.path : 'Unknown',
          operation: typeof f.operation === 'string' ? f.operation : 'edited',
          lines_added: Number(f.lines_added || 0),
          lines_deleted: Number(f.lines_deleted || 0),
        };
      })
    : [];

  return {
    revision_id: typeof rec.revision_id === 'string' ? rec.revision_id : 'unknown',
    timestamp: typeof rec.timestamp === 'string' ? rec.timestamp : new Date().toISOString(),
    files,
    description: typeof rec.description === 'string' ? rec.description : '',
  };
};

const buildRevisionFileKey = (file: RevisionFile, index: number): string =>
  `${file.file_revision_hash || file.path}::${index}`;

const formatRelativeTime = (timestamp: string) => {
  const delta = Date.now() - new Date(timestamp).getTime();
  const mins = Math.floor(delta / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
};

const getOperationText = (operation: string) => {
  switch (operation) {
    case 'create':
      return 'Create';
    case 'delete':
      return 'Delete';
    case 'rename':
      return 'Rename';
    case 'rollback':
      return 'Rollback';
    default:
      return 'Edit';
  }
};

function RevisionListPanel({ mode, onOpenDiff, allowRollback = false }: RevisionListPanelProps): JSX.Element {
  const apiService = ApiService.getInstance();
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [revisionDetailsById, setRevisionDetailsById] = useState<Record<string, Record<string, string>>>({});
  const [loadingById, setLoadingById] = useState<Record<string, boolean>>({});
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const detailAbortRef = useRef<AbortController | null>(null);

  // Abort pending requests on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      detailAbortRef.current?.abort();
    };
  }, []);

  const loadRevisions = useCallback(async () => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const { signal } = controller;

    setIsLoading(true);
    setError(null);
    try {
      let rawItems: unknown[] = [];
      if (mode === 'global') {
        const response = await apiService.getChangelog();
        if (signal.aborted) return;
        rawItems = response.revisions || [];
      } else {
        const response = await apiService.getChanges();
        if (signal.aborted) return;
        rawItems = response.changes || [];
      }
      if (signal.aborted) return;
      const normalized = (rawItems || []).map(normalizeRevision).sort((a, b) => {
        return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
      });
      setRevisions(normalized);
      setExpandedRevisionIds(normalized.length > 0 ? new Set([normalized[0].revision_id]) : new Set());
    } catch (loadError) {
      if (signal.aborted) return;
      debugLog('Failed to load revisions:', loadError);
      setError(loadError instanceof Error ? loadError.message : 'Failed to load revisions');
    } finally {
      if (!signal.aborted) {
        setIsLoading(false);
      }
    }
  }, [apiService, mode]);

  useEffect(() => {
    loadRevisions();
  }, [loadRevisions]);

  const loadRevisionDetails = useCallback(
    async (revisionId: string) => {
      if (revisionDetailsById[revisionId] || loadingById[revisionId]) {
        return;
      }
      // Cancel any pending detail load for this revision
      detailAbortRef.current?.abort();
      const controller = new AbortController();
      detailAbortRef.current = controller;
      const { signal } = controller;

      setLoadingById((prev) => ({ ...prev, [revisionId]: true }));
      try {
        const response = await apiService.getRevisionDetails(revisionId);
        if (signal.aborted) return;
        const detailMap: Record<string, string> = {};
        (response.revision?.files || []).forEach((file: RevisionDetailFile, index: number) => {
          detailMap[buildRevisionFileKey(file, index)] = file.diff || '';
        });
        setRevisionDetailsById((prev) => ({ ...prev, [revisionId]: detailMap }));
      } finally {
        if (!signal.aborted) {
          setLoadingById((prev) => ({ ...prev, [revisionId]: false }));
        }
      }
    },
    [apiService, loadingById, revisionDetailsById],
  );

  const toggleRevision = useCallback(
    (revisionId: string) => {
      setExpandedRevisionIds((prev) => {
        const next = new Set(prev);
        if (next.has(revisionId)) {
          next.delete(revisionId);
        } else {
          next.add(revisionId);
          loadRevisionDetails(revisionId);
        }
        return next;
      });
    },
    [loadRevisionDetails],
  );

  const openFileDiff = useCallback(
    async (revisionId: string, file: RevisionFile, index: number) => {
      let detailMap: Record<string, string> | undefined;
      if (!revisionDetailsById[revisionId]) {
        const response = await apiService.getRevisionDetails(revisionId);
        const newMap: Record<string, string> = {};
        (response.revision?.files || []).forEach((detailFile: RevisionDetailFile, detailIndex: number) => {
          newMap[buildRevisionFileKey(detailFile, detailIndex)] = detailFile.diff || '';
        });
        setRevisionDetailsById((prev) => ({ ...prev, [revisionId]: newMap }));
        detailMap = newMap;
      } else {
        detailMap = revisionDetailsById[revisionId];
      }

      const key = buildRevisionFileKey(file, index);
      const diff = detailMap?.[key];
      if (!diff) {
        return;
      }

      onOpenDiff({
        path: file.path,
        diff,
        title: `${mode === 'global' ? 'Revision Diff' : 'Session Change'}`,
      });
    },
    [apiService, mode, onOpenDiff, revisionDetailsById],
  );

  const summaries = useMemo(
    () =>
      revisions.map((revision) => ({
        id: revision.revision_id,
        fileCount: revision.files.length,
        additions: revision.files.reduce((sum, file) => sum + Math.max(0, file.lines_added || 0), 0),
        deletions: revision.files.reduce((sum, file) => sum + Math.max(0, file.lines_deleted || 0), 0),
      })),
    [revisions],
  );

  return (
    <div className="context-panel-tools-list">
      <div className="history-toolbar">
        <button className="history-refresh-btn" onClick={loadRevisions} disabled={isLoading}>
          <RotateCcw size={12} /> Refresh
        </button>
      </div>
      {error ? <div className="history-error-inline">{error}</div> : null}
      {isLoading ? <div className="context-panel-empty">Loading…</div> : null}
      {!isLoading && revisions.length === 0 ? <div className="context-panel-empty">No changes found.</div> : null}
      {!isLoading &&
        revisions.map((revision, index) => {
          const summary = summaries[index];
          const isExpanded = expandedRevisionIds.has(revision.revision_id);
          return (
            <div key={revision.revision_id} className="history-item">
              <button
                className="history-summary"
                onClick={() => toggleRevision(revision.revision_id)}
                aria-expanded={isExpanded}
              >
                <span className="history-expand">
                  {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                </span>
                <span className="history-main">
                  <span className="history-id">{revision.revision_id}</span>
                  <span className="history-time">{formatRelativeTime(revision.timestamp)}</span>
                </span>
                <span className="history-stats">
                  <span>{summary.fileCount} files</span>
                  {summary.additions > 0 && <span className="additions">+{summary.additions}</span>}
                  {summary.deletions > 0 && <span className="deletions">-{summary.deletions}</span>}
                </span>
              </button>
              {isExpanded && (
                <div className="history-details">
                  {revision.files.map((file, fileIndex) => (
                    <button
                      key={`${revision.revision_id}-${file.path}-${fileIndex}`}
                      className="history-file-row history-file-row-interactive"
                      onClick={() => openFileDiff(revision.revision_id, file, fileIndex)}
                    >
                      <span className="history-file-op">{getOperationText(file.operation)}</span>
                      <span className="history-file-path" title={file.path}>
                        {file.path}
                      </span>
                      <span className="history-file-diff">
                        {file.lines_added > 0 && <span className="additions">+{file.lines_added}</span>}
                        {file.lines_deleted > 0 && <span className="deletions">-{file.lines_deleted}</span>}
                      </span>
                    </button>
                  ))}
                  {allowRollback && (
                    <button
                      className="history-rollback-btn"
                      onClick={async () => {
                        if (
                          !(await showThemedConfirm(`Rollback to revision ${revision.revision_id}?`, {
                            title: 'Confirm Rollback',
                            type: 'danger',
                          }))
                        ) {
                          return;
                        }
                        await apiService.rollbackToRevision(revision.revision_id);
                        await loadRevisions();
                      }}
                    >
                      <RotateCcw size={12} /> Rollback to this revision
                    </button>
                  )}
                </div>
              )}
            </div>
          );
        })}
    </div>
  );
};

export default RevisionListPanel;
