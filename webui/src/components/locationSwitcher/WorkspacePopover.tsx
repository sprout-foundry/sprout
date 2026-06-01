import { FolderOpen, RefreshCw, Loader2 } from 'lucide-react';
import React from 'react';
import { supportsInstances } from '../../config/mode';
import type { SproutInstance } from '../../services/api';
import { normalizePath } from './pathUtils';
import type { WorkspaceDirectory, SwitchingState, SSHFailureState, RemoteWorkspaceContext } from './types';
import { WorkspaceInstances } from './WorkspaceInstances';
import { WorkspaceRecentList } from './WorkspaceRecentList';
import { WorkspaceSSHFavorites } from './WorkspaceSSHFavorites';
import { WorkspaceSuggestionList } from './WorkspaceSuggestionList';

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
  workspaceRoot,
  daemonRoot,
  remoteContext,
  switchingState,
  sshFailure,
  showExpiredSessionRecovery,
  handleReloadWithoutSSHPath,
  handleRefresh,
  isConnected,
  isLoading,
  inputValue,
  setInputValue,
  suggestions,
  suggestionsLoading,
  suggestionsError,
  selectedIndex,
  setSelectedIndex,
  recentWorkspaceItems,
  remoteHostFavorites,
  submitWorkspaceChange,
  handleInputSubmit,
  addSSHFavoriteWorkspace,
  removeSSHFavoriteWorkspace,
  popoverRef,
  pathInputRef,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
}) => {
  return (
    <div
      ref={popoverRef}
      className="location-switcher-popover"
      // role="listbox" was wrong — this popover contains mixed content
      // (error alerts, status text, and several lists), not bare options.
      // Lighthouse flagged the missing required option children. The
      // inner WorkspaceRecentList / WorkspaceInstances components carry
      // their own list semantics. Using role="dialog" with aria-label so
      // assistive tech announces it as a popover.
      role="dialog"
      aria-label="Location switcher"
      tabIndex={0}
    >
      {/* Error + status */}
      {switchingState.error ? (
        <div id="location-switcher-error" className="location-switcher-error" role="alert">
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
            {switchingState.isSwitching ? <Loader2 size={12} className="spin" /> : <FolderOpen size={12} />}
          </button>
        </div>

        <div className="location-switcher-subtitle">
          {remoteContext
            ? 'Press Enter to switch remote paths. Arrow keys select a suggestion or recent remote workspace.'
            : 'Press Enter to switch. Arrow keys select a suggestion or recent workspace.'}
        </div>

        <WorkspaceSuggestionList
          workspaceRoot={workspaceRoot}
          remoteContext={remoteContext}
          suggestions={suggestions}
          suggestionsLoading={suggestionsLoading}
          suggestionsError={suggestionsError}
          selectedIndex={selectedIndex}
          onSelectSuggestion={submitWorkspaceChange}
        />

        {/* SSH Favorites (remote only) */}
        {remoteContext ? (
          <WorkspaceSSHFavorites
            remoteContext={remoteContext}
            workspaceRoot={workspaceRoot}
            favorites={remoteHostFavorites}
            isSwitching={switchingState.isSwitching}
            onSelectFavorite={submitWorkspaceChange}
            onFillInput={setInputValue}
            onSaveFavorite={() => addSSHFavoriteWorkspace(remoteContext.hostAlias, workspaceRoot)}
            onRemoveFavorite={(path) => removeSSHFavoriteWorkspace(remoteContext.hostAlias, path)}
          />
        ) : null}

        {/* Recent */}
        <WorkspaceRecentList
          remoteContext={remoteContext}
          recentWorkspaces={recentWorkspaceItems}
          selectedIndex={selectedIndex}
          suggestionCount={suggestions.length}
          onSelectWorkspace={submitWorkspaceChange}
        />

        <div className="location-switcher-divider" role="separator" />

        {/* Instances section */}
        {supportsInstances && !remoteContext ? (
          <WorkspaceInstances
            instances={instances}
            selectedInstancePID={selectedInstancePID}
            isSwitching={switchingState.isSwitching}
            isSwitchingInstance={isSwitchingInstance}
            onInstanceChange={onInstanceChange}
          />
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
