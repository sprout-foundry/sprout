import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { ApiService } from '../../services/api';
import { getSSHProxyContext, persistTabProjectFolderPath } from '../../services/clientSession';
import { applyProjectFolder, refreshProjectFolder, useProjectFolderState } from '../../services/workspaceStore';
import { showThemedConfirm } from '../ThemedDialog';
import { normalizePath } from './pathUtils';
import type { SwitchingState, SSHFailureState, RemoteProjectFolderContext } from './types';
import { MAX_RECENT_WORKSPACES } from './types';
import {
  readRecentProjectFolders,
  writeRecentProjectFolders,
  readRemoteRecentProjectFolders,
  writeRemoteRecentProjectFolders,
  readSSHFavoriteProjectFolders,
  writeSSHFavoriteProjectFolders,
} from './workspaceStorage';

export interface UseProjectFolderDataProps {
  isConnected: boolean;
}

export interface UseProjectFolderDataResult {
  workspaceRoot: string;
  daemonRoot: string;
  remoteContext: RemoteProjectFolderContext | null;
  switchingState: SwitchingState;
  isLoading: boolean;
  sshFailure: SSHFailureState | null;
  sshHomePaths: Record<string, string>;
  showExpiredSessionRecovery: boolean;
  isOpeningSshHost: string | null;
  isClosingSshSession: string | null;
  recentProjectFolders: string[];
  remoteRecentProjectFolders: Record<string, string[]>;
  sshFavoriteProjectFolders: Record<string, string[]>;
  addRecentProjectFolder: (path: string) => void;
  addRemoteRecentProjectFolder: (hostAlias: string, path: string) => void;
  addSSHFavoriteProjectFolder: (hostAlias: string, path: string) => void;
  removeSSHFavoriteProjectFolder: (hostAlias: string, path: string) => void;
  submitProjectFolderChange: (targetPath: string) => Promise<void>;
  handleRefresh: () => Promise<void>;
  handleReloadWithoutSSHPath: () => void;
  setSwitchingState: React.Dispatch<React.SetStateAction<SwitchingState>>;
  setIsLoading: (v: boolean) => void;
  setSshFailure: (s: SSHFailureState | null) => void;
  setSshHomePaths: (updater: (c: Record<string, string>) => Record<string, string>) => void;
  setIsOpeningSshHost: (v: string | null) => void;
  setIsClosingSshSession: (v: string | null) => void;
}

