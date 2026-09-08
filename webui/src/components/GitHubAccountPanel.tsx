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

import { LogOut, Loader2, ExternalLink, X } from 'lucide-react';
import { useRef, useState } from 'react';
import type { FormEvent, ReactElement } from 'react';
import './GitHubAccountPanel.css';
import { GITHUB_TOKENS_URL, clearGitHubAccount, storeToken, storeUser, validateToken } from '../services/githubService';
import type { GitHubUser } from '../services/githubService';
import {
  isDeviceFlowAvailable,
  openVerificationPage,
  pollDeviceFlow,
  startDeviceFlow,
} from '../services/githubDeviceFlow';
import type { DeviceFlowSession } from '../services/githubDeviceFlow';
import { showThemedConfirm } from './ThemedDialog';

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
  const [flow, setFlow] = useState<DeviceFlowSession | null>(null);
  const [flowError, setFlowError] = useState<string | null>(null);
  const [showPatForm, setShowPatForm] = useState(false);
  const [startingFlow, setStartingFlow] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const flowGeneration = useRef(0);
  const deviceFlowAvailable = isDeviceFlowAvailable() && !showPatForm;

  const handleDeviceFlowStart = async () => {
    if (startingFlow) return;
    setFlowError(null);
    setError(null);
    setStartingFlow(true);
    const generation = ++flowGeneration.current;
    try {
      const session = await startDeviceFlow();
      if (flowGeneration.current !== generation) return;
      setFlow(session);
      // Best-effort browser open; the user can also tap the link button.
      void openVerificationPage(session.verificationUri).catch(() => undefined);
      void pollLoop(session, generation);
    } catch (err) {
      if (flowGeneration.current === generation) {
        setFlowError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setStartingFlow(false);
    }
  };

  const pollLoop = async (session: DeviceFlowSession, generation: number) => {
    let interval = session.interval;
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, interval));
      if (flowGeneration.current !== generation) return; // cancelled / unmounted
      try {
        const result = await pollDeviceFlow(session);
        if (flowGeneration.current !== generation) return;
        if (result.done && result.user) {
          setFlow(null);
          onSignedIn(result.user);
          return;
        }
        if (result.retryInMs) interval = result.retryInMs;
      } catch (err) {
        setFlow(null);
        setFlowError(err instanceof Error ? err.message : String(err));
        return;
      }
    }
  };

  const handleDeviceFlowCancel = () => {
    flowGeneration.current += 1;
    setFlow(null);
    setFlowError(null);
  };

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

  const handleSignOut = async () => {
    if (signingOut) return;
    // Destructive: this removes the stored PAT, which also revokes the agent's
    // git_push / git_pull credentials (agentGitTools reads the same key).
    const confirmed = await showThemedConfirm(
      'Sign out of GitHub?\n\nThe stored token will be removed from this device. Cloning private repositories and agent git push/pull will stop working until you sign in again.',
      { title: 'Sign out of GitHub', type: 'warning', confirmLabel: 'Sign out' },
    );
    if (!confirmed) return;

    setSigningOut(true);
    try {
      clearGitHubAccount();
      setToken('');
      setError(null);
      onSignedOut();
    } finally {
      setSigningOut(false);
    }
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
          onClick={() => void handleSignOut()}
          title="Sign out (removes the stored GitHub token)"
          disabled={signingOut}
          data-testid="gh-signout-btn"
        >
          {signingOut ? <Loader2 size={14} className="spin" /> : <LogOut size={14} />}
          <span>{signingOut ? 'Signing out…' : 'Sign out'}</span>
        </button>
      </div>
    );
  }

  /* ── Signed out: device flow (studio) or PAT form ────────────── */

  if (deviceFlowAvailable) {
    return (
      <div className="gh-signin" data-testid="gh-signin-form">
        {flow ? (
          <div className="gh-device-flow" data-testid="gh-device-flow">
            <p className="gh-signin-lead">Enter this code on github.com:</p>
            <div className="gh-device-code" data-testid="gh-device-code">
              {flow.userCode}
            </div>
            <button
              type="button"
              className="gh-signin-submit"
              onClick={() => void openVerificationPage(flow.verificationUri).catch(() => undefined)}
              data-testid="gh-device-open"
            >
              <ExternalLink size={14} />
              Open github.com/login/device
            </button>
            <p className="gh-signin-hint" data-testid="gh-device-waiting">
              <Loader2 size={12} className="spin" /> Waiting for authorization…
            </p>
            <button type="button" className="gh-account-signout" onClick={handleDeviceFlowCancel} data-testid="gh-device-cancel">
              <X size={14} />
              <span>Cancel</span>
            </button>
          </div>
        ) : (
          <>
            <button
              type="button"
              className="gh-signin-submit"
              onClick={() => void handleDeviceFlowStart()}
              disabled={startingFlow}
              data-testid="gh-device-signin"
            >
              {startingFlow ? <Loader2 size={14} className="spin" /> : null}
              {startingFlow ? 'Connecting…' : 'Sign in with GitHub'}
            </button>
            <p className="gh-signin-hint">
              Opens github.com in your browser — enter the code shown next, no token needed. You can also{' '}
              <button
                type="button"
                className="gh-signin-link-btn"
                onClick={() => {
                  handleDeviceFlowCancel();
                  setShowPatForm(true);
                }}
                data-testid="gh-use-pat"
              >
                paste a personal access token
              </button>{' '}
              instead.
            </p>
            {flowError && (
              <p className="gh-signin-error" role="alert" data-testid="gh-device-error">
                {flowError}
              </p>
            )}
          </>
        )}
      </div>
    );
  }

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
