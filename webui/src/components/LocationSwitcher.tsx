import React, { useState, useMemo } from 'react';
import { FolderOpen, Monitor, Loader2, Server } from 'lucide-react';
import './LocationSwitcher.css';
import { SproutInstance } from '../services/api';
import { supportsSSH } from '../config/mode';
import { getPathDisplayName, collapseHomePath } from './locationSwitcher/pathUtils';
import { useWorkspaceData } from './locationSwitcher/useWorkspaceData';
import { useSSHData } from './locationSwitcher/useSSHData';
import { useWorkspaceSuggestions } from './locationSwitcher/useWorkspaceSuggestions';
import { SSHPanel } from './locationSwitcher/SSHPanel';
import { WorkspacePopover } from './locationSwitcher/WorkspacePopover';
import { SSHWorkspacePickerDialog } from './locationSwitcher/SSHWorkspacePickerDialog';

export interface LocationSwitcherProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  sidebarCollapsed?: boolean;
}

const LocationSwitcher: React.FC<LocationSwitcherProps> = ({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  sidebarCollapsed = false,
}) => {
  // ─── Main component owns panel toggle state ───
  const [isOpen, setIsOpen] = useState(false);
  const [isSshPanelOpen, setIsSshPanelOpen] = useState(false);
  const isAnyPanelOpen = isOpen || isSshPanelOpen;

  // ─── Hook: Workspace data ───
  const ws = useWorkspaceData({ isConnected });

  // ─── Hook: SSH data (gets setters from workspace, knows panel state) ───
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
  const sug = useWorkspaceSuggestions({
    isConnected,
    workspaceRoot: ws.workspaceRoot,
    daemonRoot: ws.daemonRoot,
    remoteContext: ws.remoteContext,
    switchingState: ws.switchingState,
    sshFailure: ws.sshFailure,
    isLoading: ws.isLoading,
    recentWorkspaces: ws.recentWorkspaces,
    remoteRecentWorkspaces: ws.remoteRecentWorkspaces,
    sshFavoriteWorkspaces: ws.sshFavoriteWorkspaces,
    submitWorkspaceChange: ws.submitWorkspaceChange,
    setSwitchingState: ws.setSwitchingState,
    setSshFailure: ws.setSshFailure,
    sidebarCollapsed,
    isOpen,
    isSshPanelOpen,
    setIsOpen,
    setIsSshPanelOpen,
  });

  // ─── Derived values ───
  const triggerWorkspaceName = useMemo(() => {
    const display = getPathDisplayName(ws.workspaceRoot);
    if (!ws.remoteContext?.homePath) return display;
    return collapseHomePath(ws.workspaceRoot, ws.remoteContext.homePath);
  }, [ws.remoteContext?.homePath, ws.workspaceRoot]);

  // ─── Render ───
  return (
    <div className={`location-switcher ${sidebarCollapsed ? 'collapsed' : ''} ${ws.switchingState.isSwitching || ws.isLoading ? 'loading' : ''}`}>
      {supportsSSH && (
        <button
          ref={sug.sshBtnRef}
          type="button"
          className={`location-host-btn ${ws.remoteContext ? 'ssh-active' : ''}`}
          onClick={sug.toggleSshPanel}
          aria-expanded={isSshPanelOpen}
          aria-haspopup="listbox"
          disabled={!isConnected}
          title={ws.remoteContext ? `SSH: ${ws.remoteContext.hostAlias} — click to manage` : 'Local — click to connect via SSH'}
        >
          {ws.remoteContext ? <Server size={13} className="location-host-btn-icon" /> : <Monitor size={13} className="location-host-btn-icon" />}
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
        title={ws.workspaceRoot || 'No workspace'}
      >
        <FolderOpen size={14} className="location-switcher-trigger-icon" />
        {sug.showText && <span className="location-switcher-trigger-text">{triggerWorkspaceName}</span>}
        {ws.switchingState.isSwitching ? <Loader2 size={12} className="spin" /> : <span className="location-switcher-trigger-chevron" />}
      </button>

      {supportsSSH && isSshPanelOpen && (
        <SSHPanel
          remoteContext={ws.remoteContext}
          workspaceRoot={ws.workspaceRoot}
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
          addSSHFavoriteWorkspace={ws.addSSHFavoriteWorkspace}
          setSelectedSshBrowseHost={ssh.setSelectedSshBrowseHost}
          setFocusedSshSessionKey={ssh.setFocusedSshSessionKey}
          sshPanelRef={sug.sshPanelRef}
        />
      )}

      {isOpen && (
        <WorkspacePopover
          workspaceRoot={ws.workspaceRoot}
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
          totalWorkspaceRows={sug.totalWorkspaceRows}
          recentWorkspaceItems={sug.recentWorkspaceItems}
          remoteHostFavorites={sug.remoteHostFavorites}
          sshFavoriteWorkspaces={ws.sshFavoriteWorkspaces}
          submitWorkspaceChange={ws.submitWorkspaceChange}
          handleInputSubmit={sug.handleInputSubmit}
          addSSHFavoriteWorkspace={ws.addSSHFavoriteWorkspace}
          removeSSHFavoriteWorkspace={ws.removeSSHFavoriteWorkspace}
          popoverRef={sug.popoverRef}
          pathInputRef={sug.pathInputRef}
          sidebarCollapsed={sidebarCollapsed}
          instances={instances}
          selectedInstancePID={selectedInstancePID}
          isSwitchingInstance={isSwitchingInstance}
          onInstanceChange={onInstanceChange}
        />
      )}

      {supportsSSH && ws.showSSHWorkspacePicker && (
        <SSHWorkspacePickerDialog
          show={ws.showSSHWorkspacePicker}
          sshPickerHostAlias={ws.sshPickerHostAlias}
          sshPickerPath={ws.sshPickerPath}
          remoteRecentWorkspaces={ws.remoteRecentWorkspaces}
          sshFavoriteWorkspaces={ws.sshFavoriteWorkspaces}
          sshHomePaths={ws.sshHomePaths}
          submitWorkspaceChange={ws.submitWorkspaceChange}
          setShow={ws.setShowSSHWorkspacePicker}
          setSshPickerPath={ws.setSshPickerPath}
        />
      )}
    </div>
  );
};

export default LocationSwitcher;