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
} from 'lucide-react';
import React, { useState, useEffect, useCallback, useRef } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { gitClient } from '../../services/gitClient';
import { syncRepoToWasmVfs } from '../../services/repoVfsBridge';
import { RepoFileTree } from './RepoFileTree';
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
}

const RepoDetailPage: React.FC<RepoDetailPageProps> = ({ repoOwner, repoName, onBack }) => {
  const log = useLog();
  const [data, setData] = useState<RepoDetailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Clone state
  const [cloneStatus, setCloneStatus] = useState<
    'idle' | 'checking' | 'cloning' | 'ready' | 'error' | 'needs-auth'
  >('idle');
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
  const cloningRef = useRef(false);

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
          await gitClient.clone(
            `https://github.com/${repoOwner}/${repoName}`,
            repoDir,
            {
              depth: 1,
              branch: data?.repo?.default_branch ?? 'main',
              token,
              onProgress: ({ phase, loaded, total }) => {
                setCloneProgress({ phase, loaded, total });
              },
            }
          );
        }
        setCloneStatus('ready');
      } catch (err: any) {
        const msg = err?.message ?? String(err);
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
    [repoDir, repoOwner, repoName, data?.repo?.default_branch]
  );

  const handleFileClick = useCallback(
    (filepath: string, content: string) => {
      setOpenedFile({ path: filepath, content });
      log.debug(`Opened ${filepath} from ${repoOwner}/${repoName}`);
    },
    [repoOwner, repoName, log]
  );

  const handleOpenInIDE = useCallback(async () => {
    if (cloneStatus !== 'ready') {
      await ensureCloned();
    }
    // Bridge to WASM VFS so the agent can read these files
    const adapter = getAdapter() as any;
    const shell = adapter?.getWasmShell?.();
    if (shell) {
      try {
        await syncRepoToWasmVfs(repoDir, '/workspace/repo', shell);
        log.info(`Synced ${repoOwner}/${repoName} to workspace`);
      } catch (err: any) {
        log.error(`Sync error: ${err.message ?? err}`);
      }
    }
    // Navigate to editor
    window.history.pushState({}, '', `/webui/?repo=${encodeURIComponent(`${repoOwner}/${repoName}`)}`);
    window.dispatchEvent(new CustomEvent('sprout:navigate', { detail: 'editor' }));
  }, [cloneStatus, ensureCloned, repoDir, repoOwner, repoName, log]);

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

      {/* Repo header */}
      <div className="platform-card repo-detail-header">
        <div className="repo-detail-title-row">
          <GitBranch size={20} />
          <h2>{data.repo.full_name}</h2>
        </div>
        {data.repo.description && <p className="repo-detail-desc">{data.repo.description}</p>}
        <div className="repo-detail-actions">
          <a className="btn btn-sm" href={`/webui/?repo=${encodeURIComponent(data.repo.html_url)}`}>
            Browser IDE
          </a>
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
              <span key={b} className="branch-chip">
                {b}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Clone + File Tree */}
      <div className="platform-card">
        <div className="platform-card-header">
          <h3>
            <Download size={16} /> Repository Files
          </h3>
          {cloneStatus === 'ready' && (
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
                {cloneProgress.phase}{' '}
                {Math.round(cloneProgress.loaded / 1024)}KB
                {cloneProgress.total
                  ? ` / ${Math.round(cloneProgress.total / 1024)}KB`
                  : ''}
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

        {cloneStatus === 'ready' && (
          <div className="repo-files-container">
            <RepoFileTree dir={repoDir} onFileClick={handleFileClick} />
            {openedFile && (
              <div className="file-preview">
                <div className="file-preview-header">
                  <code>{openedFile.path}</code>
                  <button
                    className="btn btn-sm btn-ghost"
                    onClick={() => setOpenedFile(null)}
                  >
                    ×
                  </button>
                </div>
                <pre className="file-preview-content">
                  {openedFile.content.slice(0, 5000)}
                  {openedFile.content.length > 5000 && '\n… (truncated)'}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default RepoDetailPage;