export function useProjectFolderData({ isConnected }: UseProjectFolderDataProps): UseProjectFolderDataResult {
  // The shared store keeps the last known project folder across disconnects, so
  // the switcher never blanks the path while the connection recovers.
  const { workspace: folderInfo, error: projectFolderError } = useProjectFolderState();
  const projectFolder = normalizePath(folderInfo?.workspace_root || '');
  const daemonRoot = normalizePath(folderInfo?.daemon_root || '');
  const [remoteContext, setRemoteContext] = useState<RemoteProjectFolderContext | null>(null);
  const [switchingState, setSwitchingState] = useState<SwitchingState>({
    isSwitching: false,
    error: null,
    status: null,
  });
  const [isLoading, setIsLoading] = useState(false);
  const [sshFailure, setSshFailure] = useState<SSHFailureState | null>(null);
  const [sshHomePaths, setSshHomePaths] = useState<Record<string, string>>({});
  const [isOpeningSshHost, setIsOpeningSshHost] = useState<string | null>(null);
  const [isClosingSshSession, setIsClosingSshSession] = useState<string | null>(null);
  const [recentProjectFolders, setRecentProjectFolders] = useState<string[]>(() => readRecentProjectFolders());
  const [remoteRecentProjectFolders, setRemoteRecentProjectFolders] = useState<Record<string, string[]>>(() =>
    readRemoteRecentProjectFolders(),
  );
  const [sshFavoriteProjectFolders, setSshFavoriteProjectFolders] = useState<Record<string, string[]>>(() =>
    readSSHFavoriteProjectFolders(),
  );
  const apiService = useRef(ApiService.getInstance());

  const persistRecentProjectFolders = useCallback((updater: (c: string[]) => string[]) => {
    setRecentProjectFolders((current) => {
      const next = updater(current)
        .map((v) => normalizePath(v))
        .filter(Boolean)
        .slice(0, MAX_RECENT_WORKSPACES);
      writeRecentProjectFolders(next);
      return next;
    });
  }, []);

  const addRecentProjectFolder = useCallback(
    (path: string) => {
      const normalized = normalizePath(path);
      if (!normalized) return;
      persistRecentProjectFolders((current) => [normalized, ...current.filter((e) => e !== normalized)]);
    },
    [persistRecentProjectFolders],
  );

  const addRemoteRecentProjectFolder = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setRemoteRecentProjectFolders((current) => {
      const next = {
        ...current,
        [hostAlias]: [normalized, ...(current[hostAlias] || []).filter((e) => e !== normalized)].slice(
          0,
          MAX_RECENT_WORKSPACES,
        ),
      };
      writeRemoteRecentProjectFolders(next);
      return next;
    });
  }, []);

  const addSSHFavoriteProjectFolder = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setSshFavoriteProjectFolders((current) => {
      const next = {
        ...current,
        [hostAlias]: [normalized, ...(current[hostAlias] || []).filter((e) => e !== normalized)].slice(
          0,
          MAX_RECENT_WORKSPACES,
        ),
      };
      writeSSHFavoriteProjectFolders(next);
      return next;
    });
  }, []);

  const removeSSHFavoriteProjectFolder = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setSshFavoriteProjectFolders((current) => {
      const nextEntries = (current[hostAlias] || []).filter((e) => e !== normalized);
      const next = { ...current };
      if (nextEntries.length > 0) next[hostAlias] = nextEntries;
      else delete next[hostAlias];
      writeSSHFavoriteProjectFolders(next);
      return next;
    });
  }, []);

  // Fetch when the connection comes up (the store joins an in-flight request).
  useEffect(() => {
    if (!isConnected) return;
    refreshProjectFolder().catch(() => {
      // best-effort: the store records the failure and retries.
    });
  }, [isConnected]);

  // Derive the remote context and record recents from each new project folder.
  useEffect(() => {
    if (!folderInfo) return;
    const nextWS = normalizePath(folderInfo.workspace_root || '');
    if (folderInfo.ssh_context?.is_remote && folderInfo.ssh_context.host_alias) {
      const next = {
        hostAlias: folderInfo.ssh_context.host_alias,
        sessionKey: folderInfo.ssh_context.session_key,
        launcherUrl: folderInfo.ssh_context.launcher_url,
        homePath: folderInfo.ssh_context.home_path,
      };
      setRemoteContext(next);
      if (next.homePath) setSshHomePaths((c) => ({ ...c, [next.hostAlias]: next.homePath as string }));
      addRemoteRecentProjectFolder(next.hostAlias, nextWS);
      return;
    }
    const proxyCtx = getSSHProxyContext();
    if (proxyCtx) {
      setRemoteContext({ hostAlias: proxyCtx.hostAlias });
      addRemoteRecentProjectFolder(proxyCtx.hostAlias, nextWS);
    } else {
      setRemoteContext(null);
      addRecentProjectFolder(nextWS);
    }
  }, [addRecentProjectFolder, addRemoteRecentProjectFolder, folderInfo, setSshHomePaths]);

  // Without any project folder yet, an SSH proxy page still knows its host.
  useEffect(() => {
    if (folderInfo || !projectFolderError) return;
    const proxyCtx = getSSHProxyContext();
    if (proxyCtx) {
      setRemoteContext({ hostAlias: proxyCtx.hostAlias });
      addRemoteRecentProjectFolder(proxyCtx.hostAlias, '');
    }
    console.error('Failed to load folderInfo data:', projectFolderError);
  }, [addRemoteRecentProjectFolder, folderInfo, projectFolderError]);

  // After an SSH connect to a specific remote folder, open it. Otherwise the
  // remote daemon sits in its home, and the project folder gate asks for a folder —
  // one chooser, not a second SSH-only picker on top of it.
  useEffect(() => {
    const hostAlias = window.sessionStorage.getItem('sprout:ssh-just-connected');
    if (!hostAlias) return;
    window.sessionStorage.removeItem('sprout:ssh-just-connected');
    const initialProjectFolder = window.SPROUT_INITIAL_WORKSPACE;
    const isSpecificPath =
      initialProjectFolder &&
      initialProjectFolder !== '$HOME' &&
      !initialProjectFolder.startsWith('$HOME/') &&
      !initialProjectFolder.startsWith('${HOME}');
    // A failed switch leaves the gate to ask.
    if (isSpecificPath) submitProjectFolderChange(initialProjectFolder).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Auto-clear switching state (but not while SSH operations are in progress)
  useEffect(() => {
    if (!switchingState.error && !switchingState.status) return undefined;
    if (isOpeningSshHost) return undefined;
    const timer = window.setTimeout(() => setSwitchingState((p) => ({ ...p, error: null, status: null })), 3000);
    return () => window.clearTimeout(timer);
  }, [switchingState.error, switchingState.status, isOpeningSshHost]);

  const submitProjectFolderChange = useCallback(
    async (targetPath: string) => {
      const normalizedTarget = normalizePath(targetPath);
      if (!normalizedTarget || normalizedTarget === projectFolder) return;
      setSwitchingState({ isSwitching: true, error: null, status: 'Switching workspace\u2026' });
      try {
        try {
          const sessionCount = await apiService.current.getTerminalSessionCount();
          if (sessionCount > 0) {
            const confirmed = await showThemedConfirm(
              `${sessionCount} terminal session${sessionCount === 1 ? ' is' : 's are'} active. Switching folderInfo will close ${sessionCount === 1 ? 'it' : 'them'}. Continue?`,
              { type: 'warning' },
            );
            if (!confirmed) {
              setSwitchingState({ isSwitching: false, error: null, status: null });
              return;
            }
          }
        } catch {
          // best-effort: the terminal-session count only refines the confirm
          // wording; if it can't be read, switch without the warning.
        }
        const response = await apiService.current.setProjectFolder(normalizedTarget);
        const nextWS = normalizePath(response.workspace_root || normalizedTarget);
        persistTabProjectFolderPath(nextWS);
        void applyProjectFolder(nextWS, response.daemon_root);
        if (response.ssh_context?.is_remote && response.ssh_context.host_alias) {
          const next = {
            hostAlias: response.ssh_context.host_alias,
            sessionKey: response.ssh_context.session_key,
            launcherUrl: response.ssh_context.launcher_url,
            homePath: response.ssh_context.home_path,
          };
          setRemoteContext(next);
          if (next.homePath) setSshHomePaths((c) => ({ ...c, [next.hostAlias]: next.homePath as string }));
          addRemoteRecentProjectFolder(next.hostAlias, nextWS);
        } else if (remoteContext?.hostAlias) {
          addRemoteRecentProjectFolder(remoteContext.hostAlias, nextWS);
        } else addRecentProjectFolder(nextWS);
        window.setTimeout(() => window.location.reload(), 300);
      } catch (error) {
        // SP-130: the backend rejects selecting the home directory without
        // consent with a structured code. Surface a friendly message and let
        // the user pick a project folder via the project folder picker instead.
        const err = error as Error & { code?: string };
        if (err.code === 'home_workspace_requires_consent') {
          setSwitchingState({
            isSwitching: false,
            error: 'Selecting your home directory requires consent. Use the folderInfo picker to confirm.',
            status: null,
          });
          return;
        }
        const msg = error instanceof Error ? error.message : 'Failed to switch to this folder';
        if (msg.includes('HTML response')) {
          setSwitchingState({
            isSwitching: false,
            error: 'Remote folderInfo API is unavailable on this backend. Update the remote Sprout binary.',
            status: null,
          });
          return;
        }
        if (msg.toLowerCase().includes('query') && msg.toLowerCase().includes('progress')) {
          setSwitchingState({ isSwitching: false, error: 'Cannot switch while a query is running', status: null });
        } else setSwitchingState({ isSwitching: false, error: msg, status: null });
        return;
      }
      setSwitchingState({ isSwitching: false, error: null, status: null });
    },
    [addRecentProjectFolder, addRemoteRecentProjectFolder, remoteContext?.hostAlias, projectFolder, setSshHomePaths],
  );

  const handleRefresh = useCallback(async () => {
    if (!isConnected) return;
    setIsLoading(true);
    try {
      await refreshProjectFolder({ force: true });
      setSshFailure(null);
    } catch (error) {
      console.error('Failed to refresh folderInfo data:', error);
      setSwitchingState({ isSwitching: false, error: 'Failed to refresh folderInfo data', status: null });
    } finally {
      setIsLoading(false);
    }
  }, [isConnected]);

  const handleReloadWithoutSSHPath = useCallback(() => {
    const { origin, pathname } = window.location;
    if (pathname.startsWith('/ssh/')) window.location.assign(`${origin}/`);
    else window.location.reload();
  }, []);

  const showExpiredSessionRecovery = useMemo(() => {
    return (switchingState.error?.toLowerCase() || '').includes('ssh session not found or expired');
  }, [switchingState.error]);

  return {
    workspaceRoot: projectFolder,
    daemonRoot,
    remoteContext,
    switchingState,
    isLoading,
    sshFailure,
    sshHomePaths,
    showExpiredSessionRecovery,
    isOpeningSshHost,
    isClosingSshSession,
    recentProjectFolders,
    remoteRecentProjectFolders,
    sshFavoriteProjectFolders,
    addRecentProjectFolder,
    addRemoteRecentProjectFolder,
    addSSHFavoriteProjectFolder,
    removeSSHFavoriteProjectFolder,
    submitProjectFolderChange,
    handleRefresh,
    handleReloadWithoutSSHPath,
    setSwitchingState,
    setIsLoading,
    setSshFailure,
    setSshHomePaths,
    setIsOpeningSshHost,
    setIsClosingSshSession,
  };
}
