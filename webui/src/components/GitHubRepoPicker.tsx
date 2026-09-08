/**
 * GitHubRepoPicker — modal for browsing and cloning your GitHub repos.
 *
 * Two states:
 *  - signed in: search + list of the repos visible to the stored token
 *    (name, private badge, description, updated date). Clicking a repo clones
 *    it through browserGit.gitClone with the token attached, so private repos
 *    work, and the clone lands in the same workspace the anonymous import
 *    flow writes to.
 *  - signed out: the shared PAT sign-in card (GitHubAccountPanel).
 *
 * Loading, empty, error, and cloning states are all inline — no window.alert.
 * The token is passed to the clone call and never logged.
 */

import { AlertTriangle, Download, GitBranch, Loader2, Lock, Search, X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MouseEvent as ReactMouseEvent, ReactElement } from 'react';
import { createPortal } from 'react-dom';
import { cloneRepo, type CloneResult } from '../services/workspaceFs/backendsExport';
import { clearGitHubAccount, getStoredToken, getStoredUser, listRepos } from '../services/githubService';
import type { GitHubRepo, GitHubUser } from '../services/githubService';
import { debugLog } from '../utils/log';
import './GitHubRepoPicker.css';
import GitHubAccountPanel from './GitHubAccountPanel';
import { showThemedConfirm } from './ThemedDialog';

export interface GitHubRepoPickerProps {
  isOpen: boolean;
  onClose: () => void;
  /** Called after a successful clone (parent refreshes the file tree). */
  onCloned?: (repo: GitHubRepo, result: CloneResult) => void;
}

function formatUpdated(iso: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const now = Date.now();
  const diffDays = Math.floor((now - date.getTime()) / 86_400_000);
  if (diffDays <= 0) return 'today';
  if (diffDays === 1) return 'yesterday';
  if (diffDays < 30) return `${diffDays}d ago`;
  if (diffDays < 365) return `${Math.floor(diffDays / 30)}mo ago`;
  return `${Math.floor(diffDays / 365)}y ago`;
}

