/**
 * GitHubSettingsTab — Environment ▸ GitHub settings subsection.
 *
 * Account status + PAT sign-in / sign-out. Self-contained: state lives here
 * (localStorage + api.github.com), so nothing is threaded through
 * useSettingsState.
 *
 * Signing in stores the PAT under localStorage('github_pat') — the key
 * agentGitTools.ts reads — which makes agent git_push / git_pull work
 * automatically, and enables the authenticated repo picker on the Clone
 * button.
 */

import { useState } from 'react';
import type { ReactElement } from 'react';
import { getStoredUser } from '../../services/githubService';
import type { GitHubUser } from '../../services/githubService';
import GitHubAccountPanel from '../GitHubAccountPanel';

export default function GitHubSettingsTab(): ReactElement {
  const [user, setUser] = useState<GitHubUser | null>(() => getStoredUser());

  return (
    <div className="settings-section">
      <h3 className="settings-section-title">GitHub</h3>
      <p className="settings-description">
        Connect a GitHub account to browse and clone your repositories (including private ones) and to let the agent
        push and pull on your behalf.
      </p>

      <GitHubAccountPanel user={user} compact onSignedIn={setUser} onSignedOut={() => setUser(null)} />

      <div className="settings-info-note">
        The token is stored in this browser (localStorage key <code>github_pat</code>) and is sent only to
        api.github.com and github.com over HTTPS. Signing in also enables <code>git_push</code> / <code>git_pull</code>{' '}
        for the agent. Sign out to remove it.
      </div>
    </div>
  );
}
