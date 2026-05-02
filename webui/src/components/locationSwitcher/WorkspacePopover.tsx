import React from 'react';
import { FolderOpen, Monitor, RefreshCw, Loader2 } from 'lucide-react';
import { SproutInstance } from '../../services/api';
import { supportsInstances } from '../../config/mode';
import { normalizePath, collapseHomePath, getPathDisplayName } from './pathUtils';
import { WorkspaceDirectory, SwitchingState, SSHFailureState, RemoteWorkspaceContext } from './types';

export interface WorkspacePopoverProps {
  workspaceRoot: string;
  daemonRoot: string;
  remoteContext: RemoteWorkspaceContext | null;
  switchingState: SwitchingState;
  sshFailure: SSHFailureState | null;
  showExpiredSessionRecovery: boolean;
  handleReloadWithoutSSHPath: () => void;
  handleRefresh: () => Promise<void>;
  isConnected: boolean;
  isLoading: boolean;

  inputValue: string;
  setInputValue: (value: string) => void;
  suggestions: WorkspaceDirectory[];
  suggestionsLoading: boolean;
  suggestionsError: string | null;

  selectedIndex: number;
  setSelectedIndex: (index: number | ((prev: number) => number)) => void;
  totalWorkspaceRows: number;

  recentWorkspaceItems: string[];
  remoteHostFavorites: string[];
  sshFavoriteWorkspaces: Record<string, string[]>;

  submitWorkspaceChange: (targetPath: string) => Promise<void>;
  handleInputSubmit: () => Promise<void>;
  addSSHFavoriteWorkspace: (hostAlias: string, path: string) => void;
  removeSSHFavoriteWorkspace: (hostAlias: string, path: string) => void;

  popoverRef: React.RefObject<HTMLDivElement>;
  pathInputRef: React.RefObject<HTMLInputElement>;

  sidebarCollapsed?: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
}

