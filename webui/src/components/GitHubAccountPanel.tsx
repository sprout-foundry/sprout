/**
 * GitHubAccountPanel — sign-in / account card for GitHub.
 *
 * Shared by the repo picker modal (GitHubRepoPicker) and the settings
 * subsection (settings/GitHubSettingsTab) so both offer the identical
 * sign-in flow: paste a PAT → validate against api.github.com → persist it
 * under localStorage('github_pat') (the agentGitTools.ts compat key, so
 * agent git_push / git_pull start working immediately) → cache the profile
 * under 'github_user'.
 *
 * The token is held in component state only while typing; it is never logged
 * or rendered back.
 */

import { LogOut, Loader2 } from 'lucide-react';
import { useState } from 'react';
import type { FormEvent, ReactElement } from 'react';
import './GitHubAccountPanel.css';
import { GITHUB_TOKENS_URL, clearGitHubAccount, storeToken, storeUser, validateToken } from '../services/githubService';
import type { GitHubUser } from '../services/githubService';

export interface GitHubAccountPanelProps {
  /** Signed-in user, or null. */
  user: GitHubUser | null;
  /** Called after a successful sign-in with the validated profile. */
  onSignedIn: (user: GitHubUser) => void;
  /** Called after sign-out (both localStorage keys are already cleared). */
  onSignedOut: () => void;
  /** Compact variant for the settings tab. */
  compact?: boolean;
}

export default function GitHubAccountPanel({
  user,
  onSignedIn,
  onSignedOut,
  compact = false,
}: GitHubAccountPanelProps): ReactElement {
  const [token, setToken] = useState('');
  const [signingIn, setSigningIn] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSignIn = async (e: FormEvent) => {
    e.preventDefault();
    if (!token.trim() || signingIn) return;

    setSigningIn(true);
    setError(null);
    try {
      const validated = await validateToken(token);
      storeToken(token.trim());
      storeUser(validated);
      setToken('');
      onSignedIn(validated);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSigningIn(false);
    }
  };

  const handleSignOut = () => {
    clearGitHubAccount();
    setToken('');
    setError(null);
    onSignedOut();
  };

  /* ── Signed in: account card ─────────────────────────────────── */

  if (user) {
    return (
      <div className={`gh-account-card ${compact ? 'gh-account-card--compact' : ''}`} data-testid="gh-account-card">
        <img className="gh-account-avatar" src={user.avatar_url} alt="" loading="lazy" />
        <div className="gh-account-info">
          <span className="gh-account-name">{user.name || user.login}</span>
          <a
            className="gh-account-login"
            href={user.html_url}
            target="_blank"
            rel="noreferrer"
            title={`github.com/${user.login}`}
          >
            @{user.login}
          </a>
        </div>
        <button
          type="button"
          className="gh-account-signout"
          onClick={handleSignOut}
          title="Sign out (removes the stored GitHub token)"
          data-testid="gh-signout-btn"
        >
          <LogOut size={14} />
          <span>Sign out</span>
        </button>
      </div>
    );
  }

  /* ── Signed out: PAT form ────────────────────────────────────── */

  return (
    <form className="gh-signin" onSubmit={handleSignIn} data-testid="gh-signin-form">
      <p className="gh-signin-lead">
        Sign in with a GitHub personal access token to browse and clone your repositories — including private ones.
      </p>

      <input
        type="password"
        className="gh-signin-input"
        value={token}
        onChange={(e) => setToken(e.target.value)}
        placeholder="ghp_… or github_pat_…"
        aria-label="GitHub personal access token"
        autoComplete="off"
        spellCheck={false}
        disabled={signingIn}
        data-testid="gh-signin-input"
      />

      <button
        type="submit"
        className="gh-signin-submit"
        disabled={signingIn || !token.trim()}
        data-testid="gh-signin-submit"
      >
        {signingIn ? <Loader2 size={14} className="spin" /> : null}
        {signingIn ? 'Signing in…' : 'Sign in'}
      </button>

      {error && (
        <p className="gh-signin-error" role="alert" data-testid="gh-signin-error">
          {error}
        </p>
      )}

      <p className="gh-signin-hint">
        Create a token at{' '}
        <a href={GITHUB_TOKENS_URL} target="_blank" rel="noreferrer">
          github.com/settings/tokens
        </a>
        . Fine-grained tokens need <strong>repo read access</strong> (Contents: Read-only) for private clones; the
        classic <code>repo</code> scope also works.
      </p>
    </form>
  );
}
