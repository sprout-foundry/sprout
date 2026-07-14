import { ExternalLink } from 'lucide-react';
import { useEffect, useState } from 'react';

export interface GitPRDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onPullRequest: (params: {
    title: string;
    body?: string;
    base?: string;
    head?: string;
    draft?: boolean;
  }) => Promise<{ url: string; number: number; state: string }>;
}

function GitPRDialog({ isOpen, onClose, onPullRequest }: GitPRDialogProps) {
  const [prTitle, setPrTitle] = useState('');
  const [prBody, setPrBody] = useState('');
  const [prBase, setPrBase] = useState('');
  const [prHead, setPrHead] = useState('');
  const [prDraft, setPrDraft] = useState(false);
  const [isCreatingPr, setIsCreatingPr] = useState(false);
  const [prSuccessUrl, setPrSuccessUrl] = useState<string | null>(null);
  const [prError, setPrError] = useState<string | null>(null);

  // Reset form state when dialog is opened
  useEffect(() => {
    if (isOpen) {
      setPrTitle('');
      setPrBody('');
      setPrBase('');
      setPrHead('');
      setPrDraft(false);
      setIsCreatingPr(false);
      setPrSuccessUrl(null);
      setPrError(null);
    }
  }, [isOpen]);

  const handleCreatePr = async () => {
    if (!prTitle.trim()) return;
    setIsCreatingPr(true);
    setPrError(null);
    setPrSuccessUrl(null);
    try {
      const result = await onPullRequest({
        title: prTitle.trim(),
        body: prBody || undefined,
        base: prBase || undefined,
        head: prHead || undefined,
        draft: prDraft,
      });
      setPrSuccessUrl(result.url);
    } catch (err) {
      setPrError(err instanceof Error ? err.message : String(err));
    } finally {
      setIsCreatingPr(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div
      className="themed-dialog-overlay"
      onClick={() => {
        if (!isCreatingPr) onClose();
      }}
    >
      <div className="themed-dialog-card" style={{ width: 'min(500px, 100%)' }} onClick={(e) => e.stopPropagation()}>
        <div className="themed-dialog-accent-bar themed-dialog-accent-bar--info" />
        <div className="themed-dialog-header">
          <span className="themed-dialog-icon themed-dialog-icon--info">
            <ExternalLink size={16} />
          </span>
          <h2 className="themed-dialog-title">Create Pull Request</h2>
        </div>

        {prSuccessUrl ? (
          <>
            <div className="themed-dialog-body" style={{ textAlign: 'center' }}>
              <div style={{ marginBottom: 8, color: 'var(--accent-success)', fontWeight: 600 }}>
                Pull request created!
              </div>
              <a
                href={prSuccessUrl}
                target="_blank"
                rel="noopener noreferrer"
                style={{ color: 'var(--accent-primary)', wordBreak: 'break-all' }}
              >
                {prSuccessUrl}
              </a>
            </div>
            <div className="themed-dialog-footer">
              <button type="button" className="themed-dialog-btn themed-dialog-btn--primary" onClick={() => onClose()}>
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="themed-dialog-body">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <div>
                  <label
                    style={{
                      display: 'block',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--text-tertiary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.06em',
                      marginBottom: 4,
                    }}
                  >
                    Title *
                  </label>
                  <input
                    type="text"
                    className="themed-dialog-input"
                    value={prTitle}
                    onChange={(e) => setPrTitle(e.target.value)}
                    placeholder="PR title"
                    autoFocus
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && prTitle.trim() && !isCreatingPr) {
                        e.preventDefault();
                        handleCreatePr();
                      }
                    }}
                  />
                </div>
                <div>
                  <label
                    style={{
                      display: 'block',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--text-tertiary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.06em',
                      marginBottom: 4,
                    }}
                  >
                    Description
                  </label>
                  <textarea
                    className="themed-dialog-input"
                    value={prBody}
                    onChange={(e) => setPrBody(e.target.value)}
                    placeholder="Optional description…"
                    rows={4}
                    style={{ resize: 'vertical', minHeight: 80, lineHeight: 1.55 }}
                  />
                </div>
                <div style={{ display: 'flex', gap: 12 }}>
                  <div style={{ flex: 1 }}>
                    <label
                      style={{
                        display: 'block',
                        fontSize: 11,
                        fontWeight: 600,
                        color: 'var(--text-tertiary)',
                        textTransform: 'uppercase',
                        letterSpacing: '0.06em',
                        marginBottom: 4,
                      }}
                    >
                      Base branch
                    </label>
                    <input
                      type="text"
                      className="themed-dialog-input"
                      value={prBase}
                      onChange={(e) => setPrBase(e.target.value)}
                      placeholder="main"
                    />
                  </div>
                  <div style={{ flex: 1 }}>
                    <label
                      style={{
                        display: 'block',
                        fontSize: 11,
                        fontWeight: 600,
                        color: 'var(--text-tertiary)',
                        textTransform: 'uppercase',
                        letterSpacing: '0.06em',
                        marginBottom: 4,
                      }}
                    >
                      Head branch
                    </label>
                    <input
                      type="text"
                      className="themed-dialog-input"
                      value={prHead}
                      onChange={(e) => setPrHead(e.target.value)}
                      placeholder="Current branch"
                    />
                  </div>
                </div>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={prDraft}
                    onChange={(e) => setPrDraft(e.target.checked)}
                    style={{ accentColor: 'var(--accent-primary)', width: 16, height: 16 }}
                  />
                  <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>Draft PR</span>
                </label>
              </div>
            </div>

            {prError && (
              <div
                style={{
                  padding: 'var(--space-4) var(--space-4)',
                  color: 'var(--accent-error)',
                  fontSize: 13,
                  background: 'var(--color-error-bg)',
                  borderRadius: 'var(--radius-md)',
                }}
              >
                {prError}
              </div>
            )}

            <div className="themed-dialog-footer">
              <button type="button" className="themed-dialog-btn" onClick={() => onClose()} disabled={isCreatingPr}>
                Cancel
              </button>
              <button
                type="button"
                className="themed-dialog-btn themed-dialog-btn--primary"
                onClick={handleCreatePr}
                disabled={!prTitle.trim() || isCreatingPr}
              >
                {isCreatingPr ? 'Creating…' : 'Create'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

export default GitPRDialog;
