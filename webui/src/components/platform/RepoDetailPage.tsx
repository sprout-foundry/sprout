import {
  GitBranch,
  GitCommit,
  GitPullRequest,
  AlertCircle,
  ArrowLeft,
  Download,
  Loader2,
  RefreshCw,
  KeyRound,
  ServerCrash,
  History,
} from 'lucide-react';
import React, { useState, useEffect, useCallback, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getAdapter } from '../../services/apiAdapter';
import { gitClient } from '../../services/gitClient';
import { syncRepoToWasmVfs, type WasmWriter } from '../../services/repoVfsBridge';
import { downloadRepoAsZip } from '../../services/repoDownload';
import { RepoFileTree } from './RepoFileTree';
import CommitHistory from './CommitHistory';
import DiffViewer from './DiffViewer';
import RepoTabBar from './RepoTabBar';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

interface RepoDetailData {
  repo: {
    id: number;
    name: string;
    full_name: string;
    html_url: string;
    description: string;
    default_branch: string;
    language: string;
  };
  branches: string[];
  recentCommits: { sha: string; message: string; author: string; date: string }[];
  pullRequests: { number: number; title: string; state: string }[];
}

interface RepoDetailPageProps {
  repoOwner: string;
  repoName: string;
  onBack: () => void;
  attachedRepos?: Array<{ owner: string; name: string; id: string }>;
  onRepoSwitch?: (id: string) => void;
  onRepoDetach?: (id: string) => void;
  onRepoAdd?: () => void;
}

/** Check if a filename is a README file (case-insensitive). */
function isReadmeFile(filepath: string): boolean {
  const name = filepath.split('/').pop() ?? filepath;
  return /^readme(\.md|\.mdx|\.markdown|\.txt)?$/i.test(name);
}

