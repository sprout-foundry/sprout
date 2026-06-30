import { ShieldCheck, Sparkles } from 'lucide-react';

export interface GitCommitBoxProps {
  commitMessage: string;
  hasStagedFiles: boolean;
  isActing: boolean;
  isGeneratingCommitMessage: boolean;
  isReviewLoading: boolean;
  onCommitMessageChange: (value: string) => void;
  onGenerateCommitMessage: () => void;
  onCommit: () => void;
  onRunReview: () => void;
}

function GitCommitBox({
  commitMessage,
  hasStagedFiles,
  isActing,
  isGeneratingCommitMessage,
  isReviewLoading,
  onCommitMessageChange,
  onGenerateCommitMessage,
  onCommit,
  onRunReview,
}: GitCommitBoxProps) {
  return (
    <div className="git-sidebar-commit-box">
      <div className="git-sidebar-commit-header">
        <h4>Commit Message</h4>
        <button
          className="git-generate-icon-btn"
          onClick={onGenerateCommitMessage}
          disabled={!hasStagedFiles || isGeneratingCommitMessage || isActing}
          title="Generate commit message with AI"
          aria-label="Generate commit message"
        >
          <Sparkles size={14} />
        </button>
      </div>
      <textarea
        value={commitMessage}
        onChange={(e) => onCommitMessageChange(e.target.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && hasStagedFiles && commitMessage.trim() && !isActing) {
            e.preventDefault();
            onCommit();
          }
        }}
        disabled={!hasStagedFiles || isActing}
        placeholder={
          hasStagedFiles ? 'Write commit message… (⌘/Ctrl+Enter to commit)' : 'Stage files to write a commit message'
        }
        aria-label="Commit message"
        className="git-sidebar-commit-input"
        rows={3}
      />
      <div className="git-sidebar-primary-actions">
        <button
          className="sidebar-action-btn primary"
          onClick={onCommit}
          disabled={!hasStagedFiles || !commitMessage.trim() || isActing}
        >
          Commit Changes
        </button>
        <button
          className="sidebar-action-btn"
          onClick={onRunReview}
          disabled={!hasStagedFiles || isReviewLoading || isActing}
        >
          <ShieldCheck size={14} />
          {isReviewLoading ? 'Reviewing…' : 'Review'}
        </button>
      </div>
    </div>
  );
}

export default GitCommitBox;
