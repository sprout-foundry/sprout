import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { ApiService } from '../../services/api';
import { getSSHProxyContext } from '../../services/clientSession';
import { showThemedConfirm } from '../ThemedDialog';
import { normalizePath } from './pathUtils';
import type { SwitchingState, SSHFailureState, RemoteWorkspaceContext } from './types';
import { MAX_RECENT_WORKSPACES } from './types';
import {
  readRecentWorkspaces,
  writeRecentWorkspaces,
  readRemoteRecentWorkspaces,
  writeRemoteRecentWorkspaces,
  readSSHFavoriteWorkspaces,
  writeSSHFavoriteWorkspaces,
} from './workspaceStorage';

export interface UseWorkspaceDataProps {
  isConnected: boolean;
}

export interface UseWorkspaceDataResult {
  workspaceRoot: string;
  daemonRoot: string;
  remoteContext: RemoteWorkspaceContext | null;
  switchingState: SwitchingState;
  isLoading: boolean;
  sshFailure: SSHFailureState | null;
  sshHomePaths: Record<string, string>;
  showExpiredSessionRecovery: boolean;
  isOpeningSshHost: string | null;
  isClosingSshSession: string | null;
  showSSHWorkspacePicker: boolean;
  sshPickerHostAlias: string;
  sshPickerPath: string;
  setSshPickerPath: (v: string) => void;
  setShowSSHWorkspacePicker: (v: boolean) => void;
  recentWorkspaces: string[];
  remoteRecentWorkspaces: Record<string, string[]>;
  sshFavoriteWorkspaces: Record<string, string[]>;
  addRecentWorkspace: (path: string) => void;
  addRemoteRecentWorkspace: (hostAlias: string, path: string) => void;
  addSSHFavoriteWorkspace: (hostAlias: string, path: string) => void;
  removeSSHFavoriteWorkspace: (hostAlias: string, path: string) => void;
  submitWorkspaceChange: (targetPath: string) => Promise<void>;
  handleRefresh: () => Promise<void>;
  handleReloadWithoutSSHPath: () => void;
  setSwitchingState: React.Dispatch<React.SetStateAction<SwitchingState>>;
  setIsLoading: (v: boolean) => void;
  setSshFailure: (s: SSHFailureState | null) => void;
  setSshHomePaths: (updater: (c: Record<string, string>) => Record<string, string>) => void;
  setIsOpeningSshHost: (v: string | null) => void;
  setIsClosingSshSession: (v: string | null) => void;
}

