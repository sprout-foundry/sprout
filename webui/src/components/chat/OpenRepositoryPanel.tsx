import { FolderGit2 } from 'lucide-react';
import { useState } from 'react';
import type { FormEvent, ReactElement } from 'react';
import { searchForRepo } from '../../services/homeView';
import { parseRepoRef } from '../../services/workspaceFs/workspaceGit';
import GitHubRepoPicker from '../GitHubRepoPicker';
import NewProjectDialog from '../layered/NewProjectDialog';
import SproutLogo from '../SproutLogo';
import './OpenRepositoryPanel.css';

/**
 * Empty state for the hosted editor with no repository open: name one to
 * open (reloading onto ?repo= runs the same clone as a dashboard link), start
 * a new one, or pick from the account's repositories.
 */
export function OpenRepositoryPanel(): ReactElement {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [picking, setPicking] = useState(false);
  const openRepo = (url: string) => {
    window.location.search = searchForRepo(url);
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    try {
      const { url } = parseRepoRef(value.trim());
      window.location.search = searchForRepo(url.replace(/\.git$/, ''));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="welcome-message open-repo-panel" data-testid="chat-open-repo">
      <div className="welcome-icon">
        <SproutLogo showWordmark={false} />
      </div>
      <div className="welcome-text">Open a repository to get started</div>
      <div className="welcome-hint">
        The agent works on the repository you open. Public GitHub repositories work without connecting an account.
      </div>
      <form className="open-repo-form" onSubmit={submit}>
        <input
          className="open-repo-input"
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setError(null);
          }}
          placeholder="owner/repo or a GitHub URL"
          aria-label="Repository to open"
          aria-invalid={error ? true : undefined}
          data-testid="open-repo-input"
        />
        <button type="submit" className="provider-setup-btn" disabled={!value.trim()} data-testid="open-repo-submit">
          <FolderGit2 size={14} />
          Open
        </button>
      </form>
      {error && (
        <div className="open-repo-error" role="alert">
          {error}
        </div>
      )}
      <div className="open-repo-alternatives">
        <button type="button" className="open-repo-dashboard" onClick={() => setCreating(true)}>
          Start a new project
        </button>
        <span aria-hidden="true">·</span>
        <button type="button" className="open-repo-dashboard" onClick={() => setPicking(true)}>
          Choose from your repositories
        </button>
      </div>
      {creating && <NewProjectDialog onClose={() => setCreating(false)} onCreated={openRepo} />}
      {picking && <GitHubRepoPicker isOpen onClose={() => setPicking(false)} onSelect={openRepo} />}
    </div>
  );
}
