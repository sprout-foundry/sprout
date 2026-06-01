import { Server, Loader2, RefreshCw } from 'lucide-react';
import React from 'react';
import { supportsSSH } from '../../config/mode';
import type { SSHHostEntry, SSHSessionEntry } from '../../services/api';
import { collapseHomePath } from './pathUtils';
import type { SwitchingState, SSHFailureState, RemoteWorkspaceContext, WorkspaceDirectory } from './types';

export interface SSHPanelProps {
  remoteContext: RemoteWorkspaceContext | null;
  workspaceRoot: string;
  switchingState: SwitchingState;
  sshFailure: SSHFailureState | null;
  showExpiredSessionRecovery: boolean;
  isConnected: boolean;
  isLoading: boolean;

  sshHosts: SSHHostEntry[];
  sshSessions: SSHSessionEntry[];
  isOpeningSshHost: string | null;
  isClosingSshSession: string | null;
  selectedSshBrowseHost: string;
  focusedSshSessionKey: string | null;
  sshSessionPathDrafts: Record<string, string>;
  sshSessionSuggestions: Record<string, WorkspaceDirectory[]>;
  sshSessionSuggestionsLoading: Record<string, boolean>;
  sshSessionSuggestionsError: Record<string, string | null>;
  sshHomePaths: Record<string, string>;

  handleRefresh: () => Promise<void>;
  handleReloadWithoutSSHPath: () => void;
  handleOpenSshHost: (hostAlias: string, explicitRemotePath?: string) => Promise<void>;
  handleCloseSshSession: (sessionKey: string) => Promise<void>;
  updateSshSessionPathDraft: (sessionKey: string, value: string) => void;
  getSshSessionTargetPath: (session: SSHSessionEntry) => string | undefined;
  addSSHFavoriteWorkspace: (hostAlias: string, path: string) => void;
  setSelectedSshBrowseHost: (host: string) => void;
  setFocusedSshSessionKey: (key: string | null) => void;

  sshPanelRef: React.RefObject<HTMLDivElement>;
}

