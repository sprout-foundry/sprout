import { SproutInstance, SSHHostEntry, SSHSessionEntry } from '../../services/api';

export interface LocationSwitcherProps {
  isConnected: boolean;
  instances?: SproutInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  sidebarCollapsed?: boolean;
}

export interface WorkspaceDirectory {
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

export interface RemoteWorkspaceContext {
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
export const RECENT_WORKSPACES_KEY = 'sprout.recentWorkspaces';
export const REMOTE_RECENT_WORKSPACES_KEY = 'sprout.remoteRecentWorkspaces';
export const SSH_FAVORITE_WORKSPACES_KEY = 'sprout.sshFavoriteWorkspaces';

export const MAX_RECENT_WORKSPACES = 15;
export const MAX_SUGGESTIONS = 8;