const RepoDetailPage: React.FC<RepoDetailPageProps> = ({
  repoOwner,
  repoName,
  onBack,
  attachedRepos,
  onRepoSwitch,
  onRepoDetach,
  onRepoAdd,
}) => {
  const log = useLog();
  const [data, setData] = useState<RepoDetailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Clone state
  const [cloneStatus, setCloneStatus] = useState<'idle' | 'checking' | 'cloning' | 'ready' | 'error' | 'needs-auth'>(
    'idle',
  );
  const [cloneProgress, setCloneProgress] = useState<{
    phase: string;
    loaded: number;
    total: number;
  } | null>(null);
  const [cloneError, setCloneError] = useState<string>('');
  const [repoDir] = useState(`/repos/${repoOwner}/${repoName}`);
  const [openedFile, setOpenedFile] = useState<{ path: string; content: string } | null>(null);
  const [showPatPrompt, setShowPatPrompt] = useState(false);
  const [patInput, setPatInput] = useState('');
  const [activeRepoTab, setActiveRepoTab] = useState<'files' | 'history'>('files');
  const [viewingDiff, setViewingDiff] = useState<string | null>(null);
  const cloningRef = useRef(false);

  // Git operation feedback
  const [gitOpStatus, setGitOpStatus] = useState<{
    type: 'push' | 'pull' | 'checkout' | null;
    status: 'idle' | 'in_progress' | 'success' | 'error';
    message: string;
  }>({ type: null, status: 'idle', message: '' });
  const [activeBranch, setActiveBranch] = useState<string>('');

  const fetchDetail = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    const [repoRes, branchesRes, commitsRes, pullsRes] = await Promise.allSettled([
      adapter.fetch(`/user/me/repos/${repoOwner}/${repoName}`),
      adapter.fetch(`/user/me/repos/${repoOwner}/${repoName}/branches`),
      adapter.fetch(`/user/me/repos/${repoOwner}/${repoName}/commits?limit=10`),
      adapter.fetch(`/user/me/repos/${repoOwner}/${repoName}/pulls?limit=5`),
    ]);

    if (repoRes.status === 'rejected') {
      setError('Failed to load repo details');
      setLoading(false);
      return;
    }

    const repoData = await repoRes.value.json().catch(() => ({}));
    const branches =
      branchesRes.status === 'fulfilled'
        ? (await branchesRes.value.json().catch(() => [])).map((b: { name?: string }) => b.name || String(b))
        : [];
    const commits =
      commitsRes.status === 'fulfilled'
        ? (await commitsRes.value.json().catch(() => [])).map(
            (c: { sha?: string; commit?: { message?: string; author?: { name?: string; date?: string } } }) => ({
              sha: c.sha?.slice(0, 7) ?? '',
              message: c.commit?.message?.split('\n')[0] ?? '',
              author: c.commit?.author?.name ?? '',
              date: c.commit?.author?.date ?? '',
            }),
          )
        : [];
    const pulls =
      pullsRes.status === 'fulfilled'
        ? (await pullsRes.value.json().catch(() => [])).map(
            (p: { number?: number; title?: string; state?: string }) => ({
              number: p.number ?? 0,
              title: p.title ?? '',
              state: p.state ?? 'open',
            }),
          )
        : [];

    setData({ repo: repoData, branches, recentCommits: commits, pullRequests: pulls });
    if (!activeBranch && repoData?.default_branch) {
      setActiveBranch(repoData.default_branch);
    }
    setLoading(false);
  }, [repoOwner, repoName]);

  useEffect(() => {
    fetchDetail();
  }, [fetchDetail]);

  const ensureCloned = useCallback(
    async (token?: string) => {
      if (cloningRef.current) return;
      cloningRef.current = true;
      setCloneStatus('checking');
      setCloneError('');

      try {
        const exists = await gitClient.exists(repoDir);
        if (!exists) {
          setCloneStatus('cloning');
          await gitClient.clone(`https://github.com/${repoOwner}/${repoName}`, repoDir, {
            depth: 1,
            branch: data?.repo?.default_branch ?? 'main',
            token,
            onProgress: ({ phase, loaded, total }) => {
              setCloneProgress({ phase, loaded, total });
            },
          });
        }
        setCloneStatus('ready');
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (/401|403|Unauthorized|authentication/i.test(msg)) {
          setCloneStatus('needs-auth');
        } else {
          setCloneError(msg);
          setCloneStatus('error');
        }
      } finally {
        cloningRef.current = false;
      }
    },
    [repoDir, repoOwner, repoName, data?.repo?.default_branch],
  );

  const handleFileClick = useCallback(
    (filepath: string, content: string) => {
      setOpenedFile({ path: filepath, content });
      log.debug(`Opened ${filepath} from ${repoOwner}/${repoName}`);
    },
    [repoOwner, repoName, log],
  );

  const handleOpenInIDE = useCallback(async () => {
    if (cloneStatus !== 'ready') {
      await ensureCloned();
    }
    // Bridge to WASM VFS so the agent can read these files
    const adapter = getAdapter() as { getWasmShell?: () => WasmWriter };
    const shell = adapter?.getWasmShell?.();
    if (shell) {
      try {
        await syncRepoToWasmVfs(repoDir, '/workspace/repo', shell);
        log.info(`Synced ${repoOwner}/${repoName} to workspace`);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        log.error(`Sync error: ${msg}`);
      }
    }
    // Navigate to editor
    window.history.pushState({}, '', `/webui/?repo=${encodeURIComponent(`${repoOwner}/${repoName}`)}`);
    window.dispatchEvent(new CustomEvent('sprout:navigate', { detail: 'editor' }));
  }, [cloneStatus, ensureCloned, repoDir, repoOwner, repoName, log]);

  const handlePush = useCallback(async () => {
    const PAT = localStorage.getItem('github_pat');
    if (!PAT) {
      setGitOpStatus({ type: 'push', status: 'error', message: 'No GitHub token configured. Add one in Settings.' });
      return;
    }
    setGitOpStatus({ type: 'push', status: 'in_progress', message: 'Pushing to remote…' });
    try {
      await gitClient.push(repoDir, {
        token: PAT,
        branch: data?.repo?.default_branch ?? 'main',
      });
      setGitOpStatus({ type: 'push', status: 'success', message: 'Push successful' });
      setTimeout(() => setGitOpStatus({ type: null, status: 'idle', message: '' }), 3000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setGitOpStatus({ type: 'push', status: 'error', message: msg });
    }
  }, [repoDir, data?.repo?.default_branch]);

  const handlePull = useCallback(async () => {
    let token: string | undefined;
    const stored = localStorage.getItem('github_pat');
    if (stored && stored.length > 0) token = stored;

    setGitOpStatus({ type: 'pull', status: 'in_progress', message: 'Pulling from remote…' });
    try {
      await gitClient.pull(repoDir, { token });
      // Re-bridge VFS after pull
      const adapter = getAdapter() as { getWasmShell?: () => WasmWriter };
      const shell = adapter?.getWasmShell?.();
      if (shell) {
        await syncRepoToWasmVfs(repoDir, '/workspace/repo', shell);
      }
      setGitOpStatus({ type: 'pull', status: 'success', message: 'Pull successful — files synced' });
      setTimeout(() => setGitOpStatus({ type: null, status: 'idle', message: '' }), 3000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setGitOpStatus({ type: 'pull', status: 'error', message: msg });
    }
  }, [repoDir, log]);

  const handleBranchCheckout = useCallback(
    async (branchName: string) => {
      // Check for uncommitted changes first
      try {
        const status = await gitClient.status(repoDir);
        const hasUncommitted = status.length > 0;
        if (hasUncommitted) {
          const confirmed = window.confirm(
            `You have uncommitted changes. Switching branches may lose them.\n\n` +
            `- Commit your changes first, or\n` +
            `- Discard them and switch.\n\n` +
            `Switch anyway?`,
          );
          if (!confirmed) return;
        }
      } catch {
        // Status check failed — proceed with checkout anyway
      }

      setGitOpStatus({ type: 'checkout', status: 'in_progress', message: `Switching to ${branchName}…` });
      try {
        await gitClient.checkout(repoDir, branchName);
        setActiveBranch(branchName);

        // Re-bridge VFS: clear stale files then sync new branch's content
        const adapter = getAdapter() as { getWasmShell?: () => WasmWriter };
        const shell = adapter?.getWasmShell?.();
        if (shell) {
          // Clear workspace via shell command (rm -rf /workspace/repo)
          try {
            shell.writeFile('/workspace/repo/.clear-marker', '');
          } catch {
            // VFS may not be mounted — ignore
          }
          try {
            await syncRepoToWasmVfs(repoDir, '/workspace/repo', shell, (p) => {
              setGitOpStatus({ type: 'checkout', status: 'in_progress', message: `Syncing: ${p.current}` });
            });
          } catch {
            // Bridge failure — not fatal; tree still shows lightning-fs content
          }
        }

        setGitOpStatus({ type: 'checkout', status: 'success', message: `Switched to ${branchName}` });
        setTimeout(() => setGitOpStatus({ type: null, status: 'idle', message: '' }), 3000);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setGitOpStatus({ type: 'checkout', status: 'error', message: msg });
      }
    },
    [repoDir],
  );

  const handleCreateFile = useCallback(
    async (name: string) => {
      const filepath = `/${name}`;
      await gitClient.writeFile(repoDir, filepath, '');
      log.info(`Created file ${name} in ${repoOwner}/${repoName}`);
    },
    [repoDir, repoOwner, repoName, log],
  );

  const handleCreateFolder = useCallback(
    async (name: string) => {
      await gitClient.mkdir(repoDir, `/${name}`);
      log.info(`Created folder ${name} in ${repoOwner}/${repoName}`);
    },
    [repoDir, repoOwner, repoName, log],
  );

  useEffect(() => {
    ensureCloned();
  }, [ensureCloned]);

  if (loading) {
    return (
      <div className="platform-page">
        <div className="platform-loading">
          <div className="platform-spinner" />
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="platform-page">
        <button className="btn btn-sm btn-ghost" onClick={onBack}>
          <ArrowLeft size={14} /> Back
        </button>
        <div className="platform-card">
          <p className="platform-empty-text">{error || 'Repo not found'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="platform-page">
      <button className="btn btn-sm btn-ghost" onClick={onBack}>
        <ArrowLeft size={14} /> Back to Repos
      </button>

      {attachedRepos && attachedRepos.length > 0 && (
        <RepoTabBar
          repos={attachedRepos}
          activeRepoId={data?.repo?.full_name ?? ''}
          onSelectRepo={(id) => onRepoSwitch?.(id)}
          onAddRepo={() => onRepoAdd?.()}
          onDetachRepo={(id) => onRepoDetach?.(id)}
        />
      )}

      {/* Repo header */}
      <div className="platform-card repo-detail-header">
        <div className="repo-detail-title-row">
          <GitBranch size={20} />
          <h2>{data.repo.full_name}</h2>
        </div>
        {data.repo.description && <p className="repo-detail-desc">{data.repo.description}</p>}
        <div className="repo-detail-actions">
          {cloneStatus === 'ready' && (
            <div className="git-action-bar">
              <button
                className="btn btn-sm btn-ghost"
                onClick={handlePush}
                disabled={gitOpStatus.status === 'in_progress'}
              >
                {gitOpStatus.type === 'push' && gitOpStatus.status === 'in_progress' ? (
                  <Loader2 size={14} className="spinner" />
                ) : (
                  <GitCommit size={14} />
                )}
                {' Push'}
              </button>
              <button
                className="btn btn-sm btn-ghost"
                onClick={handlePull}
                disabled={gitOpStatus.status === 'in_progress'}
              >
                {gitOpStatus.type === 'pull' && gitOpStatus.status === 'in_progress' ? (
                  <Loader2 size={14} className="spinner" />
                ) : (
                  <RefreshCw size={14} />
                )}
                {' Pull'}
              </button>
            </div>
          )}
          {gitOpStatus.status !== 'idle' && gitOpStatus.message && (
            <span
              className={`git-op-status git-op-status--${gitOpStatus.status === 'error' ? 'error' : gitOpStatus.status === 'success' ? 'success' : 'pending'}`}
            >
              {gitOpStatus.message}
            </span>
          )}
          <a className="btn btn-sm" href={`/webui/?repo=${encodeURIComponent(data.repo.html_url)}`}>
            Browser IDE
          </a>
          {cloneStatus === 'ready' && (
            <button
              className="btn btn-sm btn-ghost"
              onClick={async () => {
                try {
                  await downloadRepoAsZip(repoDir, `${repoOwner}-${repoName}`);
                  log.info(`Downloaded ${repoOwner}/${repoName} as ZIP`);
                } catch (err) {
                  const msg = err instanceof Error ? err.message : String(err);
                  log.error(`ZIP download failed: ${msg}`);
                }
              }}
            >
              <Download size={14} /> ZIP
            </button>
          )}
          {data.repo.language && <span className="repo-detail-lang">{data.repo.language}</span>}
        </div>
      </div>

      <div className="dashboard-grid">
        {/* Recent commits */}
        <div className="platform-card">
          <div className="platform-card-header">
            <h3>
              <GitCommit size={16} /> Recent Commits
            </h3>
          </div>
          {data.recentCommits.length === 0 ? (
            <p className="platform-empty-text">No commits found.</p>
          ) : (
            <div className="platform-list">
              {data.recentCommits.map((c, i) => (
                <div key={i} className="commit-row">
                  <code className="commit-sha">{c.sha}</code>
                  <span className="commit-message">{c.message}</span>
                  <span className="commit-author">{c.author}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Pull requests */}
        <div className="platform-card">
          <div className="platform-card-header">
            <h3>
              <GitPullRequest size={16} /> Pull Requests
            </h3>
          </div>
          {data.pullRequests.length === 0 ? (
            <p className="platform-empty-text">No open pull requests.</p>
          ) : (
            <div className="platform-list">
              {data.pullRequests.map((pr) => (
                <div key={pr.number} className="pr-row">
                  <GitPullRequest size={14} className={`pr-icon pr-icon-${pr.state}`} />
                  <span className="pr-title">
                    #{pr.number} {pr.title}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Branches */}
      {data.branches.length > 0 && (
        <div className="platform-card">
          <div className="platform-card-header">
            <h3>
              <GitBranch size={16} /> Branches ({data.branches.length})
            </h3>
          </div>
          <div className="branch-chips">
            {data.branches.slice(0, 20).map((b) => (
              <button
                key={b}
                className={`branch-chip ${b === activeBranch ? 'branch-chip--active' : ''}`}
                onClick={() => handleBranchCheckout(b)}
                disabled={gitOpStatus.status === 'in_progress' || b === activeBranch}
                title={`Checkout ${b}`}
              >
                {b === activeBranch ? <GitBranch size={12} /> : null}
                {b}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Clone + File Tree / History */}
      <div className="platform-card">
        <div className="platform-card-header">
          <div className="repo-tab-bar">
            <button
              className={`repo-tab ${activeRepoTab === 'files' ? 'active' : ''}`}
              onClick={() => setActiveRepoTab('files')}
            >
              <Download size={14} /> Files
            </button>
            <button
              className={`repo-tab ${activeRepoTab === 'history' ? 'active' : ''}`}
              onClick={() => setActiveRepoTab('history')}
            >
              <History size={14} /> History
            </button>
          </div>
          {cloneStatus === 'ready' && activeRepoTab === 'files' && (
            <button className="btn btn-sm btn-ghost" onClick={() => ensureCloned()}>
              <RefreshCw size={14} /> Refresh
            </button>
          )}
        </div>

        {cloneStatus === 'checking' && (
          <div className="clone-status">
            <Loader2 size={16} className="spinner" /> Checking local cache…
          </div>
        )}

        {cloneStatus === 'cloning' && (
          <div className="clone-status">
            <Loader2 size={16} className="spinner" /> Cloning from GitHub…
            {cloneProgress && (
              <span className="clone-progress">
                {cloneProgress.phase} {Math.round(cloneProgress.loaded / 1024)}KB
                {cloneProgress.total ? ` / ${Math.round(cloneProgress.total / 1024)}KB` : ''}
              </span>
            )}
          </div>
        )}

        {cloneStatus === 'error' && (
          <div className="clone-error">
            <AlertCircle size={14} /> {cloneError}
            <button className="btn btn-sm" onClick={() => ensureCloned()}>
              Retry
            </button>
          </div>
        )}

        {cloneStatus === 'needs-auth' && (
          <div className="clone-needs-auth">
            <AlertCircle size={14} /> This repository is private.
            {!showPatPrompt ? (
              <button className="btn btn-sm" onClick={() => setShowPatPrompt(true)}>
                <KeyRound size={14} /> Add GitHub Token
              </button>
            ) : (
              <div className="pat-input-row">
                <input
                  type="password"
                  className="input"
                  placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                  value={patInput}
                  onChange={(e) => setPatInput(e.target.value)}
                />
                <button
                  className="btn btn-sm btn-primary"
                  onClick={() => {
                    localStorage.setItem('github_pat', patInput);
                    setShowPatPrompt(false);
                    ensureCloned(patInput);
                  }}
                >
                  Clone with Token
                </button>
              </div>
            )}
          </div>
        )}

        {cloneStatus === 'ready' && activeRepoTab === 'history' && (
          <div className="repo-files-container">
            {viewingDiff ? (
              <DiffViewer
                repoDir={repoDir}
                sha={viewingDiff}
                onClose={() => setViewingDiff(null)}
              />
            ) : (
              <CommitHistory
                repoDir={repoDir}
                onViewDiff={(sha) => setViewingDiff(sha)}
              />
            )}
          </div>
        )}

        {cloneStatus === 'ready' && activeRepoTab === 'files' && (
          <div className="repo-files-container">
            <RepoFileTree
              dir={repoDir}
              onFileClick={handleFileClick}
              onCreateFile={handleCreateFile}
              onCreateFolder={handleCreateFolder}
            />
            {openedFile && (
              <div className="file-preview">
                <div className="file-preview-header">
                  <code>{openedFile.path}</code>
                  <button className="btn btn-sm btn-ghost" onClick={() => setOpenedFile(null)}>
                    ×
                  </button>
                </div>
                {isReadmeFile(openedFile.path) ? (
                  <div className="readme-preview">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {openedFile.content}
                    </ReactMarkdown>
                  </div>
                ) : (
                  <pre className="file-preview-content">
                    {openedFile.content.slice(0, 5000)}
                    {openedFile.content.length > 5000 && '\n… (truncated)'}
                  </pre>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default RepoDetailPage;
