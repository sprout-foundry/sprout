/**
 * RepoOnboarding — First-run onboarding screen for the repo detail view.
 *
 * Shown when no repo is loaded. Offers:
 *   - Import from GitHub URL
 *   - Select from PAT-authenticated repo list
 *   - Create a new local repo (git init)
 */

import React, { useState, useEffect, useCallback } from 'react';
import {
  GitBranch,
  Plus,
  Loader2,
  Search,
  ArrowRight,
  Globe,
  KeyRound,
  Check,
  X,
  FileText,
  Terminal,
  Download,
  AlertCircle,
} from 'lucide-react';
import { getAdapter } from '../../services/apiAdapter';
import { gitClient } from '../../services/gitClient';
import type { FileEntry } from '../../services/gitClient';
import './RepoOnboarding.css';

interface RepoItem {
  name: string;
  fullName: string;
  description: string;
  language: string | null;
  isPrivate: boolean;
}

interface RepoOnboardingProps {
  onRepoSelected: (owner: string, name: string) => void;
  isDesktop?: boolean;
}

const RepoOnboarding: React.FC<RepoOnboardingProps> = ({ onRepoSelected, isDesktop = false }) => {
  const [activeTab, setActiveTab] = useState<'import' | 'select' | 'create'>('import');
  const [urlInput, setUrlInput] = useState('');
  const [cloneStatus, setCloneStatus] = useState<'idle' | 'cloning' | 'done' | 'error'>('idle');
  const [cloneError, setCloneError] = useState('');
  const [repos, setRepos] = useState<RepoItem[]>([]);
  const [reposLoading, setReposLoading] = useState(false);
  const [repoSearch, setRepoSearch] = useState('');
  const [showCreateDialog, setShowCreateDialog] = useState(false);

  // ── URL Import ──────────────────────────────────────────────

  const parseGitHubUrl = (input: string): { owner: string; name: string } | null => {
    // https://github.com/owner/repo or https://github.com/owner/repo.git
    const httpsMatch = input.match(/^https:\/\/github\.com\/([^/]+)\/([^/\s]+?)(?:\.git)?\/?$/);
    if (httpsMatch) return { owner: httpsMatch[1], name: httpsMatch[2].replace(/\.git$/, '') };

    // git@github.com:owner/repo or git@github.com:owner/repo.git
    const sshMatch = input.match(/^git@github\.com:([^/]+)\/([^/\s]+?)(?:\.git)?\/?$/);
    if (sshMatch) return { owner: sshMatch[1], name: sshMatch[2].replace(/\.git$/, '') };

    // owner/repo shorthand
    const shortMatch = input.match(/^([a-zA-Z0-9_-]+)\/([a-zA-Z0-9._-]+)$/);
    if (shortMatch) return { owner: shortMatch[1], name: shortMatch[2] };

    return null;
  };

  const handleUrlImport = useCallback(async () => {
    const parsed = parseGitHubUrl(urlInput.trim());
    if (!parsed) {
      setCloneStatus('error');
      setCloneError('Invalid GitHub URL. Use "owner/repo" or "https://github.com/owner/repo".');
      return;
    }

    setCloneStatus('cloning');
    const pat = localStorage.getItem('github_pat') ?? undefined;
    const repoDir = `/repos/${parsed.owner}/${parsed.name}`;

    try {
      await gitClient.clone(`https://github.com/${parsed.owner}/${parsed.name}`, repoDir, {
        depth: 1,
        branch: 'main',
        token: pat,
      });
      setCloneStatus('done');
      onRepoSelected(parsed.owner, parsed.name);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (/401|403|Unauthorized|authentication/i.test(msg)) {
        setCloneStatus('error');
        setCloneError(`Private repo needs a GitHub token. Add one in Settings or paste the token below.`);
      } else {
        setCloneStatus('error');
        setCloneError(msg);
      }
    }
  }, [urlInput, onRepoSelected]);

  // ── PAT Repo List ───────────────────────────────────────────

  const pat = typeof window !== 'undefined' ? localStorage.getItem('github_pat') : null;

  const fetchRepos = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) return;
    setReposLoading(true);
    try {
      const response = await adapter.fetch('/user/me/repos');
      if (response.ok) {
        const data = await response.json();
        const reposData: {
          name?: string;
          full_name?: string;
          description?: string;
          language?: string | null;
          private?: boolean;
        }[] = data.repos || data || [];
        const list: RepoItem[] = reposData.map((r) => ({
          name: r.name || '',
          fullName: r.full_name || r.name || '',
          description: r.description || '',
          language: r.language || null,
          isPrivate: r.private || false,
        }));
        setRepos(list);
      }
    } catch {
      // silently fail — will show empty state
    } finally {
      setReposLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeTab === 'select' && pat) {
      fetchRepos();
    }
  }, [activeTab, pat, fetchRepos]);

  const filteredRepos = repoSearch
    ? repos.filter(
        (r) =>
          r.fullName.toLowerCase().includes(repoSearch.toLowerCase()) ||
          r.name.toLowerCase().includes(repoSearch.toLowerCase()),
      )
    : repos;

  // ── Render ──────────────────────────────────────────────────

  return (
    <div className="onboarding-container">
      <div className="onboarding-hero">
        <GitBranch size={32} className="onboarding-hero-icon" />
        <h2>Get Started with a Repository</h2>
        <p className="onboarding-hero-desc">
          Clone an existing repository or create a new one to start working in the browser IDE.
        </p>
      </div>

      {/* Tab bar */}
      <div className="onboarding-tabs">
        <button
          className={`onboarding-tab ${activeTab === 'import' ? 'active' : ''}`}
          onClick={() => setActiveTab('import')}
        >
          <Globe size={16} /> Import URL
        </button>
        {pat && (
          <button
            className={`onboarding-tab ${activeTab === 'select' ? 'active' : ''}`}
            onClick={() => setActiveTab('select')}
          >
            <Search size={16} /> Select Repo
          </button>
        )}
        <button
          className={`onboarding-tab ${activeTab === 'create' ? 'active' : ''}`}
          onClick={() => setActiveTab('create')}
        >
          <Plus size={16} /> New Repo
        </button>
      </div>

      <div className="onboarding-content">
        {/* Tab: Import URL */}
        {activeTab === 'import' && (
          <div className="onboarding-import">
            <p className="onboarding-label">Paste a GitHub repository URL</p>
            <div className="onboarding-url-row">
              <input
                className="onboarding-url-input"
                type="text"
                placeholder="https://github.com/owner/repo"
                value={urlInput}
                onChange={(e) => setUrlInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleUrlImport();
                }}
                disabled={cloneStatus === 'cloning'}
              />
              <button
                className="btn btn-sm btn-primary"
                onClick={handleUrlImport}
                disabled={cloneStatus === 'cloning' || !urlInput.trim()}
              >
                {cloneStatus === 'cloning' ? <Loader2 size={14} className="spinner" /> : <ArrowRight size={14} />}
                {cloneStatus === 'cloning' ? ' Cloning…' : ' Clone'}
              </button>
            </div>
            <p className="onboarding-hint">
              Supports <code>https://github.com/owner/repo</code>, <code>git@github.com:owner/repo</code>, or shorthand{' '}
              <code>owner/repo</code>
            </p>
            {cloneStatus === 'error' && (
              <div className="onboarding-error">
                <AlertCircle size={14} /> {cloneError}
                <button className="btn btn-sm btn-ghost" onClick={() => setCloneStatus('idle')}>
                  Dismiss
                </button>
              </div>
            )}
            {cloneStatus === 'done' && (
              <div className="onboarding-success">
                <Check size={14} /> Repository cloned successfully!
              </div>
            )}
          </div>
        )}

        {/* Tab: Select from PAT list */}
        {activeTab === 'select' && pat && (
          <div className="onboarding-select">
            {reposLoading ? (
              <div className="onboarding-loading">
                <Loader2 size={16} className="spinner" /> Loading your repositories…
              </div>
            ) : repos.length === 0 ? (
              <div className="onboarding-empty">
                No repositories found. Make sure your GitHub token has <code>repo</code> scope.
              </div>
            ) : (
              <>
                <input
                  className="onboarding-search-input"
                  type="text"
                  placeholder="Search repositories…"
                  value={repoSearch}
                  onChange={(e) => setRepoSearch(e.target.value)}
                />
                <div className="onboarding-repo-list">
                  {filteredRepos.slice(0, 50).map((repo) => (
                    <button
                      key={repo.fullName}
                      className="onboarding-repo-card"
                      onClick={() => {
                        const parts = repo.fullName.split('/');
                        if (parts.length === 2) {
                          onRepoSelected(parts[0], parts[1]);
                        }
                      }}
                    >
                      <div className="onboarding-repo-left">
                        <GitBranch size={14} />
                        <span className="onboarding-repo-name">{repo.fullName}</span>
                        {repo.isPrivate && <KeyRound size={12} className="private-icon" />}
                        {repo.language && <span className="onboarding-repo-lang">{repo.language}</span>}
                      </div>
                      {repo.description && <span className="onboarding-repo-desc">{repo.description}</span>}
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>
        )}

        {/* Tab: Create new repo */}
        {activeTab === 'create' && (
          <div className="onboarding-create">
            <p className="onboarding-label">
              Create a new repository to start fresh. This will be stored locally in your browser.
            </p>
            <button className="btn btn-primary" onClick={() => setShowCreateDialog(true)}>
              <Plus size={16} /> New Repository
            </button>

            {showCreateDialog && (
              <CreateRepoDialog
                onClose={() => setShowCreateDialog(false)}
                onCreated={(owner, name) => onRepoSelected(owner, name)}
              />
            )}

            <div className="onboarding-create-info">
              <h4>Can&apos;t find your repo?</h4>
              <ul>
                <li>
                  <strong>Add a GitHub token</strong> in Settings to see private repositories.
                </li>
                <li>
                  Make sure your token has <code>repo</code> scope.
                </li>
              </ul>
            </div>
          </div>
        )}
      </div>

      {/* Quick tips */}
      <div className="onboarding-tips">
        <h4>💡 Quick Tips</h4>
        <div className="onboarding-tip-grid">
          <div className="onboarding-tip-card">
            <Terminal size={18} />
            <span>Use the terminal to run commands in the browser shell</span>
          </div>
          <div className="onboarding-tip-card">
            <FileText size={18} />
            <span>Open any file by clicking it in the file tree</span>
          </div>
          <div className="onboarding-tip-card">
            <Download size={18} />
            <span>Download your repo as a ZIP from the repo detail page</span>
          </div>
        </div>
      </div>
    </div>
  );
};

// ── Create Repo Dialog ──────────────────────────────────────────────────────

interface CreateRepoDialogProps {
  onClose: () => void;
  onCreated: (owner: string, name: string) => void;
}

const CreateRepoDialog: React.FC<CreateRepoDialogProps> = ({ onClose, onCreated }) => {
  const [repoName, setRepoName] = useState('');
  const [description, setDescription] = useState('');
  const [initReadme, setInitReadme] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState(false);

  const handleCreate = useCallback(async () => {
    const name = repoName.trim();
    if (!name) return;

    // Validate name: alphanumeric, hyphens, underscores, dots
    if (!/^[a-zA-Z0-9_.-]+$/.test(name)) {
      setError('Repo name can only contain letters, numbers, hyphens, underscores, and dots.');
      return;
    }

    setCreating(true);
    setError('');

    try {
      const repoDir = `/repos/local/${name}`;

      // Create directory and init git
      await gitClient.mkdir('/repos/local', `/${name}`);

      // Initialize the git repo (isomorphic-git init)
      const { default: git } = await import('isomorphic-git');

      // Write a .gitkeep marker file
      await gitClient.writeFile(repoDir, '/.gitkeep', '');

      // Init: isomorphic-git doesn't have a direct init function that
      // works outside of its filesystem wrapper. We'll create an empty repo
      // by writing the .git/HEAD
      const pfs = gitClient.getFs().promises;
      await pfs.mkdir(`${repoDir}/.git`).catch(() => {});
      await pfs.mkdir(`${repoDir}/.git/objects`).catch(() => {});
      await pfs.mkdir(`${repoDir}/.git/refs/heads`).catch(() => {});
      await pfs.writeFile(`${repoDir}/.git/HEAD`, 'ref: refs/heads/main\n', 'utf8');

      if (description) {
        // Write description as a file
        await gitClient.writeFile(repoDir, '/DESCRIPTION', description);
      }

      if (initReadme) {
        const readmeContent = `# ${name}\n\n${description || 'Created with Sprout browser IDE.'}\n`;
        await gitClient.writeFile(repoDir, '/README.md', readmeContent);

        // Stage and commit the README
        await git.init({ fs: gitClient.getFs(), dir: repoDir });
        await git.add({ fs: gitClient.getFs(), dir: repoDir, filepath: 'README.md' });
        await git.commit({
          fs: gitClient.getFs(),
          dir: repoDir,
          message: 'Initial commit',
          author: { name: 'Sprout User', email: 'user@sprout.local' },
        });
      }

      setDone(true);
      setTimeout(() => onCreated('local', name), 500);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }, [repoName, description, initReadme, onCreated]);

  return (
    <div className="onboarding-dialog-overlay">
      <div className="onboarding-dialog">
        <div className="onboarding-dialog-header">
          <h3>Create New Repository</h3>
          <button className="btn btn-sm btn-ghost" onClick={onClose}>
            <X size={16} />
          </button>
        </div>
        <div className="onboarding-dialog-body">
          <div className="onboarding-dialog-field">
            <label>Repository Name *</label>
            <input
              type="text"
              placeholder="my-project"
              value={repoName}
              onChange={(e) => setRepoName(e.target.value)}
              disabled={creating}
              autoFocus
            />
          </div>
          <div className="onboarding-dialog-field">
            <label>Description (optional)</label>
            <input
              type="text"
              placeholder="A short description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={creating}
            />
          </div>
          <div className="onboarding-dialog-checkbox">
            <input
              type="checkbox"
              id="init-readme"
              checked={initReadme}
              onChange={(e) => setInitReadme(e.target.checked)}
              disabled={creating}
            />
            <label htmlFor="init-readme">Initialize with README</label>
          </div>
          {error && <div className="onboarding-error">{error}</div>}
          {done && <div className="onboarding-dialog-done">Repository created. Opening…</div>}
        </div>
        <div className="onboarding-dialog-footer">
          <button className="btn btn-sm btn-ghost" onClick={onClose} disabled={creating}>
            Cancel
          </button>
          <button
            className="btn btn-sm btn-primary"
            onClick={handleCreate}
            disabled={creating || !repoName.trim() || done}
          >
            {creating ? <Loader2 size={14} className="spinner" /> : null}
            {creating ? ' Creating…' : 'Create Repository'}
          </button>
        </div>
      </div>
    </div>
  );
};

export default RepoOnboarding;
