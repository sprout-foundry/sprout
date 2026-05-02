import { GitBranch } from 'lucide-react';

interface ChatHeaderProps {
  worktreePath?: string;
}

export function ChatHeader({ worktreePath }: ChatHeaderProps): JSX.Element | null {
  if (!worktreePath) return null;
  return (
    <div className="worktree-indicator">
      <div className="worktree-indicator-content">
        <div className="worktree-indicator-icon">
          <GitBranch size={14} />
        </div>
        <span className="worktree-indicator-text" title={worktreePath}>
          Worktree: {worktreePath.split('/').pop()}
        </span>
      </div>
    </div>
  );
}
