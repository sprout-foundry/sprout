/**
 * "New project" in the hosted editor: a new GitHub repository in the user's
 * account, opened in the editor once it exists.
 */

import { FolderPlus } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent, type ReactElement } from 'react';
import { useHost } from '../../host/useHost';
import { openHome } from '../../services/homeView';
import '../ThemedDialog.css';

const NAME_PATTERN = /^[A-Za-z0-9._-]{1,100}$/;

/** A platform error code, read generically off a thrown error (`code`). */
function errorCode(err: unknown): string | undefined {
  return (err as { code?: string } | null)?.code;
}

/** GitHub turns anything outside its name alphabet into "-"; do that as the user types. */
export function toRepoName(input: string): string {
  return input.replace(/\s+/g, '-').replace(/[^A-Za-z0-9._-]/g, '');
}

interface NewProjectDialogProps {
  onClose: () => void;
  onCreated: (htmlURL: string) => void;
}

export default function NewProjectDialog({ onClose, onCreated }: NewProjectDialogProps): ReactElement {
  const host = useHost();
  const hostGithub = host.github;
  const [name, setName] = useState('');
  const [isPrivate, setPrivate] = useState(true);
  const [connected, setConnected] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let live = true;
    hostGithub
      ?.isConnected()
      .then((ok) => live && setConnected(ok))
      .catch(() => live && setConnected(true));
    return () => {
      live = false;
    };
  }, [hostGithub]);

  useEffect(() => {
    if (connected) inputRef.current?.focus();
  }, [connected]);

  const connectGitHub = () => {
    onClose();
    openHome('/settings');
  };

  const valid = NAME_PATTERN.test(name) && name !== '.' && name !== '..';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!valid || busy) return;
    if (!hostGithub?.createRepo) {
      setError('Creating a repository is not supported by this host.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const url = await hostGithub.createRepo({ name, private: isPrivate });
      onCreated(url);
    } catch (err) {
      if (errorCode(err) === 'github_not_connected') {
        setConnected(false);
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
      setBusy(false);
    }
  };

  return (
    <div
      className="themed-dialog-overlay"
      onClick={busy ? undefined : onClose}
      onKeyDown={(e) => e.key === 'Escape' && !busy && onClose()}
    >
      <form
        className="themed-dialog-card new-project-dialog"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
        aria-label="New project"
        data-testid="new-project-dialog"
      >
        <div className="themed-dialog-accent-bar themed-dialog-accent-bar--info" />
        <div className="themed-dialog-header">
          <span className="themed-dialog-icon themed-dialog-icon--info">
            <FolderPlus size={16} />
          </span>
          <h2 className="themed-dialog-title">New project</h2>
        </div>
        {connected === false ? (
          <>
            <div className="themed-dialog-body">
              New projects are GitHub repositories in your account. Connect GitHub to create one.
            </div>
            <div className="themed-dialog-footer">
              <button type="button" className="themed-dialog-btn" onClick={onClose}>
                Cancel
              </button>
              <button type="button" className="themed-dialog-btn themed-dialog-btn--primary" onClick={connectGitHub}>
                Connect GitHub
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="themed-dialog-body">Creates a GitHub repository in your account and opens it here.</div>
            <div className="themed-dialog-input-row">
              <input
                ref={inputRef}
                type="text"
                className="themed-dialog-input"
                aria-label="Repository name"
                placeholder="my-project"
                value={name}
                maxLength={100}
                disabled={busy || connected === null}
                onChange={(e) => {
                  setName(toRepoName(e.target.value));
                  setError(null);
                }}
              />
            </div>
            <fieldset className="new-project-visibility" disabled={busy}>
              <label>
                <input type="radio" name="visibility" checked={isPrivate} onChange={() => setPrivate(true)} />
                Private
              </label>
              <label>
                <input type="radio" name="visibility" checked={!isPrivate} onChange={() => setPrivate(false)} />
                Public
              </label>
            </fieldset>
            {error && (
              <div className="new-project-error" role="alert">
                {error}
              </div>
            )}
            <div className="themed-dialog-footer">
              <button type="button" className="themed-dialog-btn" onClick={onClose} disabled={busy}>
                Cancel
              </button>
              <button
                type="submit"
                className="themed-dialog-btn themed-dialog-btn--primary"
                disabled={!valid || busy || connected === null}
              >
                {busy ? 'Creating…' : 'Create'}
              </button>
            </div>
          </>
        )}
      </form>
    </div>
  );
}