export function useWorkspaceData({ isConnected }: UseWorkspaceDataProps): UseWorkspaceDataResult {
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [daemonRoot, setDaemonRoot] = useState('');
  const [remoteContext, setRemoteContext] = useState<RemoteWorkspaceContext | null>(null);
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
  const [showSSHWorkspacePicker, setShowSSHWorkspacePicker] = useState(false);
  const [sshPickerHostAlias, setSshPickerHostAlias] = useState('');
  const [sshPickerPath, setSshPickerPath] = useState('');
  const [recentWorkspaces, setRecentWorkspaces] = useState<string[]>(() => readRecentWorkspaces());
  const [remoteRecentWorkspaces, setRemoteRecentWorkspaces] = useState<Record<string, string[]>>(() =>
    readRemoteRecentWorkspaces(),
  );
  const [sshFavoriteWorkspaces, setSshFavoriteWorkspaces] = useState<Record<string, string[]>>(() =>
    readSSHFavoriteWorkspaces(),
  );
  const apiService = useRef(ApiService.getInstance());

  const persistRecentWorkspaces = useCallback((updater: (c: string[]) => string[]) => {
    setRecentWorkspaces((current) => {
      const next = updater(current)
        .map((v) => normalizePath(v))
        .filter(Boolean)
        .slice(0, MAX_RECENT_WORKSPACES);
      writeRecentWorkspaces(next);
      return next;
    });
  }, []);

  const addRecentWorkspace = useCallback(
    (path: string) => {
      const normalized = normalizePath(path);
      if (!normalized) return;
      persistRecentWorkspaces((current) => [normalized, ...current.filter((e) => e !== normalized)]);
    },
    [persistRecentWorkspaces],
  );

  const addRemoteRecentWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setRemoteRecentWorkspaces((current) => {
      const next = {
        ...current,
        [hostAlias]: [normalized, ...(current[hostAlias] || []).filter((e) => e !== normalized)].slice(
          0,
          MAX_RECENT_WORKSPACES,
        ),
      };
      writeRemoteRecentWorkspaces(next);
      return next;
    });
  }, []);

  const addSSHFavoriteWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setSshFavoriteWorkspaces((current) => {
      const next = {
        ...current,
        [hostAlias]: [normalized, ...(current[hostAlias] || []).filter((e) => e !== normalized)].slice(
          0,
          MAX_RECENT_WORKSPACES,
        ),
      };
      writeSSHFavoriteWorkspaces(next);
      return next;
    });
  }, []);

  const removeSSHFavoriteWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) return;
    setSshFavoriteWorkspaces((current) => {
      const nextEntries = (current[hostAlias] || []).filter((e) => e !== normalized);
      const next = { ...current };
      if (nextEntries.length > 0) next[hostAlias] = nextEntries;
      else delete next[hostAlias];
      writeSSHFavoriteWorkspaces(next);
      return next;
    });
  }, []);

  // Load workspace data on connect
  useEffect(() => {
    if (!isConnected) {
      setWorkspaceRoot('');
      setDaemonRoot('');
      setRemoteContext(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const workspace = await apiService.current.getWorkspace();
        if (cancelled) return;
        const nextWS = normalizePath(workspace.workspace_root || '');
        setWorkspaceRoot(nextWS);
        setDaemonRoot(normalizePath(workspace.daemon_root || ''));
        if (workspace.ssh_context?.is_remote && workspace.ssh_context.host_alias) {
          const next = {
            hostAlias: workspace.ssh_context.host_alias,
            sessionKey: workspace.ssh_context.session_key,
            launcherUrl: workspace.ssh_context.launcher_url,
            homePath: workspace.ssh_context.home_path,
          };
          setRemoteContext(next);
          if (next.homePath) setSshHomePaths((c) => ({ ...c, [next.hostAlias]: next.homePath as string }));
          addRemoteRecentWorkspace(next.hostAlias, nextWS);
        } else {
          const proxyCtx = getSSHProxyContext();
          if (proxyCtx) {
            setRemoteContext({ hostAlias: proxyCtx.hostAlias });
            addRemoteRecentWorkspace(proxyCtx.hostAlias, nextWS);
          } else {
            setRemoteContext(null);
            addRecentWorkspace(nextWS);
          }
        }
      } catch (err) {
        const proxyCtx = getSSHProxyContext();
        if (proxyCtx) {
          setRemoteContext({ hostAlias: proxyCtx.hostAlias });
          addRemoteRecentWorkspace(proxyCtx.hostAlias, '');
        }
        console.error('Failed to load workspace data:', err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [addRecentWorkspace, addRemoteRecentWorkspace, isConnected, setSshHomePaths]);

  // Post-SSH-connect workspace picker
  useEffect(() => {
    const hostAlias = window.sessionStorage.getItem('sprout:ssh-just-connected');
    if (!hostAlias) return;
    window.sessionStorage.removeItem('sprout:ssh-just-connected');
    setSshPickerHostAlias(hostAlias);
    const initialWorkspace = window.SPROUT_INITIAL_WORKSPACE;
    const isSpecificPath =
      initialWorkspace &&
      initialWorkspace !== '$HOME' &&
      !initialWorkspace.startsWith('$HOME/') &&
      !initialWorkspace.startsWith('${HOME}');
    if (isSpecificPath) {
      submitWorkspaceChange(initialWorkspace).catch(() => setShowSSHWorkspacePicker(true));
    } else setShowSSHWorkspacePicker(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Auto-clear switching state (but not while SSH operations are in progress)
  useEffect(() => {
    if (!switchingState.error && !switchingState.status) return undefined;
    if (isOpeningSshHost) return undefined;
    const timer = window.setTimeout(() => setSwitchingState((p) => ({ ...p, error: null, status: null })), 3000);
    return () => window.clearTimeout(timer);
  }, [switchingState.error, switchingState.status, isOpeningSshHost]);

  const submitWorkspaceChange = useCallback(
    async (targetPath: string) => {
      const normalizedTarget = normalizePath(targetPath);
      if (!normalizedTarget || normalizedTarget === workspaceRoot) return;
      setSwitchingState({ isSwitching: true, error: null, status: 'Switching workspace\u2026' });
      try {
        try {
          const sessionCount = await apiService.current.getTerminalSessionCount();
          if (sessionCount > 0) {
            const confirmed = await showThemedConfirm(
              `${sessionCount} terminal session${sessionCount === 1 ? ' is' : 's are'} active. Switching workspace will close ${sessionCount === 1 ? 'it' : 'them'}. Continue?`,
              { type: 'warning' },
            );
            if (!confirmed) {
              setSwitchingState({ isSwitching: false, error: null, status: null });
              return;
            }
          }
        } catch {
          /* continue */
        }
        const response = await apiService.current.setWorkspace(normalizedTarget);
        const nextWS = normalizePath(response.workspace_root || normalizedTarget);
        setWorkspaceRoot(nextWS);
        if (response.ssh_context?.is_remote && response.ssh_context.host_alias) {
          const next = {
            hostAlias: response.ssh_context.host_alias,
            sessionKey: response.ssh_context.session_key,
            launcherUrl: response.ssh_context.launcher_url,
            homePath: response.ssh_context.home_path,
          };
          setRemoteContext(next);
          if (next.homePath) setSshHomePaths((c) => ({ ...c, [next.hostAlias]: next.homePath as string }));
          addRemoteRecentWorkspace(next.hostAlias, nextWS);
        } else if (remoteContext?.hostAlias) {
          addRemoteRecentWorkspace(remoteContext.hostAlias, nextWS);
        } else addRecentWorkspace(nextWS);
        window.setTimeout(() => window.location.reload(), 300);
      } catch (error) {
        const msg = error instanceof Error ? error.message : 'Failed to switch to this folder';
        if (msg.includes('HTML response')) {
          setSwitchingState({
            isSwitching: false,
            error: 'Remote workspace API is unavailable on this backend. Update the remote Sprout binary.',
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
    [addRecentWorkspace, addRemoteRecentWorkspace, remoteContext?.hostAlias, workspaceRoot, setSshHomePaths],
  );

  const handleRefresh = useCallback(async () => {
    if (!isConnected) return;
    setIsLoading(true);
    try {
      const workspace = await apiService.current.getWorkspace();
      const nextWS = normalizePath(workspace.workspace_root || '');
      setWorkspaceRoot(nextWS);
      setDaemonRoot(normalizePath(workspace.daemon_root || ''));
      if (workspace.ssh_context?.is_remote && workspace.ssh_context.host_alias) {
        const next = {
          hostAlias: workspace.ssh_context.host_alias,
          sessionKey: workspace.ssh_context.session_key,
          launcherUrl: workspace.ssh_context.launcher_url,
          homePath: workspace.ssh_context.home_path,
        };
        setRemoteContext(next);
        if (next.homePath) setSshHomePaths((c) => ({ ...c, [next.hostAlias]: next.homePath as string }));
        addRemoteRecentWorkspace(next.hostAlias, nextWS);
      } else {
        const proxyCtx = getSSHProxyContext();
        if (proxyCtx) {
          setRemoteContext({ hostAlias: proxyCtx.hostAlias });
          addRemoteRecentWorkspace(proxyCtx.hostAlias, nextWS);
        } else addRecentWorkspace(nextWS);
      }
      setSshFailure(null);
    } catch (error) {
      console.error('Failed to refresh workspace data:', error);
      setSwitchingState({ isSwitching: false, error: 'Failed to refresh workspace data', status: null });
    } finally {
      setIsLoading(false);
    }
  }, [addRecentWorkspace, addRemoteRecentWorkspace, isConnected, setSshHomePaths]);

  const handleReloadWithoutSSHPath = useCallback(() => {
    const { origin, pathname } = window.location;
    if (pathname.startsWith('/ssh/')) window.location.assign(`${origin}/`);
    else window.location.reload();
  }, []);

  const showExpiredSessionRecovery = useMemo(() => {
    return (switchingState.error?.toLowerCase() || '').includes('ssh session not found or expired');
  }, [switchingState.error]);

  return {
    workspaceRoot,
    daemonRoot,
    remoteContext,
    switchingState,
    isLoading,
    sshFailure,
    sshHomePaths,
    showExpiredSessionRecovery,
    isOpeningSshHost,
    isClosingSshSession,
    showSSHWorkspacePicker,
    sshPickerHostAlias,
    sshPickerPath,
    setSshPickerPath,
    setShowSSHWorkspacePicker,
    recentWorkspaces,
    remoteRecentWorkspaces,
    sshFavoriteWorkspaces,
    addRecentWorkspace,
    addRemoteRecentWorkspace,
    addSSHFavoriteWorkspace,
    removeSSHFavoriteWorkspace,
    submitWorkspaceChange,
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
