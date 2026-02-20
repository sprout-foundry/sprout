/**
 * Git View Provider
 *
 * Data-driven provider for Git view sidebar content
 */

import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';
import { ApiService } from '../services/api';

export class GitViewProvider implements ContentProvider {
  readonly id = 'git-view';
  readonly viewType = 'git';
  readonly name = 'Git View Provider';

  // State management for the provider
  private state: {
    status: any;
    loading: boolean;
    error: string | null;
  } = {
    status: null,
    loading: false,
    error: null
  };

  private listeners: Set<() => void> = new Set();
  private apiService: ApiService;

  constructor() {
    this.apiService = ApiService.getInstance();
  }

  // Subscribe to state changes
  subscribe(listener: () => void) {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  // Notify all listeners
  private notify() {
    this.listeners.forEach(listener => listener());
  }

  // Fetch git status
  async fetchStatus() {
    if (this.state.loading) return;

    this.state.loading = true;
    this.notify();

    try {
      const response = await this.apiService.getGitStatus();
      this.state.status = response.status;
      this.state.error = null;
    } catch (error) {
      console.error('Failed to fetch git status:', error);
      this.state.error = 'Failed to fetch git status';
      this.state.status = null;
    } finally {
      this.state.loading = false;
      this.notify();
    }
  }

  getSections(context: ProviderContext): SidebarSection[] {
    // Trigger initial fetch if needed
    if (!this.state.status && !this.state.loading && !this.state.error) {
      this.fetchStatus();
    }

    return [
      {
        id: 'git-branch',
        dataSource: { type: 'state' },
        renderItem: () => {
          if (this.state.loading) {
            return <div className="loading">Loading...</div>;
          }

          if (this.state.error) {
            return (
              <div className="error">
                <span className="error-icon">⚠️</span>
                <span>{this.state.error}</span>
                <button
                  className="retry-button"
                  onClick={() => this.fetchStatus()}
                  style={{ marginLeft: '8px', padding: '2px 8px' }}
                >
                  Retry
                </button>
              </div>
            );
          }

          if (!this.state.status) {
            return <div className="empty">Not a git repository</div>;
          }

          const { branch, ahead, behind } = this.state.status;
          return (
            <div className="git-branch-info">
              <div className="branch-name">
                <span className="branch-icon">🔀</span>
                <span className="branch-text">{branch || 'No branch'}</span>
              </div>
              {(ahead > 0 || behind > 0) && (
                <div className="branch-tracking">
                  {ahead > 0 && <span className="ahead">▲ {ahead} ahead</span>}
                  {behind > 0 && <span className="behind">▼ {behind} behind</span>}
                </div>
              )}
            </div>
          );
        },
        title: () => `🔀 Git Status`,
        order: 1
      },
      {
        id: 'git-staged',
        dataSource: { type: 'state' },
        renderItem: () => {
          if (!this.state.status || !this.state.status.staged || this.state.status.staged.length === 0) {
            return null;
          }

          return (
            <div className="git-section">
              <div className="git-section-title">Staged Changes</div>
              <div className="files-list">
                {this.state.status.staged.map((file: any, index: number) => (
                  <div key={index} className="file-item staged">
                    <span className={`badge status-${file.status.toLowerCase()}`}>{file.status}</span>
                    <span className="file-path">{file.path}</span>
                  </div>
                ))}
              </div>
            </div>
          );
        },
        title: () => '',
        order: 2
      },
      {
        id: 'git-modified',
        dataSource: { type: 'state' },
        renderItem: () => {
          if (!this.state.status) {
            return <div className="empty">No changes</div>;
          }

          const modified = this.state.status.modified || [];
          const untracked = this.state.status.untracked || [];
          const deleted = this.state.status.deleted || [];
          const renamed = this.state.status.renamed || [];

          if (modified.length === 0 && untracked.length === 0 && deleted.length === 0 && renamed.length === 0) {
            return <div className="empty">Working tree clean</div>;
          }

          return (
            <div className="git-changes">
              {modified.length > 0 && (
                <div className="git-file-group">
                  <div className="git-group-title">Modified</div>
                  {modified.map((file: any, index: number) => (
                    <div key={`mod-${index}`} className="file-item modified">
                      <span className="badge status-m">M</span>
                      <span className="file-path">{file.path}</span>
                    </div>
                  ))}
                </div>
              )}

              {untracked.length > 0 && (
                <div className="git-file-group">
                  <div className="git-group-title">Untracked</div>
                  {untracked.map((file: any, index: number) => (
                    <div key={`untr-${index}`} className="file-item untracked">
                      <span className="badge status-?">?</span>
                      <span className="file-path">{file.path}</span>
                    </div>
                  ))}
                </div>
              )}

              {deleted.length > 0 && (
                <div className="git-file-group">
                  <div className="git-group-title">Deleted</div>
                  {deleted.map((file: any, index: number) => (
                    <div key={`del-${index}`} className="file-item deleted">
                      <span className="badge status-d">D</span>
                      <span className="file-path">{file.path}</span>
                    </div>
                  ))}
                </div>
              )}

              {renamed.length > 0 && (
                <div className="git-file-group">
                  <div className="git-group-title">Renamed</div>
                  {renamed.map((file: any, index: number) => (
                    <div key={`ren-${index}`} className="file-item renamed">
                      <span className="badge status-r">R</span>
                      <span className="file-path">{file.path}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        },
        title: () => '',
        order: 3
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'refresh':
        this.fetchStatus();
        return { success: true };
      case 'stage-file':
        if (action.payload?.path) {
          this.apiService.stageFile(action.payload.path)
            .then(() => this.fetchStatus())
            .catch(err => console.error('Failed to stage file:', err));
          return { success: true };
        }
        return { success: false, error: 'File path required' };
      case 'unstage-file':
        if (action.payload?.path) {
          this.apiService.unstageFile(action.payload.path)
            .then(() => this.fetchStatus())
            .catch(err => console.error('Failed to unstage file:', err));
          return { success: true };
        }
        return { success: false, error: 'File path required' };
      case 'discard-changes':
        if (action.payload?.path) {
          this.apiService.discardChanges(action.payload.path)
            .then(() => this.fetchStatus())
            .catch(err => console.error('Failed to discard changes:', err));
          return { success: true };
        }
        return { success: false, error: 'File path required' };
      case 'stage-all':
        this.apiService.stageAll()
          .then(() => this.fetchStatus())
          .catch(err => console.error('Failed to stage all:', err));
        return { success: true };
      case 'unstage-all':
        this.apiService.unstageAll()
          .then(() => this.fetchStatus())
          .catch(err => console.error('Failed to unstage all:', err));
        return { success: true };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {
    this.listeners.clear();
    this.state = { status: null, loading: false, error: null };
  }
}
