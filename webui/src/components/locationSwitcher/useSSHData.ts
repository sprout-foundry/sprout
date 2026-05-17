import { useState, useEffect, useCallback, useRef } from 'react';
import { supportsSSH } from '../../config/mode';
import type { SSHBrowseEntry, SSHHostEntry, SSHSessionEntry } from '../../services/api';
import { ApiService, SSHWorkspaceOpenError } from '../../services/api';
import { getSSHBrowseQuery } from './pathUtils';
import type { WorkspaceDirectory, SwitchingState, SSHFailureState, RemoteWorkspaceContext } from './types';
import { MAX_SUGGESTIONS } from './types';

export interface UseSSHDataProps {
  isAnyPanelOpen: boolean;
  remoteContext: RemoteWorkspaceContext | null;
  setSshHomePaths: (updater: (c: Record<string, string>) => Record<string, string>) => void;
  setSwitchingState: React.Dispatch<React.SetStateAction<SwitchingState>>;
  setSshFailure: (s: SSHFailureState | null) => void;
  setIsOpeningSshHost: (v: string | null) => void;
  setIsClosingSshSession: (v: string | null) => void;
}

export interface UseSSHDataResult {
  sshHosts: SSHHostEntry[];
  sshSessions: SSHSessionEntry[];
  selectedSshBrowseHost: string;
  focusedSshSessionKey: string | null;
  sshSessionPathDrafts: Record<string, string>;
  sshSessionSuggestions: Record<string, WorkspaceDirectory[]>;
  sshSessionSuggestionsLoading: Record<string, boolean>;
  sshSessionSuggestionsError: Record<string, string | null>;
  setSelectedSshBrowseHost: (h: string) => void;
  setFocusedSshSessionKey: (k: string | null) => void;
  updateSshSessionPathDraft: (k: string, v: string) => void;
  getSshSessionTargetPath: (s: SSHSessionEntry) => string | undefined;
  handleOpenSshHost: (hostAlias: string, explicitRemotePath?: string) => Promise<void>;
  handleCloseSshSession: (sessionKey: string) => Promise<void>;
}

