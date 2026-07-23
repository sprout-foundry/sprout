import { GitBranch, Clock, Zap, LayoutDashboard, Search } from 'lucide-react';
import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import { getEditorSync } from '../../services/crossTabSync';
import { useAppStoreSetState } from '../../contexts/AppStore';
import { useLog } from '../../utils/log';
import './PlatformPages.css';

interface Repo {
  id: number;
  name: string;
  full_name: string;
  html_url: string;
  description: string;
  language: string;
  updated_at: string;
  open_issues_count: number;
  stargazers_count: number;
}

interface TaskRecord {
  task_id: string;
  status: string;
  repo_url: string;
  prompt: string;
  created_at: string;
}

interface UsageSummary {
  tier: string;
  task_credits: { used: number; included: number; remaining: number };
  llm_spend?: { spend_cents: number; budget_cents: number; remaining: number };
  daily_llm?: {
    requests_used: number;
    requests_limit: number;
    tokens_used: number;
    tokens_limit: number;
    max_output_tokens: number;
  };
}

const DashboardPage: React.FC = () => {
  const log = useLog();
  const setState = useAppStoreSetState();
  const [repos, setRepos] = useState<Repo[]>([]);
  const [tasks, setTasks] = useState<TaskRecord[]>([]);
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  const fetchDashboard = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    const [reposRes, tasksRes, usageRes] = await Promise.allSettled([
      adapter.fetch('/user/me/repos?limit=12'),
      adapter.fetch('/api/tasks?limit=5'),
      adapter.fetch('/user/me/usage'),
    ]);

    if (reposRes.status === 'fulfilled') {
      const data = await reposRes.value.json().catch(() => ({ repos: [] }));
      setRepos(data.repos ?? []);
    }
    if (tasksRes.status === 'fulfilled') {
      const data = await tasksRes.value.json().catch(() => ({ tasks: [] }));
      setTasks(data.tasks ?? []);
    }
    if (usageRes.status === 'fulfilled') {
      const data = await usageRes.value.json().catch(() => null);
      setUsage(data);
    }

    if (reposRes.status === 'rejected' && tasksRes.status === 'rejected') {
      setError('Failed to load dashboard data');
      log.error('Dashboard fetch failed', { title: 'Dashboard' });
    }
    setLoading(false);
  }, [log]);

  useEffect(() => {
    fetchDashboard();
  }, [fetchDashboard]);

  // Cross-tab sync: refresh when dashboard pushes a task_update
  useEffect(() => {
    const sync = getEditorSync();
    const unsub = sync.subscribe((msg) => {
      if (msg.type === 'task_update') fetchDashboard();
    });
    return unsub;
  }, [fetchDashboard]);

  const filteredRepos = useMemo(() => {
    if (!searchQuery) return repos;
    const q = searchQuery.toLowerCase();
    return repos.filter((r) => r.full_name?.toLowerCase().includes(q) || r.name?.toLowerCase().includes(q));
  }, [repos, searchQuery]);

  const handleRepoClick = useCallback(
    (fullName: string) => {
      const [owner, name] = fullName.split('/');
      if (!owner || !name) return;
      setState(() => ({ currentView: 'repodetail', selectedRepo: { owner, name } }));
    },
    [setState],
  );

  const taskPct = usage
    ? Math.min(100, Math.round((usage.task_credits.used / Math.max(1, usage.task_credits.included)) * 100))
    : 0;

  if (loading) {
    return (
      <div className="platform-page">
        <div className="platform-loading">
          <div className="platform-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="platform-page dashboard-page">
      {/* Usage bar */}
      {usage && (
        <div className="platform-card dashboard-usage-bar">
          <div className="dashboard-usage-left">
            <Zap size={16} className="dashboard-tier-icon" />
            <span className="dashboard-tier-label" style={{ textTransform: 'capitalize' }}>
              {usage.tier}
            </span>
          </div>
          <div className="dashboard-usage-right">
            <div className="dashboard-meter">
              <div className="dashboard-meter-label">
                {usage.task_credits.used} / {usage.task_credits.included} tasks
              </div>
              <div className="dashboard-meter-bar">
                <div className="dashboard-meter-fill" style={{ width: `${taskPct}%` }} />
              </div>
            </div>
            {usage.tier === 'auto' && usage.daily_llm ? (
              <>
                <div className="dashboard-meter">
                  <div className="dashboard-meter-label">
                    {usage.daily_llm.requests_used} / {usage.daily_llm.requests_limit} requests today
                  </div>
                  <div className="dashboard-meter-bar">
                    <div
                      className="dashboard-meter-fill"
                      style={{
                        width: `${Math.min(100, (usage.daily_llm.requests_used / Math.max(1, usage.daily_llm.requests_limit)) * 100)}%`,
                      }}
                    />
                  </div>
                </div>
                <div className="dashboard-meter">
                  <div className="dashboard-meter-label">
                    {(usage.daily_llm.tokens_used / 1000).toFixed(1)}K / {(usage.daily_llm.tokens_limit / 1000).toFixed(0)}K tokens today
                  </div>
                  <div className="dashboard-meter-bar">
                    <div
                      className="dashboard-meter-fill"
                      style={{
                        width: `${Math.min(100, (usage.daily_llm.tokens_used / Math.max(1, usage.daily_llm.tokens_limit)) * 100)}%`,
                      }}
                    />
                  </div>
                </div>
              </>
            ) : (
              usage.llm_spend && (
                <div className="dashboard-meter">
                  <div className="dashboard-meter-label">
                    ${(usage.llm_spend.spend_cents / 100).toFixed(2)} / ${(usage.llm_spend.budget_cents / 100).toFixed(2)}{' '}
                    LLM
                  </div>
                  <div className="dashboard-meter-bar">
                    <div
                      className="dashboard-meter-fill"
                      style={{
                        width: `${Math.min(100, (usage.llm_spend.spend_cents / Math.max(1, usage.llm_spend.budget_cents)) * 100)}%`,
                      }}
                    />
                  </div>
                </div>
              )
            )}
          </div>
        </div>
      )}

      <div className="dashboard-grid">
        {/* Repo list */}
        <div className="platform-card dashboard-repos">
          <div className="platform-card-header">
            <h3>Repositories</h3>
            <div className="dashboard-search">
              <Search size={14} />
              <input
                type="text"
                placeholder="Search repos…"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="dashboard-search-input"
              />
            </div>
          </div>
          {filteredRepos.length === 0 ? (
            <p className="platform-empty-text">No repos found. Connect your GitHub account.</p>
          ) : (
            <div className="dashboard-repo-list">
              {filteredRepos.map((repo) => (
                <div
                  key={repo.id}
                  className="dashboard-repo-item dashboard-repo-item--clickable"
                  onClick={() => handleRepoClick(repo.full_name)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      handleRepoClick(repo.full_name);
                    }
                  }}
                >
                  <GitBranch size={14} className="dashboard-repo-icon" />
                  <div className="dashboard-repo-info">
                    <span className="dashboard-repo-name">{repo.full_name}</span>
                    {repo.description && <p className="dashboard-repo-desc">{repo.description}</p>}
                    {repo.language && <span className="dashboard-repo-lang">{repo.language}</span>}
                  </div>
                  <a
                    href={`/webui/?repo=${encodeURIComponent(repo.html_url)}`}
                    className="dashboard-repo-open-ide"
                    onClick={(e) => e.stopPropagation()}
                    title="Open in Browser IDE"
                  >
                    IDE
                  </a>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Recent tasks */}
        <div className="platform-card dashboard-tasks">
          <div className="platform-card-header">
            <h3>Recent Tasks</h3>
          </div>
          {tasks.length === 0 ? (
            <p className="platform-empty-text">No tasks yet. Run an agent task from a repo.</p>
          ) : (
            <div className="dashboard-task-list">
              {tasks.map((task) => (
                <div key={task.task_id} className="dashboard-task-item">
                  <div className={`dashboard-task-status status-dot-${task.status}`} />
                  <div className="dashboard-task-info">
                    <p className="dashboard-task-prompt">{task.prompt?.slice(0, 80) ?? 'Untitled task'}</p>
                    <div className="dashboard-task-meta">
                      <Clock size={11} />
                      <span>{new Date(task.created_at).toLocaleDateString()}</span>
                      <span className="dashboard-task-repo">{task.repo_url?.split('/').slice(-2).join('/')}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {error && <div className="platform-error-banner">{error}</div>}
    </div>
  );
};

export default DashboardPage;
