import { GitBranch, GitCommit, GitPullRequest, AlertCircle, ArrowLeft } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
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
    </div>
  );
};

export default RepoDetailPage;
