import { FolderOpen, Monitor, Loader2, Server } from 'lucide-react';
import React, { useState, useMemo } from 'react';
import './LocationSwitcher.css';
import { useHostCapabilities } from '../host';
import type { SproutInstance } from '../services/api';
import { getPathDisplayName, collapseHomePath } from './locationSwitcher/pathUtils';
import { SSHPanel } from './locationSwitcher/SSHPanel';
import { useSSHData } from './locationSwitcher/useSSHData';
import { useProjectFolderData } from './locationSwitcher/useWorkspaceData';
import { useProjectFolderSuggestions } from './locationSwitcher/useWorkspaceSuggestions';
import { ProjectFolderPopover } from './locationSwitcher/WorkspacePopover';

export interface LocationSwitcherProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  sidebarCollapsed?: boolean;
  /** Label the trigger with the folder's own name only, for a narrow header. */
  nameOnly?: boolean;
}

const LocationSwitcher: React.FC<LocationSwitcherProps> = ({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  sidebarCollapsed = false,
  nameOnly = false,
}) => {
  const { ssh: supportsSSH } = useHostCapabilities();
  // ─── Main component owns panel toggle state ───
  const [isOpen, setIsOpen] = useState(false);
  const [isSshPanelOpen, setIsSshPanelOpen] = useState(false);
  const isAnyPanelOpen = isOpen || isSshPanelOpen;

  // ─── Hook: Workspace data ───
  const ws = useProjectFolderData({ isConnected });

  // ─── Hook: SSH data (gets setters from project folder, knows panel state) ───
  const ssh = useSSHData({
    isAnyPanelOpen,
    remoteContext: ws.remoteContext,
    setSshHomePaths: ws.setSshHomePaths,
    setSwitchingState: ws.setSwitchingState,
    setSshFailure: ws.setSshFailure,
    setIsOpeningSshHost: ws.setIsOpeningSshHost,
    setIsClosingSshSession: ws.setIsClosingSshSession,
  });

  // ─── Hook: Suggestions, input, keyboard, panel toggles ───
  const sug = useProjectFolderSuggestions({
    isConnected,
    projectFolder: ws.projectFolder,
    daemonRoot: ws.daemonRoot,
    remoteContext: ws.remoteContext,
    switchingState: ws.switchingState,
    sshFailure: ws.sshFailure,
    isLoading: ws.isLoading,
    recentProjectFolders: ws.recentProjectFolders,
    remoteRecentProjectFolders: ws.remoteRecentProjectFolders,
    sshFavoriteProjectFolders: ws.sshFavoriteProjectFolders,
    submitProjectFolderChange: ws.submitProjectFolderChange,
    setSwitchingState: ws.setSwitchingState,
    setSshFailure: ws.setSshFailure,
    sidebarCollapsed,
    isOpen,
    isSshPanelOpen,
    setIsOpen,
    setIsSshPanelOpen,
  });

  // ─── Derived values ───
  const triggerProjectFolderName = useMemo(() => {
    if (nameOnly) return ws.projectFolder?.split('/').filter(Boolean).pop() || ws.projectFolder || '';
    const display = getPathDisplayName(ws.projectFolder);
    if (!ws.remoteContext?.homePath) return display;
    return collapseHomePath(ws.projectFolder, ws.remoteContext.homePath);
  }, [nameOnly, ws.remoteContext?.homePath, ws.projectFolder]);

  // ─── Render ───
  return (
    <div
      className={`location-switcher ${sidebarCollapsed ? 'collapsed' : ''} ${ws.switchingState.isSwitching || ws.isLoading ? 'loading' : ''}`}
    >
      {supportsSSH && (
        <button
          ref={sug.sshBtnRef}
          type="button"
          className={`location-host-btn ${ws.remoteContext ? 'ssh-active' : ''}`}
          onClick={sug.toggleSshPanel}
          aria-expanded={isSshPanelOpen}
          aria-haspopup="listbox"
          disabled={!isConnected}
          title={
            ws.remoteContext
              ? `SSH: ${ws.remoteContext.hostAlias} — click to manage`
              : 'Local — click to connect via SSH'
          }
        >
          {ws.remoteContext ? (
            <Server size={13} className="location-host-btn-icon" />
          ) : (
            <Monitor size={13} className="location-host-btn-icon" />
          )}
        </button>
      )}

      <button
        ref={sug.triggerRef}
        type="button"
        className="location-switcher-trigger"
        onClick={sug.togglePopover}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        disabled={!isConnected || (ws.isLoading && !isOpen)}
        title={ws.projectFolder || 'No project folder'}
      >
        <FolderOpen size={14} className="location-switcher-trigger-icon" />
        {sug.showText && <span className="location-switcher-trigger-text">{triggerProjectFolderName}</span>}
        {ws.switchingState.isSwitching ? (
          <Loader2 size={12} className="spin" />
        ) : (
          <span className="location-switcher-trigger-chevron" />
        )}
      </button>

      {supportsSSH && isSshPanelOpen && (
        <SSHPanel
          remoteContext={ws.remoteContext}
          projectFolder={ws.projectFolder}
          switchingState={ws.switchingState}
          sshFailure={ws.sshFailure}
          showExpiredSessionRecovery={ws.showExpiredSessionRecovery}
          isConnected={isConnected}
          isLoading={ws.isLoading}
          sshHosts={ssh.sshHosts}
          sshSessions={ssh.sshSessions}
          isOpeningSshHost={ws.isOpeningSshHost}
          isClosingSshSession={ws.isClosingSshSession}
          selectedSshBrowseHost={ssh.selectedSshBrowseHost}
          focusedSshSessionKey={ssh.focusedSshSessionKey}
          sshSessionPathDrafts={ssh.sshSessionPathDrafts}
          sshSessionSuggestions={ssh.sshSessionSuggestions}
          sshSessionSuggestionsLoading={ssh.sshSessionSuggestionsLoading}
          sshSessionSuggestionsError={ssh.sshSessionSuggestionsError}
          sshHomePaths={ws.sshHomePaths}
          handleRefresh={ws.handleRefresh}
          handleReloadWithoutSSHPath={ws.handleReloadWithoutSSHPath}
          handleOpenSshHost={ssh.handleOpenSshHost}
          handleCloseSshSession={ssh.handleCloseSshSession}
          updateSshSessionPathDraft={ssh.updateSshSessionPathDraft}
          getSshSessionTargetPath={ssh.getSshSessionTargetPath}
          addSSHFavoriteProjectFolder={ws.addSSHFavoriteProjectFolder}
          setSelectedSshBrowseHost={ssh.setSelectedSshBrowseHost}
          setFocusedSshSessionKey={ssh.setFocusedSshSessionKey}
          sshPanelRef={sug.sshPanelRef}
        />
      )}

      {isOpen && (
        <ProjectFolderPopover
          projectFolder={ws.projectFolder}
          daemonRoot={ws.daemonRoot}
          remoteContext={ws.remoteContext}
          switchingState={ws.switchingState}
          sshFailure={ws.sshFailure}
          showExpiredSessionRecovery={ws.showExpiredSessionRecovery}
          handleReloadWithoutSSHPath={ws.handleReloadWithoutSSHPath}
          handleRefresh={ws.handleRefresh}
          isConnected={isConnected}
          isLoading={ws.isLoading}
          inputValue={sug.inputValue}
          setInputValue={sug.setInputValue}
          suggestions={sug.suggestions}
          suggestionsLoading={sug.suggestionsLoading}
          suggestionsError={sug.suggestionsError}
          selectedIndex={sug.selectedIndex}
          setSelectedIndex={sug.setSelectedIndex}
          recentProjectFolderItems={sug.recentProjectFolderItems}
          remoteHostFavorites={sug.remoteHostFavorites}
          sshFavoriteProjectFolders={ws.sshFavoriteProjectFolders}
          submitProjectFolderChange={ws.submitProjectFolderChange}
          handleInputSubmit={sug.handleInputSubmit}
          addSSHFavoriteProjectFolder={ws.addSSHFavoriteProjectFolder}
          removeSSHFavoriteProjectFolder={ws.removeSSHFavoriteProjectFolder}
          popoverRef={sug.popoverRef}
          pathInputRef={sug.pathInputRef}
          sidebarCollapsed={sidebarCollapsed}
          instances={instances}
          selectedInstancePID={selectedInstancePID}
          isSwitchingInstance={isSwitchingInstance}
          onInstanceChange={onInstanceChange}
        />
      )}
    </div>
  );
};

export default LocationSwitcher;
