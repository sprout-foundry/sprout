import React from 'react';
import ReactDOM from 'react-dom/client';
import './ThemedDialog.css';

/* ── Types ───────────────────────────────────────────────────── */

export type FileChangeResult = 'reload' | 'keep-mine' | 'show-diff' | 'ignore';

interface FileChangeDialogProps {
  fileName: string;
  deleted: boolean;
  hasUnsavedChanges: boolean;
  onResolve: (result: FileChangeResult) => void;
}

/* ── Internal dialog component ───────────────────────────────── */

const FileChangeDialog: React.FC<FileChangeDialogProps> = ({ fileName, deleted, hasUnsavedChanges, onResolve }) => {
  const handleKeyDown = React.useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        if (deleted) {
          onResolve('ignore');
        } else {
          onResolve('keep-mine');
        }
      } else if (e.key === 'Enter' && !deleted && !hasUnsavedChanges) {
        e.preventDefault();
        onResolve('reload');
      }
    },
    [onResolve, deleted, hasUnsavedChanges],
  );

  React.useEffect(() => {
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }, []);

  // Deleted + unsaved changes — the user has work that could be lost.
  if (deleted && hasUnsavedChanges) {
    return (
      <div className="themed-dialog-overlay" onClick={() => onResolve('keep-mine')} onKeyDown={handleKeyDown}>
        <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
          <div className="themed-dialog-accent-bar themed-dialog-accent-bar--error" />
          <div className="themed-dialog-header">
            <span className="themed-dialog-icon themed-dialog-icon--error">✕</span>
            <h2 className="themed-dialog-title">File Deleted with Unsaved Changes</h2>
          </div>
          <div className="themed-dialog-body" style={{ whiteSpace: 'pre-line' }}>
            {`This file has been deleted on disk and you have unsaved changes in the editor.\n\n${fileName}\n\nChoose "Keep Mine" to preserve your unsaved edits in the editor tab.\nChoose "Close" to dismiss this notice.`}
          </div>
          <div className="themed-dialog-footer">
            <button type="button" className="themed-dialog-btn" onClick={() => onResolve('keep-mine')} autoFocus>
              Keep Mine
            </button>
            <button
              type="button"
              className="themed-dialog-btn themed-dialog-btn--primary"
              onClick={() => onResolve('ignore')}
            >
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Deleted mode (no unsaved changes)
  if (deleted) {
    return (
      <div className="themed-dialog-overlay" onClick={() => onResolve('ignore')} onKeyDown={handleKeyDown}>
        <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
          <div className="themed-dialog-accent-bar themed-dialog-accent-bar--error" />
          <div className="themed-dialog-header">
            <span className="themed-dialog-icon themed-dialog-icon--error">✕</span>
            <h2 className="themed-dialog-title">File Deleted</h2>
          </div>
          <div
            className="themed-dialog-body"
            style={{ whiteSpace: 'pre-line' }}
          >{`This file has been deleted on disk.\n\n${fileName}`}</div>
          <div className="themed-dialog-footer">
            <button
              type="button"
              className="themed-dialog-btn themed-dialog-btn--primary"
              onClick={() => onResolve('ignore')}
              autoFocus
            >
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Conflict mode (has unsaved changes)
  if (hasUnsavedChanges) {
    return (
      <div className="themed-dialog-overlay" onClick={() => onResolve('keep-mine')} onKeyDown={handleKeyDown}>
        <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
          <div className="themed-dialog-accent-bar themed-dialog-accent-bar--warning" />
          <div className="themed-dialog-header">
            <span className="themed-dialog-icon themed-dialog-icon--warning">⚠</span>
            <h2 className="themed-dialog-title">File Changed Externally</h2>
          </div>
          <div className="themed-dialog-body" style={{ whiteSpace: 'pre-line' }}>
            {`This file has been changed by another process and you have unsaved changes.\n\n${fileName}\n\nHow do you want to resolve this?`}
          </div>
          <div className="themed-dialog-footer">
            <button type="button" className="themed-dialog-btn" onClick={() => onResolve('keep-mine')}>
              Keep Mine
            </button>
            <button type="button" className="themed-dialog-btn" onClick={() => onResolve('show-diff')}>
              Show Diff
            </button>
            <button
              type="button"
              className="themed-dialog-btn themed-dialog-btn--primary"
              onClick={() => onResolve('reload')}
              autoFocus
            >
              Reload from Disk
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Clean mode (no unsaved changes — notification / fallback)
  return (
    <div className="themed-dialog-overlay" onClick={() => onResolve('reload')} onKeyDown={handleKeyDown}>
      <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
        <div className="themed-dialog-accent-bar themed-dialog-accent-bar--warning" />
        <div className="themed-dialog-header">
          <span className="themed-dialog-icon themed-dialog-icon--warning">⚠</span>
          <h2 className="themed-dialog-title">File Changed Externally</h2>
        </div>
        <div className="themed-dialog-body" style={{ whiteSpace: 'pre-line' }}>
          {`This file has been changed by another process.\n\n${fileName}\n\nThe file will be reloaded automatically.`}
        </div>
        <div className="themed-dialog-footer">
          <button
            type="button"
            className="themed-dialog-btn themed-dialog-btn--primary"
            onClick={() => onResolve('reload')}
            autoFocus
          >
            OK
          </button>
        </div>
      </div>
    </div>
  );
};

/* ── Portal helper (same pattern as ThemedDialog) ────────────── */

function mountToBody(element: React.ReactElement): () => void {
  const container = document.createElement('div');
  container.setAttribute('data-file-change-dialog-portal', '');
  document.body.appendChild(container);

  const root = ReactDOM.createRoot(container);
  root.render(element);

  return () => {
    root.unmount();
    if (container.parentNode) {
      container.parentNode.removeChild(container);
    }
  };
}

/* ── Public API ──────────────────────────────────────────────── */

export async function showFileChangeDialog(
  fileName: string,
  options?: { deleted?: boolean; hasUnsavedChanges?: boolean },
): Promise<FileChangeResult> {
  return new Promise<FileChangeResult>((resolve) => {
    const deleted = options?.deleted ?? false;
    const hasUnsavedChanges = options?.hasUnsavedChanges ?? false;
    let cleanup: (() => void) | null = null;

    const dismiss = (result: FileChangeResult) => {
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve(result);
      });
    };

    cleanup = mountToBody(
      <FileChangeDialog
        fileName={fileName}
        deleted={deleted}
        hasUnsavedChanges={hasUnsavedChanges}
        onResolve={dismiss}
      />,
    );
  });
}
