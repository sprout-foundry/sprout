import type { SproutInstance } from '../../services/api';

export interface LocationSwitcherProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  sidebarCollapsed?: boolean;
}

export interface ProjectFolderDirectory {
  name: string;
  path: string;
}

export interface SwitchingState {
  isSwitching: boolean;
  error: string | null;
  status: string | null;
}

export interface SSHFailureState {
  step?: string;
  details?: string;
  logPath?: string;
}

export interface RemoteProjectFolderContext {
  hostAlias: string;
  sessionKey?: string;
  launcherUrl?: string;
  homePath?: string;
}

export interface SSHBrowseQuery {
  browsePath: string;
  prefix: string;
}

// localStorage keys
export const RECENT_PROJECT_FOLDERS_KEY = 'sprout.recentProjectFolders';
export const REMOTE_RECENT_PROJECT_FOLDERS_KEY = 'sprout.remoteRecentProjectFolders';
export const SSH_FAVORITE_PROJECT_FOLDERS_KEY = 'sprout.sshFavoriteProjectFolders';
// The same keys as earlier versions spelled them; read once, then moved.
export const EARLIER_RECENT_PROJECT_FOLDERS_KEY = 'sprout.recentWorkspaces';
export const EARLIER_REMOTE_RECENT_PROJECT_FOLDERS_KEY = 'sprout.remoteRecentWorkspaces';
export const EARLIER_SSH_FAVORITE_PROJECT_FOLDERS_KEY = 'sprout.sshFavoriteWorkspaces';

export const MAX_RECENT_PROJECT_FOLDERS = 15;
export const MAX_SUGGESTIONS = 8;