export const SSHPanel: React.FC<SSHPanelProps> = ({
  remoteContext,
  workspaceRoot,
  switchingState,
  sshFailure,
  showExpiredSessionRecovery,
  isConnected,
  isLoading,
  sshHosts,
  sshSessions,
  isOpeningSshHost,
  isClosingSshSession,
  selectedSshBrowseHost,
  focusedSshSessionKey,
  sshSessionPathDrafts,
  sshSessionSuggestions,
  sshSessionSuggestionsLoading,
  sshSessionSuggestionsError,
  sshHomePaths,
  handleRefresh,
  handleReloadWithoutSSHPath,
  handleOpenSshHost,
  handleCloseSshSession,
  updateSshSessionPathDraft,
  getSshSessionTargetPath,
  addSSHFavoriteWorkspace,
  setSelectedSshBrowseHost,
  setFocusedSshSessionKey,
  sshPanelRef,
}) => {
  if (!supportsSSH) return null;

  return (
    <div
      ref={sshPanelRef}
      className="location-switcher-popover location-ssh-panel"
      // Mixed-content popover (errors + lists) — not a listbox. See
      // WorkspacePopover for the same rationale.
      role="dialog"
      aria-label="SSH connection panel"
      tabIndex={0}
    >
      {switchingState.error ? (
        <div className="location-switcher-error" role="alert">
          <div>{switchingState.error}</div>
          {showExpiredSessionRecovery ? (
            <div className="location-switcher-error-actions">
              <button type="button" className="location-switcher-session-btn" onClick={handleReloadWithoutSSHPath}>
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
        {/* Active SSH connection status */}
        {remoteContext ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              <Server size={12} className="location-switcher-section-icon" />
              SSH connection
            </div>
            <div className="location-ssh-connected-row">
              <div className="location-ssh-connected-info">
                <div className="location-ssh-connected-host">{remoteContext.hostAlias}</div>
                <div className="location-ssh-connected-path">
                  {collapseHomePath(workspaceRoot, remoteContext.homePath)}
                </div>
              </div>
              <button
                type="button"
                className="location-switcher-session-btn danger"
                onClick={() => {
                  window.location.assign(remoteContext.launcherUrl || window.location.origin + '/');
                }}
                disabled={Boolean(isClosingSshSession)}
              >
                Return to local
              </button>
            </div>
          </>
        ) : null}

        {/* SSH Hosts */}
        {sshHosts.length > 0 ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              {sshHosts.length === 1 ? (
                <>
                  SSH — <strong>{sshHosts[0].alias}</strong>
                </>
              ) : (
                'SSH Hosts'
              )}
            </div>
            {sshHosts.length > 1 ? (
              <div className="location-switcher-ssh-input-container">
                <select
                  className="location-switcher-ssh-select"
                  value={selectedSshBrowseHost}
                  onChange={(event) => setSelectedSshBrowseHost(event.target.value)}
                  disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                  title="Choose SSH host"
                >
                  {sshHosts.map((host) => (
                    <option key={`ssh-suggest-${host.alias}`} value={host.alias}>
                      {host.alias}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
            <div className="location-switcher-ssh-connect-row">
              <button
                type="button"
                className="location-switcher-session-btn primary"
                onClick={() => handleOpenSshHost(selectedSshBrowseHost)}
                disabled={!selectedSshBrowseHost || Boolean(isOpeningSshHost) || switchingState.isSwitching}
              >
                {isOpeningSshHost ? <Loader2 size={12} className="spin" /> : 'Connect'}
              </button>
            </div>
          </>
        ) : !remoteContext ? (
          <div className="location-switcher-item location-switcher-item-empty" role="option" aria-selected={false}>
            <span className="location-switcher-item-text">No SSH hosts found in ~/.ssh/config</span>
          </div>
        ) : null}

        {/* Active SSH sessions */}
        {sshSessions.length > 0 ? (
          <>
            <div className="location-switcher-section-header" role="presentation">
              SSH Sessions
            </div>
            {sshSessions.map((session) => (
              <div
                key={`ssh-session-${session.key}`}
                className={`location-switcher-item location-switcher-item-session ${session.active ? 'active' : ''}`}
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">{session.host_alias}</span>
                <span className="location-switcher-item-meta">
                  {collapseHomePath(session.remote_workspace_path || '$HOME', sshHomePaths[session.host_alias])}
                </span>
                <div className="location-switcher-session-retarget">
                  <input
                    type="text"
                    className="location-switcher-session-input"
                    value={sshSessionPathDrafts[session.key] ?? ''}
                    onChange={(event) => updateSshSessionPathDraft(session.key, event.target.value)}
                    onFocus={() => setFocusedSshSessionKey(session.key)}
                    placeholder={`Open another path on ${session.host_alias}`}
                    disabled={Boolean(isOpeningSshHost) || Boolean(isClosingSshSession)}
                    autoComplete="off"
                    spellCheck={false}
                  />
                  {focusedSshSessionKey === session.key && sshSessionSuggestionsLoading[session.key] ? (
                    <div className="location-switcher-session-hint">
                      <Loader2 size={12} className="spin" />
                      <span>Finding remote folders...</span>
                    </div>
                  ) : null}
                  {focusedSshSessionKey === session.key && sshSessionSuggestionsError[session.key] ? (
                    <div className="location-switcher-session-error">{sshSessionSuggestionsError[session.key]}</div>
                  ) : null}
                  {focusedSshSessionKey === session.key && (sshSessionSuggestions[session.key] || []).length > 0 ? (
                    <div className="location-switcher-directory-list location-switcher-session-suggestions">
                      {(sshSessionSuggestions[session.key] || []).map((dir) => (
                        <button
                          key={`ssh-session-suggestion-${session.key}-${dir.path}`}
                          type="button"
                          className="location-switcher-item"
                          onClick={() => updateSshSessionPathDraft(session.key, dir.path)}
                        >
                          <span className="location-switcher-item-text">{dir.name}</span>
                          <span className="location-switcher-item-meta">
                            {collapseHomePath(dir.path, sshHomePaths[session.host_alias])}
                          </span>
                        </button>
                      ))}
                    </div>
                  ) : null}
                </div>
                <div className="location-switcher-session-actions">
                  <button
                    type="button"
                    className="location-switcher-session-btn"
                    onClick={() => handleOpenSshHost(session.host_alias, session.remote_workspace_path)}
                    disabled={Boolean(isOpeningSshHost) || Boolean(isClosingSshSession)}
                  >
                    Open
                  </button>
                  <button
                    type="button"
                    className="location-switcher-session-btn"
                    onClick={() => handleOpenSshHost(session.host_alias, getSshSessionTargetPath(session))}
                    disabled={
                      Boolean(isOpeningSshHost) || Boolean(isClosingSshSession) || !getSshSessionTargetPath(session)
                    }
                  >
                    Open Path
                  </button>
                  <button
                    type="button"
                    className="location-switcher-session-btn"
                    onClick={() => {
                      const targetPath = getSshSessionTargetPath(session);
                      if (targetPath) {
                        addSSHFavoriteWorkspace(session.host_alias, targetPath);
                      }
                    }}
                    disabled={
                      Boolean(isOpeningSshHost) || Boolean(isClosingSshSession) || !getSshSessionTargetPath(session)
                    }
                  >
                    Save
                  </button>
                  <button
                    type="button"
                    className="location-switcher-session-btn danger"
                    onClick={() => handleCloseSshSession(session.key)}
                    disabled={Boolean(isOpeningSshHost) || Boolean(isClosingSshSession)}
                  >
                    {isClosingSshSession === session.key ? 'Closing…' : 'Close'}
                  </button>
                </div>
              </div>
            ))}
          </>
        ) : null}
      </div>

      <div className="location-switcher-footer">
        <button
          type="button"
          className="location-switcher-footer-refresh"
          onClick={handleRefresh}
          disabled={isLoading || !isConnected}
          title="Refresh SSH data"
        >
          <RefreshCw size={14} className={isLoading ? 'spin' : ''} />
          Refresh
        </button>
        <span className="location-switcher-footer-esc">Esc</span>
      </div>
    </div>
  );
};