export function useSSHData({
  isAnyPanelOpen,
  remoteContext,
  setSshHomePaths,
  setSwitchingState,
  setSshFailure,
  setIsOpeningSshHost,
  setIsClosingSshSession,
}: UseSSHDataProps): UseSSHDataResult {
  const [sshHosts, setSshHosts] = useState<SSHHostEntry[]>([]);
  const [sshSessions, setSshSessions] = useState<SSHSessionEntry[]>([]);
  const [selectedSshBrowseHost, setSelectedSshBrowseHost] = useState('');
  const [focusedSshSessionKey, setFocusedSshSessionKey] = useState<string | null>(null);
  const [sshSessionPathDrafts, setSshSessionPathDrafts] = useState<Record<string, string>>({});
  const [sshSessionSuggestions, setSshSessionSuggestions] = useState<Record<string, WorkspaceDirectory[]>>({});
  const [sshSessionSuggestionsLoading, setSshSessionSuggestionsLoading] = useState<Record<string, boolean>>({});
  const [sshSessionSuggestionsError, setSshSessionSuggestionsError] = useState<Record<string, string | null>>({});
  const apiService = useRef(ApiService.getInstance());

  // Load SSH hosts/sessions when panels are open
  useEffect(() => {
    if (!supportsSSH || !isAnyPanelOpen) return;
    const desktopBridge = window.sproutDesktop;
    let cancelled = false;
    Promise.all([
      desktopBridge?.listSshHosts
        ? (desktopBridge.listSshHosts() as Promise<SSHHostEntry[]>)
        : apiService.current.getSSHHosts(),
      apiService.current.getSSHSessions().catch(() => []),
    ])
      .then(([hosts, sessions]) => {
        if (!cancelled) {
          const nextHosts = Array.isArray(hosts) ? hosts : [];
          setSshHosts(nextHosts);
          setSshSessions(Array.isArray(sessions) ? sessions : []);
          if (nextHosts.length > 0) {
            setSelectedSshBrowseHost((c) => (c && nextHosts.some((h) => h.alias === c) ? c : nextHosts[0].alias));
          } else setSelectedSshBrowseHost('');
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          console.error('Failed to load SSH hosts:', e);
          setSshHosts([]);
          setSshSessions([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isAnyPanelOpen]);

  // SSH session suggestions
  useEffect(() => {
    if (!isAnyPanelOpen || remoteContext || !focusedSshSessionKey) return;
    const session = sshSessions.find((e) => e.key === focusedSshSessionKey);
    if (!session) return;
    const draftValue = sshSessionPathDrafts[session.key] ?? session.remote_workspace_path ?? '';
    const { browsePath, prefix } = getSSHBrowseQuery(draftValue);
    let cancelled = false;
    (async () => {
      setSshSessionSuggestionsLoading((c) => ({ ...c, [session.key]: true }));
      setSshSessionSuggestionsError((c) => ({ ...c, [session.key]: null }));
      try {
        const data = await apiService.current.browseSSHDirectory(session.host_alias, browsePath);
        if (cancelled) return;
        if (data.home_path) setSshHomePaths((c) => ({ ...c, [session.host_alias]: data.home_path as string }));
        const next = (data.files || [])
          .filter((f: SSHBrowseEntry) => f.type === 'directory')
          .map((f: SSHBrowseEntry) => ({ name: String(f.name), path: String(f.path) }))
          .filter((e: WorkspaceDirectory) => (prefix ? e.name.toLowerCase().startsWith(prefix.toLowerCase()) : true))
          .slice(0, MAX_SUGGESTIONS);
        setSshSessionSuggestions((c) => ({ ...c, [session.key]: next }));
      } catch (error) {
        if (cancelled) return;
        setSshSessionSuggestions((c) => ({ ...c, [session.key]: [] }));
        setSshSessionSuggestionsError((c) => ({
          ...c,
          [session.key]: error instanceof Error ? error.message : 'Failed to fetch remote folders',
        }));
      } finally {
        if (!cancelled) setSshSessionSuggestionsLoading((c) => ({ ...c, [session.key]: false }));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [focusedSshSessionKey, isAnyPanelOpen, remoteContext, sshSessionPathDrafts, sshSessions, setSshHomePaths]);

  const updateSshSessionPathDraft = useCallback((k: string, v: string) => {
    setSshSessionPathDrafts((c) => ({ ...c, [k]: v }));
  }, []);

  const getSshSessionTargetPath = useCallback(
    (s: SSHSessionEntry): string | undefined => {
      const d = sshSessionPathDrafts[s.key];
      const t = typeof d === 'string' ? d.trim() : '';
      if (t) return t;
      const sp = (s.remote_workspace_path || '').trim();
      return sp || undefined;
    },
    [sshSessionPathDrafts],
  );

  const handleOpenSshHost = useCallback(
    async (hostAlias: string, explicitRemotePath?: string) => {
      const desktopBridge = window.sproutDesktop;
      if (!hostAlias) return;
      const targetRemotePath = explicitRemotePath?.trim() || undefined;
      setIsOpeningSshHost(hostAlias);
      setSshFailure(null);
      setSwitchingState({ isSwitching: false, error: null, status: `Connecting to ${hostAlias}\u2026` });
      let statusPollCancelled = false;
      const pollLaunchStatus = async () => {
        try {
          const ls = await apiService.current.getSSHLaunchStatus(hostAlias, targetRemotePath);
          if (!statusPollCancelled && ls?.status) setSwitchingState((p) => ({ ...p, status: ls.status }));
        } catch {
          /* ignore */
        }
      };
      const statusPollTimer = window.setInterval(() => {
        void pollLaunchStatus();
      }, 1000);
      void pollLaunchStatus();
      try {
        if (desktopBridge?.openSshWorkspace) {
          await desktopBridge.openSshWorkspace({
            hostAlias,
            remoteWorkspacePath: targetRemotePath,
            forceNewWindow: true,
          });
        } else {
          const response = await apiService.current.openSSHWorkspace(hostAlias, targetRemotePath);
          const targetUrl = response.proxy_url || response.url;
          if (!targetUrl) throw new Error('SSH workspace did not return a local URL');
          setSwitchingState((prev) => ({ ...prev, status: `Waiting for proxy\u2026` }));
          const healthUrl = targetUrl.endsWith('/') ? `${targetUrl}health` : `${targetUrl}/health`;
          const deadline = Date.now() + 12_000;
          while (Date.now() < deadline) {
            try {
              const hr = await fetch(healthUrl, { cache: 'no-store' });
              if (hr.ok) break;
            } catch {
              /* wait */
            }
            await new Promise<void>((r) => window.setTimeout(r, 400));
          }
          window.sessionStorage.setItem('sprout:ssh-just-connected', hostAlias);
          window.location.assign(targetUrl);
        }
        setSshSessions(await apiService.current.getSSHSessions().catch(() => []));
        setSwitchingState({ isSwitching: false, error: null, status: `SSH workspace ready: ${hostAlias}` });
      } catch (error) {
        const message = error instanceof Error ? error.message : 'Failed to open SSH host';
        if (error instanceof SSHWorkspaceOpenError) {
          setSshFailure({ step: error.step, details: error.details, logPath: error.logPath });
        } else setSshFailure(null);
        setSwitchingState({ isSwitching: false, error: message, status: null });
      } finally {
        statusPollCancelled = true;
        window.clearInterval(statusPollTimer);
        setIsOpeningSshHost(null);
      }
    },
    [setSshFailure, setSwitchingState, setIsOpeningSshHost],
  );

  const handleCloseSshSession = useCallback(
    async (sessionKey: string) => {
      if (!sessionKey) return;
      setIsClosingSshSession(sessionKey);
      try {
        await apiService.current.closeSSHSession(sessionKey);
        setSshSessions(await apiService.current.getSSHSessions().catch(() => []));
        setSwitchingState({ isSwitching: false, error: null, status: 'SSH session closed' });
      } catch (error) {
        const message = error instanceof Error ? error.message : 'Failed to close SSH session';
        setSwitchingState({ isSwitching: false, error: message, status: null });
      } finally {
        setIsClosingSshSession(null);
      }
    },
    [setSwitchingState, setIsClosingSshSession],
  );

  return {
    sshHosts,
    sshSessions,
    selectedSshBrowseHost,
    focusedSshSessionKey,
    sshSessionPathDrafts,
    sshSessionSuggestions,
    sshSessionSuggestionsLoading,
    sshSessionSuggestionsError,
    setSelectedSshBrowseHost,
    setFocusedSshSessionKey,
    updateSshSessionPathDraft,
    getSshSessionTargetPath,
    handleOpenSshHost,
    handleCloseSshSession,
  };
}