export const WorkspacePopover: React.FC<WorkspacePopoverProps> = ({
  workspaceRoot, daemonRoot, remoteContext, switchingState, sshFailure,
  showExpiredSessionRecovery, handleReloadWithoutSSHPath, handleRefresh,
  isConnected, isLoading,
  inputValue, setInputValue, suggestions, suggestionsLoading, suggestionsError,
  selectedIndex, setSelectedIndex, totalWorkspaceRows,
  recentWorkspaceItems, remoteHostFavorites, sshFavoriteWorkspaces,
  submitWorkspaceChange, handleInputSubmit, addSSHFavoriteWorkspace, removeSSHFavoriteWorkspace,
  popoverRef, pathInputRef,
  sidebarCollapsed = false, instances = [], selectedInstancePID = 0,
  isSwitchingInstance = false, onInstanceChange,
}) => {
  return (
    <div
      ref={popoverRef}
      className="location-switcher-popover"
      role="listbox"
      aria-label="Location switcher"
      tabIndex={0}
    >
      {/* Error + status */}
      {switchingState.error ? (
        <div id="location-switcher-error" className="location-switcher-error" role="alert">
          <div>{switchingState.error}</div>
          {showExpiredSessionRecovery ? (
            <div className="location-switcher-error-actions">
              <button
                type="button"
                className="location-switcher-session-btn"
                onClick={handleReloadWithoutSSHPath}
              >
                Reload Without SSH Path
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      {switchingState.error && sshFailure ? (
        <div className="location-switcher-error-details">
          {sshFailure.step ? (
            <div className="location-switcher-error-detail-row">
              <span className="location-switcher-error-detail-label">Step</span>
              <span>{sshFailure.step}</span>
            </div>
          ) : null}
          {sshFailure.details ? (
            <pre className="location-switcher-error-detail-output">{sshFailure.details}</pre>
          ) : null}
          {sshFailure.logPath ? (
            <div className="location-switcher-error-detail-row">
              <span className="location-switcher-error-detail-label">Log</span>
              <span className="location-switcher-error-detail-path">{sshFailure.logPath}</span>
            </div>
          ) : null}
        </div>
      ) : null}
      {!switchingState.error && switchingState.status ? (
        <div className="location-switcher-status" role="status" aria-live="polite">
          {switchingState.status}
        </div>
      ) : null}

      <div className="location-switcher-content">
        <div className="location-switcher-section-header" role="presentation">
          <FolderOpen size={12} className="location-switcher-section-icon" />
          Workspace
        </div>

        {remoteContext ? (
          <div className="location-switcher-remote-context">
            <span className="location-switcher-remote-badge">Remote</span>
            <span className="location-switcher-remote-host">{remoteContext.hostAlias}</span>
            <span className="location-switcher-remote-meta">
              Switching paths here affects the remote host directly.
            </span>
            {remoteContext.launcherUrl ? (
              <a
                className="location-switcher-remote-link"
                href={remoteContext.launcherUrl}
                target="_blank"
                rel="noreferrer"
              >
                Return to launcher
              </a>
            ) : null}
          </div>
        ) : null}

        <div className="location-switcher-path-input-container">
          <input
            ref={pathInputRef}
            type="text"
            className="location-switcher-path-input"
            value={inputValue}
            onChange={(event) => {
              setInputValue(event.target.value);
              setSelectedIndex(-1);
            }}
            placeholder={daemonRoot ? `Path within ${daemonRoot}` : 'Open path...'}
            disabled={!isConnected || switchingState.isSwitching}
            title="Type a workspace path and press Enter"
            autoComplete="off"
            spellCheck={false}
          />
          <button
            type="button"
            className="location-switcher-path-input-refresh"
            onClick={handleInputSubmit}
            disabled={!isConnected || switchingState.isSwitching || !normalizePath(inputValue)}
            title="Switch workspace"
          >
            {switchingState.isSwitching ? (
              <Loader2 size={12} className="spin" />
            ) : (
              <FolderOpen size={12} />
            )}
          </button>
        </div>

        <div className="location-switcher-subtitle">
          {remoteContext
            ? 'Press Enter to switch remote paths. Arrow keys select a suggestion or recent remote workspace.'
            : 'Press Enter to switch. Arrow keys select a suggestion or recent workspace.'}
        </div>

        {suggestionsLoading ? (
          <div className="location-switcher-directory-loading">
            <Loader2 size={14} className="spin" />
            <span>Finding folders...</span>
          </div>
        ) : null}

        {suggestionsError ? (
          <div className="location-switcher-directory-error">
            <span>{suggestionsError}</span>
          </div>
        ) : null}

        {!suggestionsLoading && suggestions.length > 0 ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              Suggestions
            </div>
            <div className="location-switcher-directory-list">
              {suggestions.map((dir, index) => (
                <button
                  key={dir.path}
                  type="button"
                  className={`location-switcher-item ${
                    index === selectedIndex ? 'selected' : ''
                  } ${dir.path === workspaceRoot ? 'active' : ''}`}
                  onClick={() => submitWorkspaceChange(dir.path)}
                  role="option"
                  aria-selected={dir.path === workspaceRoot}
                >
                  <span className="location-switcher-item-text">
                    {dir.name}
                  </span>
                  <span className="location-switcher-item-meta">
                    {remoteContext
                      ? collapseHomePath(dir.path, remoteContext.homePath)
                      : dir.path}
                  </span>
                </button>
              ))}
            </div>
          </>
        ) : null}

        {/* SSH Favorites (remote only) */}
        {remoteContext ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              Favorite Paths on {remoteContext.hostAlias}
            </div>
            <div className="location-switcher-ssh-actions">
              <button
                type="button"
                className="location-switcher-session-btn"
                onClick={() => addSSHFavoriteWorkspace(remoteContext.hostAlias, workspaceRoot)}
                disabled={!workspaceRoot || switchingState.isSwitching}
              >
                Save Current Path
              </button>
            </div>
            <div className="location-switcher-recent-list">
              {remoteHostFavorites.length === 0 ? (
                <div
                  className="location-switcher-item location-switcher-item-empty"
                  role="option"
                  aria-selected={false}
                >
                  <span className="location-switcher-item-text">
                    No saved paths on this host yet
                  </span>
                </div>
              ) : (
                remoteHostFavorites.map((path) => (
                  <div
                    key={`remote-favorite-${remoteContext.hostAlias}-${path}`}
                    className="location-switcher-item location-switcher-item-session"
                  >
                    <span className="location-switcher-item-text">
                      {getPathDisplayName(path)}
                    </span>
                    <span className="location-switcher-item-meta">
                      {collapseHomePath(path, remoteContext.homePath)}
                    </span>
                    <div className="location-switcher-session-actions">
                      <button
                        type="button"
                        className="location-switcher-session-btn"
                        onClick={() => submitWorkspaceChange(path)}
                        disabled={switchingState.isSwitching}
                      >
                        Open
                      </button>
                      <button
                        type="button"
                        className="location-switcher-session-btn"
                        onClick={() => setInputValue(path)}
                        disabled={switchingState.isSwitching}
                      >
                        Fill
                      </button>
                      <button
                        type="button"
                        className="location-switcher-session-btn danger"
                        onClick={() => removeSSHFavoriteWorkspace(remoteContext.hostAlias, path)}
                        disabled={switchingState.isSwitching}
                      >
                        Remove
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        ) : null}

        {/* Recent */}
        <div className="location-switcher-section-header" role="presentation">
          {remoteContext ? `Recent Paths on ${remoteContext.hostAlias}` : 'Recent Workspaces'}
        </div>

        <div className="location-switcher-recent-list">
          {recentWorkspaceItems.length === 0 ? (
            <div
              className="location-switcher-item location-switcher-item-empty"
              role="option"
              aria-selected={false}
            >
              <span className="location-switcher-item-text">
                No recent workspaces yet
              </span>
            </div>
          ) : (
            recentWorkspaceItems.map((path, index) => {
              const rowIndex = suggestions.length + index;
              return (
                <button
                  key={path}
                  type="button"
                  className={`location-switcher-item ${
                    rowIndex === selectedIndex ? 'selected' : ''
                  }`}
                  onClick={() => submitWorkspaceChange(path)}
                  role="option"
                  aria-selected={false}
                >
                  <span className="location-switcher-item-text">
                    {getPathDisplayName(path)}
                  </span>
                  <span className="location-switcher-item-meta">
                    {remoteContext
                      ? collapseHomePath(path, remoteContext.homePath)
                      : path}
                  </span>
                </button>
              );
            })
          )}
        </div>

        <div className="location-switcher-divider" role="separator" />

        {/* Instances section */}
        {supportsInstances && !remoteContext ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              <Monitor size={12} className="location-switcher-section-icon" />
              Instances
            </div>

            {instances.length === 0 ? (
              <div
                className="location-switcher-item location-switcher-item-empty"
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">No instances available</span>
              </div>
            ) : (
              instances.map((instance) => {
                const name = instance.working_dir
                  .split('/')
                  .filter(Boolean)
                  .slice(-2)
                  .join('/');
                const label = `${name} · pid:${instance.pid}`;

                return (
                  <button
                    key={`instance-${instance.id}`}
                    type="button"
                    className={`location-switcher-item ${
                      instance.pid === selectedInstancePID ? 'active' : ''
                    }`}
                    onClick={() => {
                      if (onInstanceChange && instance.pid) {
                        onInstanceChange(instance.pid);
                      }
                    }}
                    role="option"
                    aria-selected={instance.pid === selectedInstancePID}
                    aria-label={`Switch to instance ${label}`}
                    disabled={
                      switchingState.isSwitching ||
                      isSwitchingInstance ||
                      !onInstanceChange ||
                      instance.is_host
                    }
                  >
                    <span className="location-switcher-item-text">{label}</span>
                    {instance.pid === selectedInstancePID ? (
                      <span className="location-switcher-item-indicator">●</span>
                    ) : null}
                  </button>
                );
              })
            )}
          </>
        ) : null}
      </div>

      <div className="location-switcher-footer">
        <button
          type="button"
          className="location-switcher-footer-refresh"
          onClick={handleRefresh}
          disabled={isLoading || !isConnected}
          title="Refresh workspace data"
        >
          <RefreshCw size={14} className={isLoading ? 'spin' : ''} />
          Refresh
        </button>
        <span className="location-switcher-footer-esc">Esc</span>
      </div>
    </div>
  );
};
