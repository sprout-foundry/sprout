/**
 * DiffViewer — Unified diff viewer for a single commit.
 *
 * Fetches changed files via gitClient.getChangedFiles, loads content
 * from both the commit and its parent, and renders a line-level diff.
 */

import React, { useState, useEffect, useCallback } from 'react';
import { X, ChevronDown, ChevronRight, Loader2, FileCode, AlertCircle } from 'lucide-react';
import { gitClient } from '../../services/gitClient';
import './DiffViewer.css';

interface DiffViewerProps {
  repoDir: string;
  sha: string;
  onClose: () => void;
}

interface FileDiff {
  filepath: string;
  type: 'added' | 'deleted' | 'modified';
  lines: DiffLine[];
}

interface DiffLine {
  type: 'add' | 'del' | 'ctx' | 'header';
  content: string;
  oldLine?: number;
  newLine?: number;
}

function computeDiff(oldContent: string, newContent: string): DiffLine[] {
  const oldLines = oldContent.split('\n');
  const newLines = newContent.split('\n');

  // Simple LCS-based diff
  const oldLen = oldLines.length;
  const newLen = newLines.length;
  const dp: number[][] = Array.from({ length: oldLen + 1 }, () => new Array(newLen + 1).fill(0));

  for (let i = 1; i <= oldLen; i++) {
    for (let j = 1; j <= newLen; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  // Backtrack to produce diff
  const result: DiffLine[] = [];
  let i = oldLen;
  let j = newLen;
  const temp: DiffLine[] = [];

  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      temp.push({ type: 'ctx', content: oldLines[i - 1], oldLine: i, newLine: j });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      temp.push({ type: 'add', content: newLines[j - 1], newLine: j });
      j--;
    } else {
      temp.push({ type: 'del', content: oldLines[i - 1], oldLine: i });
      i--;
    }
  }

  // Reverse to get correct order
  for (let k = temp.length - 1; k >= 0; k--) {
    result.push(temp[k]);
  }

  return result;
}

const DiffViewer: React.FC<DiffViewerProps> = ({ repoDir, sha, onClose }) => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [fileDiffs, setFileDiffs] = useState<FileDiff[]>([]);
  const [expandedFiles, setExpandedFiles] = useState<Set<string>>(new Set());

  const loadDiff = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const log = await gitClient.log(repoDir, { depth: 1, ref: sha });
      if (log.length === 0) {
        setError('Commit not found');
        return;
      }
      const commit = log[0];
      const parentSha = commit.commit.parent[0];
      const changed = await gitClient.getChangedFiles(repoDir, sha, parentSha);

      const diffs: FileDiff[] = [];
      for (const file of changed) {
        const oldContent = parentSha
          ? (await gitClient.readFileAtCommit(repoDir, file.filepath, parentSha)) ?? ''
          : '';
        const newContent = (await gitClient.readFileAtCommit(repoDir, file.filepath, sha)) ?? '';
        const lines = computeDiff(oldContent, newContent);
        diffs.push({
          filepath: file.filepath,
          type: file.type,
          lines,
        });
      }

      setFileDiffs(diffs);

      // Auto-expand first file
      if (diffs.length > 0) {
        setExpandedFiles(new Set([diffs[0].filepath]));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load diff');
    } finally {
      setLoading(false);
    }
  }, [repoDir, sha]);

  useEffect(() => {
    loadDiff();
  }, [loadDiff]);

  const toggleFile = (filepath: string) => {
    setExpandedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(filepath)) next.delete(filepath);
      else next.add(filepath);
      return next;
    });
  };

  const addedLines = fileDiffs.reduce((sum, f) => sum + f.lines.filter((l) => l.type === 'add').length, 0);
  const removedLines = fileDiffs.reduce((sum, f) => sum + f.lines.filter((l) => l.type === 'del').length, 0);

  return (
    <div className="diff-viewer">
      <div className="diff-viewer-header">
        <div className="diff-viewer-title">
          <FileCode size={16} />
          <span>
            Diff — <code>{sha.slice(0, 7)}</code>
          </span>
        </div>
        <div className="diff-viewer-stats">
          <span className="diff-stat-added">+{addedLines}</span>
          <span className="diff-stat-removed">-{removedLines}</span>
          <span className="diff-stat-files">{fileDiffs.length} files</span>
        </div>
        <button className="btn btn-sm btn-ghost" onClick={onClose}>
          <X size={14} />
        </button>
      </div>

      {loading && (
        <div className="diff-viewer-loading">
          <Loader2 size={16} className="spinner" /> Loading diff…
        </div>
      )}

      {error && (
        <div className="diff-viewer-error">
          <AlertCircle size={14} /> {error}
        </div>
      )}

      {!loading && !error && (
        <div className="diff-viewer-files">
          {fileDiffs.map((file) => {
            const isExpanded = expandedFiles.has(file.filepath);
            const fileAdds = file.lines.filter((l) => l.type === 'add').length;
            const fileDels = file.lines.filter((l) => l.type === 'del').length;

            return (
              <div key={file.filepath} className={`diff-file ${isExpanded ? 'expanded' : ''}`}>
                <div
                  className="diff-file-header"
                  onClick={() => toggleFile(file.filepath)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      toggleFile(file.filepath);
                    }
                  }}
                >
                  {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  <span className={`diff-file-status diff-file-status--${file.type}`}>
                    {file.type === 'added' ? 'A' : file.type === 'deleted' ? 'D' : 'M'}
                  </span>
                  <span className="diff-file-path">{file.filepath}</span>
                  <span className="diff-file-stats">
                    {fileAdds > 0 && <span className="diff-stat-added">+{fileAdds}</span>}
                    {fileDels > 0 && <span className="diff-stat-removed">-{fileDels}</span>}
                  </span>
                </div>

                {isExpanded && (
                  <div className="diff-file-content">
                    <div className="diff-lines">
                      {file.lines.map((line, idx) => {
                        const lineNum =
                          line.type === 'add'
                            ? `+${line.newLine}`
                            : line.type === 'del'
                              ? `-${line.oldLine}`
                              : `${line.oldLine || ''}`;
                        const lineNumRight =
                          line.type === 'add'
                            ? `${line.newLine}`
                            : line.type === 'del'
                              ? ''
                              : `${line.newLine || ''}`;

                        return (
                          <div key={idx} className={`diff-line diff-line--${line.type}`}>
                            <span className="diff-line-num">{lineNum || ' '}</span>
                            <span className="diff-line-num diff-line-num-right">{lineNumRight || ' '}</span>
                            <span className="diff-line-content">{line.content}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default DiffViewer;