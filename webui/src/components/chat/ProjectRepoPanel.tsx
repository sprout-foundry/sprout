import { FolderGit2, RotateCcw } from 'lucide-react';
import type { ReactElement } from 'react';
import { useSyncExternalStore } from 'react';
import { copy } from '../../config/copy';
import { useHost } from '../../host/useHost';
import {
  getHostProjectRepoStatus,
  retryHostProjectRepo,
  subscribeHostProjectRepoStatus,
  type HostProjectRepoStatus,
} from '../../services/startupRepoImport';
import SproutLogo from '../SproutLogo';
import './OpenRepositoryPanel.css';

/** The host project repo's status, kept current. */
export function useHostProjectRepoStatus(): HostProjectRepoStatus {
  return useSyncExternalStore(subscribeHostProjectRepoStatus, getHostProjectRepoStatus, getHostProjectRepoStatus);
}

/** Whether an open failure reads as a missing or expired git connection. */
function needsGitConnection(error: string): boolean {
  return /\b(401|403)\b|unauthori[sz]ed|forbidden|not connected|reauth|credential|authentication/i.test(error);
}

/**
 * The empty chat of a space bound to a project: its repository is opening,
 * or it could not be opened — said with the reason and a way to fix it, never
 * an offer to open some other repository.
 */
export function ProjectRepoPanel({ status }: { status: NonNullable<HostProjectRepoStatus> }): ReactElement {
  const host = useHost();
  const name =
    status.repo
      .replace(/\.git$/, '')
      .split('/')
      .pop() || status.repo;
  if (status.state !== 'failed') {
    return (
      <div className="welcome-message open-repo-panel" data-testid="chat-project-repo" data-state={status.state}>
        <div className="welcome-icon">
          <SproutLogo showWordmark={false} />
        </div>
        <div className="welcome-text" role="status">
          {copy('chat.projectRepoOpening')}
        </div>
        <div className="welcome-hint">{name}</div>
      </div>
    );
  }
  const error = status.error ?? '';
  const gitFix = needsGitConnection(error);
  const canConnect = gitFix && host.navigation.intentPath?.({ type: 'gitConnections' }) !== null;
  return (
    <div className="welcome-message open-repo-panel" data-testid="chat-project-repo" data-state="failed">
      <div className="welcome-icon">
        <SproutLogo showWordmark={false} />
      </div>
      <div className="welcome-text">{copy('chat.projectRepoFailed')}</div>
      <div className="welcome-hint">{gitFix ? copy('chat.projectRepoNeedsGit') : error}</div>
      <div className="open-repo-alternatives">
        {canConnect && (
          <button
            type="button"
            className="provider-setup-btn"
            onClick={() => void host.navigation.open({ type: 'gitConnections' })}
            data-testid="project-repo-connect"
          >
            <FolderGit2 size={14} />
            {copy('action.connectGit')}
          </button>
        )}
        <button
          type="button"
          className="open-repo-dashboard"
          onClick={retryHostProjectRepo}
          data-testid="project-repo-retry"
        >
          <RotateCcw size={14} aria-hidden="true" /> {copy('action.tryAgain')}
        </button>
      </div>
    </div>
  );
}