export default function GitHubRepoPicker({ isOpen, onClose, onCloned }: GitHubRepoPickerProps): ReactElement | null {
  const [user, setUser] = useState<GitHubUser | null>(() => getStoredUser());
  const [token, setToken] = useState<string | null>(() => getStoredToken());
  const [repos, setRepos] = useState<GitHubRepo[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [cloningRepo, setCloningRepo] = useState<string | null>(null);
  const [cloneError, setCloneError] = useState<string | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  /* ── Reset + load repos whenever the modal opens ─────────────── */

  const loadRepos = useCallback(async (activeToken: string) => {
    setLoading(true);
    setListError(null);
    setRepos(null);
    try {
      const list = await listRepos(activeToken);
      setRepos(list);
    } catch (err) {
      setListError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    // Re-read storage on open — the user may have signed in/out in Settings.
    const currentToken = getStoredToken();
    const currentUser = getStoredUser();
    setToken(currentToken);
    setUser(currentUser);
    setQuery('');
    setCloningRepo(null);
    setCloneError(null);
    if (currentToken) {
      void loadRepos(currentToken);
    } else {
      setRepos(null);
      setLoading(false);
      setListError(null);
    }
  }, [isOpen, loadRepos]);

  /* ── Keyboard: Escape closes (not while cloning) ─────────────── */

  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !cloningRepo) onClose();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose, cloningRepo]);

  /* ── Focus the search box once the list view mounts ──────────── */

  useEffect(() => {
    if (isOpen && token) {
      const timer = setTimeout(() => searchInputRef.current?.focus(), 60);
      return () => clearTimeout(timer);
    }
  }, [isOpen, token]);

  /* ── Clone ───────────────────────────────────────────────────── */

  const handleClone = async (repo: GitHubRepo) => {
    const activeToken = token ?? getStoredToken();
    if (!activeToken || cloningRepo) return;

    setCloningRepo(repo.full_name);
    setCloneError(null);
    try {
      // Clone through the workspaceFs seam into repos/<owner>/<name>/ — the
      // same layout the agent's git tools use, so UI and agent share one
      // checkout. Old lightning-fs sidecar path removed.
      const result = await cloneRepo(repo.clone_url, { token: activeToken });
      debugLog(`[github-picker] cloned ${result.repo} (${result.entries} files, ${result.defaultBranch ?? 'no branch'})`);
      onCloned?.(repo, result);
      onClose();
    } catch (err) {
      // Unwrap isomorphic-git's MultipleGitError: its `.message` alone is an
      // opaque "refer to the errors property" — show the real per-file
      // failures instead of burying them (user-visible error contract).
      let message = err instanceof Error ? err.message : String(err);
      const inner = (err as { errors?: unknown[] }).errors;
      if (Array.isArray(inner) && inner.length > 0) {
        const details = inner
          .slice(0, 5)
          .map((e) => (e instanceof Error ? e.message : String(e)))
          .join(' · ');
        const more = inner.length > 5 ? ` (+${inner.length - 5} more)` : '';
        message = `${message} [${details}${more}]`;
      }
      debugLog(`[github-picker] clone failed for ${repo.full_name}: ${message}`);
      setCloneError(`Could not clone ${repo.full_name}: ${message}`);
    } finally {
      setCloningRepo(null);
    }
  };

  /* ── Signed out inside the modal: sign-in card ───────────────── */

  const handleSignedIn = (signedIn: GitHubUser) => {
    setUser(signedIn);
    const stored = getStoredToken();
    setToken(stored);
    if (stored) void loadRepos(stored);
  };

  // Called by GitHubAccountPanel AFTER it has already confirmed with the
  // user — do not confirm twice.
  const handleSignedOut = () => {
    clearGitHubAccount();
    setUser(null);
    setToken(null);
    setRepos(null);
    setListError(null);
  };

  // Fallback card (token present, no cached profile): nothing else confirms
  // for us, so confirm here.
  const handleStoredTokenSignOut = async () => {
    const confirmed = await showThemedConfirm(
      'Sign out of GitHub?\n\nThe stored token will be removed from this device.',
      { title: 'Sign out of GitHub', type: 'warning', confirmLabel: 'Sign out' },
    );
    if (!confirmed) return;
    handleSignedOut();
  };

  /* ── Client-side filter ──────────────────────────────────────── */

  const filteredRepos = useMemo(() => {
    if (!repos) return [];
    const q = query.trim().toLowerCase();
    if (!q) return repos;
    return repos.filter(
      (repo) =>
        repo.name.toLowerCase().includes(q) ||
        repo.full_name.toLowerCase().includes(q) ||
        (repo.description ?? '').toLowerCase().includes(q),
    );
  }, [repos, query]);

  if (!isOpen) return null;

  const handleOverlayClick = (e: ReactMouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget && !cloningRepo) onClose();
  };

  const showRepoList = Boolean(token);

  // Portal to document.body so the fixed overlay isn't clipped or
  // repositioned by transformed/overflow-hidden sidebar ancestors.
  return createPortal(
    <div
      className="gh-picker-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-label="Clone from GitHub"
      data-testid="gh-picker-overlay"
    >
      <div className="gh-picker-card" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="gh-picker-header">
          <div className="gh-picker-title">
            <GitBranch size={16} />
            <h2>{showRepoList ? 'Clone from GitHub' : 'Sign in to GitHub'}</h2>
          </div>
          <button
            type="button"
            className="gh-picker-close"
            onClick={onClose}
            aria-label="Close"
            disabled={Boolean(cloningRepo)}
            data-testid="gh-picker-close"
          >
            <X size={16} />
          </button>
        </div>

        {/* Body */}
        <div className="gh-picker-body">
          {!showRepoList && (
            <GitHubAccountPanel user={null} onSignedIn={handleSignedIn} onSignedOut={handleSignedOut} />
          )}

          {showRepoList && (
            <>
              {/* Errors go at the TOP of the modal body, above everything the
                  user is about to act on. A banner rendered below the list (or
                  under the fold of a long list on a tablet) is effectively
                  invisible at the moment the action fails. */}
              {cloneError && (
                <div className="gh-picker-clone-error" role="alert" data-testid="gh-picker-clone-error">
                  <AlertTriangle size={14} />
                  <span>{cloneError}</span>
                </div>
              )}

              {!loading && listError && (
                <div className="gh-picker-clone-error" role="alert" data-testid="gh-picker-list-error">
                  <AlertTriangle size={14} />
                  <span>{listError}</span>
                  <button
                    type="button"
                    className="gh-picker-retry"
                    onClick={() => {
                      const activeToken = token ?? getStoredToken();
                      if (activeToken) void loadRepos(activeToken);
                    }}
                    disabled={loading}
                    data-testid="gh-picker-retry"
                  >
                    Retry
                  </button>
                </div>
              )}

              {user ? (
                <GitHubAccountPanel user={user} onSignedIn={handleSignedIn} onSignedOut={handleSignedOut} compact />
              ) : (
                // Token present but no cached profile (e.g. written by an older
                // flow): still offer sign-out from here.
                <div className="gh-account-card gh-account-card--compact">
                  <span className="gh-account-name">Signed in with a stored token</span>
                  <button
                    type="button"
                    className="gh-account-signout"
                    onClick={() => void handleStoredTokenSignOut()}
                    data-testid="gh-signout-btn"
                  >
                    Sign out
                  </button>
                </div>
              )}

              <div className="gh-picker-search">
                <Search size={14} aria-hidden="true" />
                <input
                  ref={searchInputRef}
                  type="text"
                  className="gh-picker-search-input"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Filter repositories…"
                  aria-label="Filter repositories"
                  disabled={Boolean(cloningRepo)}
                  data-testid="gh-picker-search"
                />
              </div>

              {loading && (
                <div className="gh-picker-state" data-testid="gh-picker-loading">
                  <Loader2 size={16} className="spin" />
                  <span>Loading repositories…</span>
                </div>
              )}

              {!loading && !listError && repos !== null && filteredRepos.length === 0 && (
                <div className="gh-picker-state" data-testid="gh-picker-empty">
                  {repos.length === 0 ? (
                    <span>No repositories visible to this token.</span>
                  ) : (
                    <span>No repositories match “{query}”.</span>
                  )}
                </div>
              )}

              {!loading && !listError && filteredRepos.length > 0 && (
                <ul className="gh-picker-list" data-testid="gh-picker-list">
                  {filteredRepos.map((repo) => {
                    const isCloning = cloningRepo === repo.full_name;
                    return (
                      <li key={repo.id}>
                        <button
                          type="button"
                          className={`gh-repo-row ${cloningRepo && !isCloning ? 'gh-repo-row--dim' : ''}`}
                          onClick={() => void handleClone(repo)}
                          disabled={Boolean(cloningRepo)}
                          data-testid={`gh-repo-${repo.full_name}`}
                        >
                          <span className="gh-repo-main">
                            <span className="gh-repo-name">
                              {repo.name}
                              {repo.private && (
                                <span className="gh-repo-badge" title="Private repository">
                                  <Lock size={10} /> Private
                                </span>
                              )}
                            </span>
                            {repo.description && <span className="gh-repo-desc">{repo.description}</span>}
                          </span>
                          <span className="gh-repo-meta">
                            <span className="gh-repo-updated">{formatUpdated(repo.updated_at)}</span>
                            <span className="gh-repo-action">
                              {isCloning ? <Loader2 size={13} className="spin" /> : <Download size={13} />}
                              <span>{isCloning ? 'Cloning…' : 'Clone'}</span>
                            </span>
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </>
          )}
        </div>

        {/* Footer */}
        <div className="gh-picker-footer">
          <span className="gh-picker-footer-hint">
            {showRepoList
              ? 'Shallow clone (depth 1) of the default branch into your workspace.'
              : 'Or close this dialog and paste a public repository URL instead.'}
          </span>
        </div>
      </div>
    </div>,
    document.body,
  );
}
