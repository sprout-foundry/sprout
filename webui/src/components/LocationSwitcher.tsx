import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import './LocationSwitcher.css';
import { FolderOpen, Monitor, RefreshCw, Loader2, Server } from 'lucide-react';
import { showThemedConfirm } from './ThemedDialog';
import {
  ApiService,
  SSHBrowseEntry,
  LeditInstance,
  SSHHostEntry,
  SSHSessionEntry,
  SSHWorkspaceOpenError,
} from '../services/api';
import { clientFetch, getSSHProxyContext } from '../services/clientSession';

interface LocationSwitcherProps {
  isConnected: boolean;
  instances?: LeditInstance[];
  selectedInstancePID?: number;
  isSwitchingInstance?: boolean;
  onInstanceChange?: (pid: number) => void;
  sidebarCollapsed?: boolean;
}

interface WorkspaceDirectory {
  name: string;
  path: string;
}

interface SwitchingState {
  isSwitching: boolean;
  error: string | null;
  status: string | null;
}

interface SSHFailureState {
  step?: string;
  details?: string;
  logPath?: string;
}

interface RemoteWorkspaceContext {
  hostAlias: string;
  sessionKey?: string;
  launcherUrl?: string;
  homePath?: string;
}

interface SSHBrowseQuery {
  browsePath: string;
  prefix: string;
}

const RECENT_WORKSPACES_KEY = 'ledit.recentWorkspaces';
const REMOTE_RECENT_WORKSPACES_KEY = 'ledit.remoteRecentWorkspaces';
const SSH_FAVORITE_WORKSPACES_KEY = 'ledit.sshFavoriteWorkspaces';
const MAX_RECENT_WORKSPACES = 15;
const MAX_SUGGESTIONS = 8;

const normalizePath = (rawPath: string): string => {
  let normalized = rawPath.trim().replace(/\/+/g, '/');
  if (!normalized) {
    return '';
  }
  if (!normalized.startsWith('/')) {
    normalized = `/${normalized}`;
  }
  if (normalized.length > 1 && normalized.endsWith('/')) {
    normalized = normalized.slice(0, -1);
  }
  return normalized;
};

const getPathDisplayName = (path: string): string => {
  const normalized = normalizePath(path);
  const segments = normalized.split('/').filter(Boolean);
  if (segments.length <= 2) {
    return segments.join('/') || normalized || 'No workspace';
  }
  return segments.slice(-2).join('/');
};

const collapseHomePath = (path: string, homePath?: string): string => {
  const trimmedPath = (path || '').trim();
  const trimmedHome = (homePath || '').trim();
  if (!trimmedPath) {
    return '';
  }
  if (!trimmedHome) {
    return trimmedPath;
  }
  if (trimmedPath === trimmedHome) {
    return '~';
  }
  if (trimmedPath.startsWith(`${trimmedHome}/`)) {
    return `~${trimmedPath.slice(trimmedHome.length)}`;
  }
  return trimmedPath;
};

const getSSHBrowseQuery = (rawPath: string): SSHBrowseQuery => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return { browsePath: '$HOME', prefix: '' };
  }

  const normalized = trimmed.replace(/^~(?=\/|$)/, '$HOME').replace(/\/+/g, '/');
  if (normalized === '$HOME') {
    return { browsePath: '$HOME', prefix: '' };
  }

  const endsWithSlash = normalized.endsWith('/');
  const withoutTrailingSlash =
    normalized.length > 1 && endsWithSlash ? normalized.replace(/\/+$/, '') : normalized;

  if (withoutTrailingSlash.startsWith('$HOME/')) {
    const lastSlash = withoutTrailingSlash.lastIndexOf('/');
    if (endsWithSlash) {
      return { browsePath: withoutTrailingSlash, prefix: '' };
    }
    return {
      browsePath: lastSlash > '$HOME'.length ? withoutTrailingSlash.slice(0, lastSlash) : '$HOME',
      prefix: withoutTrailingSlash.slice(lastSlash + 1),
    };
  }

  if (withoutTrailingSlash.startsWith('/')) {
    const lastSlash = withoutTrailingSlash.lastIndexOf('/');
    if (endsWithSlash) {
      return { browsePath: withoutTrailingSlash || '/', prefix: '' };
    }
    return {
      browsePath: lastSlash > 0 ? withoutTrailingSlash.slice(0, lastSlash) : '/',
      prefix: withoutTrailingSlash.slice(lastSlash + 1),
    };
  }

  return { browsePath: '$HOME', prefix: withoutTrailingSlash };
};

const readRecentWorkspaces = (): string[] => {
  if (typeof window === 'undefined') {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(RECENT_WORKSPACES_KEY);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .map((value) => (typeof value === 'string' ? normalizePath(value) : ''))
      .filter(Boolean)
      .slice(0, MAX_RECENT_WORKSPACES);
  } catch {
    return [];
  }
};

const writeRecentWorkspaces = (paths: string[]) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(
    RECENT_WORKSPACES_KEY,
    JSON.stringify(paths.slice(0, MAX_RECENT_WORKSPACES))
  );
};

const readRemoteRecentWorkspaces = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(REMOTE_RECENT_WORKSPACES_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).map(([hostAlias, value]) => [
        hostAlias,
        Array.isArray(value)
          ? value
              .map((entry) => (typeof entry === 'string' ? normalizePath(entry) : ''))
              .filter(Boolean)
              .slice(0, MAX_RECENT_WORKSPACES)
          : [],
      ])
    );
  } catch {
    return {};
  }
};

const writeRemoteRecentWorkspaces = (value: Record<string, string[]>) => {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(REMOTE_RECENT_WORKSPACES_KEY, JSON.stringify(value));
};

const readSSHFavoriteWorkspaces = (): Record<string, string[]> => {
  if (typeof window === 'undefined') {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(SSH_FAVORITE_WORKSPACES_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }
    return Object.fromEntries(
      Object.entries(parsed).map(([hostAlias, value]) => [
        hostAlias,
        Array.isArray(value)
          ? value
              .map((entry) => (typeof entry === 'string' ? normalizePath(entry) : ''))
              .filter(Boolean)
              .slice(0, MAX_RECENT_WORKSPACES)
          : [],
      ])
    );
  } catch {
    return {};
  }
};

const writeSSHFavoriteWorkspaces = (value: Record<string, string[]>) => {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    window.localStorage.setItem(SSH_FAVORITE_WORKSPACES_KEY, JSON.stringify(value));
  } catch {
    // QuotaExceededError: storage is full; the favorites won't persist this session
    // but shouldn't crash the UI.
  }
};

const LocationSwitcher: React.FC<LocationSwitcherProps> = ({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  sidebarCollapsed = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [daemonRoot, setDaemonRoot] = useState('');
  const [inputValue, setInputValue] = useState('');
  const [switchingState, setSwitchingState] = useState<SwitchingState>({
    isSwitching: false,
    error: null,
    status: null,
  });
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [isLoading, setIsLoading] = useState(false);
  const [suggestions, setSuggestions] = useState<WorkspaceDirectory[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsError, setSuggestionsError] = useState<string | null>(null);
  const [recentWorkspaces, setRecentWorkspaces] = useState<string[]>(() => readRecentWorkspaces());
  const [remoteRecentWorkspaces, setRemoteRecentWorkspaces] = useState<Record<string, string[]>>(
    () => readRemoteRecentWorkspaces()
  );
  const [sshFavoriteWorkspaces, setSshFavoriteWorkspaces] = useState<Record<string, string[]>>(
    () => readSSHFavoriteWorkspaces()
  );
  const [sshHosts, setSshHosts] = useState<SSHHostEntry[]>([]);
  const [sshSessions, setSshSessions] = useState<SSHSessionEntry[]>([]);
  const [isOpeningSshHost, setIsOpeningSshHost] = useState<string | null>(null);
  const [isClosingSshSession, setIsClosingSshSession] = useState<string | null>(null);
  const [remoteWorkspacePath, setRemoteWorkspacePath] = useState('');
  const [sshFailure, setSshFailure] = useState<SSHFailureState | null>(null);
  const [remoteContext, setRemoteContext] = useState<RemoteWorkspaceContext | null>(null);
  const [sshSessionPathDrafts, setSshSessionPathDrafts] = useState<Record<string, string>>({});
  const [selectedSshBrowseHost, setSelectedSshBrowseHost] = useState('');
  const [sshPathSuggestions, setSshPathSuggestions] = useState<WorkspaceDirectory[]>([]);
  const [sshPathSuggestionsLoading, setSshPathSuggestionsLoading] = useState(false);
  const [sshPathSuggestionsError, setSshPathSuggestionsError] = useState<string | null>(null);
  const [focusedSshSessionKey, setFocusedSshSessionKey] = useState<string | null>(null);
  const [sshSessionSuggestions, setSshSessionSuggestions] = useState<Record<string, WorkspaceDirectory[]>>({});
  const [sshSessionSuggestionsLoading, setSshSessionSuggestionsLoading] = useState<Record<string, boolean>>({});
  const [sshSessionSuggestionsError, setSshSessionSuggestionsError] = useState<Record<string, string | null>>({});
  const [sshHomePaths, setSshHomePaths] = useState<Record<string, string>>({});

  const [isSshPanelOpen, setIsSshPanelOpen] = useState(false);

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const sshBtnRef = useRef<HTMLButtonElement>(null);
  const sshPanelRef = useRef<HTMLDivElement>(null);
  const pathInputRef = useRef<HTMLInputElement>(null);
  const apiService = useRef(ApiService.getInstance());

  const persistRecentWorkspaces = useCallback((updater: (current: string[]) => string[]) => {
    setRecentWorkspaces((current) => {
      const next = updater(current)
        .map((value) => normalizePath(value))
        .filter(Boolean)
        .slice(0, MAX_RECENT_WORKSPACES);
      writeRecentWorkspaces(next);
      return next;
    });
  }, []);

  const addRecentWorkspace = useCallback((path: string) => {
    const normalized = normalizePath(path);
    if (!normalized) {
      return;
    }
    persistRecentWorkspaces((current) => [
      normalized,
      ...current.filter((entry) => entry !== normalized),
    ]);
  }, [persistRecentWorkspaces]);

  const addRemoteRecentWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) {
      return;
    }
    setRemoteRecentWorkspaces((current) => {
      const next = {
        ...current,
        [hostAlias]: [
          normalized,
          ...(current[hostAlias] || []).filter((entry) => entry !== normalized),
        ].slice(0, MAX_RECENT_WORKSPACES),
      };
      writeRemoteRecentWorkspaces(next);
      return next;
    });
  }, []);

  const addSSHFavoriteWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) {
      return;
    }
    setSshFavoriteWorkspaces((current) => {
      const next = {
        ...current,
        [hostAlias]: [
          normalized,
          ...(current[hostAlias] || []).filter((entry) => entry !== normalized),
        ].slice(0, MAX_RECENT_WORKSPACES),
      };
      writeSSHFavoriteWorkspaces(next);
      return next;
    });
  }, []);

  const removeSSHFavoriteWorkspace = useCallback((hostAlias: string, path: string) => {
    const normalized = normalizePath(path);
    if (!hostAlias || !normalized) {
      return;
    }
    setSshFavoriteWorkspaces((current) => {
      const nextEntries = (current[hostAlias] || []).filter((entry) => entry !== normalized);
      const next = { ...current };
      if (nextEntries.length > 0) {
        next[hostAlias] = nextEntries;
      } else {
        delete next[hostAlias];
      }
      writeSSHFavoriteWorkspaces(next);
      return next;
    });
  }, []);

  useEffect(() => {
    if (!isConnected) {
      setWorkspaceRoot('');
      setDaemonRoot('');
      setInputValue('');
      setSuggestions([]);
      setSuggestionsError(null);
      setSshFailure(null);
      setRemoteContext(null);
      return;
    }

    let cancelled = false;

    const loadData = async () => {
      try {
        const workspace = await apiService.current.getWorkspace();
        if (cancelled) {
          return;
        }
        const nextWorkspaceRoot = normalizePath(workspace.workspace_root || '');
        const nextDaemonRoot = normalizePath(workspace.daemon_root || '');
        setWorkspaceRoot(nextWorkspaceRoot);
        setDaemonRoot(nextDaemonRoot);
        setInputValue(nextWorkspaceRoot);
        if (workspace.ssh_context?.is_remote && workspace.ssh_context.host_alias) {
          const nextRemoteContext = {
            hostAlias: workspace.ssh_context.host_alias,
            sessionKey: workspace.ssh_context.session_key,
            launcherUrl: workspace.ssh_context.launcher_url,
            homePath: workspace.ssh_context.home_path,
          };
          setRemoteContext(nextRemoteContext);
          if (nextRemoteContext.homePath) {
            setSshHomePaths((current) => ({ ...current, [nextRemoteContext.hostAlias]: nextRemoteContext.homePath as string }));
          }
          addRemoteRecentWorkspace(nextRemoteContext.hostAlias, nextWorkspaceRoot);
        } else {
          // ssh_context absent — fall back to the proxy base injected when
          // serving the SSH proxy page (covers older remote binaries).
          const proxyCtx = getSSHProxyContext();
          if (proxyCtx) {
            const nextRemoteContext = { hostAlias: proxyCtx.hostAlias };
            setRemoteContext(nextRemoteContext);
            addRemoteRecentWorkspace(proxyCtx.hostAlias, nextWorkspaceRoot);
          } else {
            setRemoteContext(null);
            addRecentWorkspace(nextWorkspaceRoot);
          }
        }
      } catch (error) {
        // Even when the workspace API fails (e.g. older remote binary that
        // doesn't expose /api/workspace correctly), we can still detect the
        // SSH context from the proxy base that the local server injected.
        const proxyCtx = getSSHProxyContext();
        if (proxyCtx) {
          setRemoteContext({ hostAlias: proxyCtx.hostAlias });
          addRemoteRecentWorkspace(proxyCtx.hostAlias, '');
        }
        console.error('Failed to load workspace data:', error);
      }
    };

    loadData();

    return () => {
      cancelled = true;
    };
  }, [addRecentWorkspace, addRemoteRecentWorkspace, isConnected]);

  useEffect(() => {
    if (!switchingState.error && !switchingState.status) {
      return undefined;
    }
    // Never auto-clear while an SSH launch is in progress — the status messages
    // are actively updated by the stage interval and should not be wiped early.
    if (isOpeningSshHost) {
      return undefined;
    }
    const timer = window.setTimeout(() => {
      setSwitchingState((prev) => ({ ...prev, error: null, status: null }));
    }, 3000);
    return () => window.clearTimeout(timer);
  }, [switchingState.error, switchingState.status, isOpeningSshHost]);

  useEffect(() => {
    if (!isOpen && !isSshPanelOpen) {
      return;
    }
    const desktopBridge = (window as any).leditDesktop;

    let cancelled = false;
    Promise.all([
      (desktopBridge?.listSshHosts ? desktopBridge.listSshHosts() : apiService.current.getSSHHosts()),
      apiService.current.getSSHSessions().catch(() => []),
    ])
      .then(([hosts, sessions]) => {
        if (!cancelled) {
          const nextHosts = Array.isArray(hosts) ? hosts : [];
          setSshHosts(nextHosts);
          setSshSessions(Array.isArray(sessions) ? sessions : []);
          if (nextHosts.length > 0) {
            setSelectedSshBrowseHost((current) =>
              current && nextHosts.some((host) => host.alias === current) ? current : nextHosts[0].alias
            );
          } else {
            setSelectedSshBrowseHost('');
          }
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          console.error('Failed to load SSH hosts:', error);
          setSshHosts([]);
          setSshSessions([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [isOpen, isSshPanelOpen]);

  useEffect(() => {
    if ((!isOpen && !isSshPanelOpen) || remoteContext || sshHosts.length === 0 || !selectedSshBrowseHost) {
      setSshPathSuggestions([]);
      setSshPathSuggestionsLoading(false);
      setSshPathSuggestionsError(null);
      return;
    }

    const { browsePath, prefix } = getSSHBrowseQuery(remoteWorkspacePath);
    let cancelled = false;

    const loadSuggestions = async () => {
      setSshPathSuggestionsLoading(true);
      setSshPathSuggestionsError(null);
      try {
        const data = await apiService.current.browseSSHDirectory(selectedSshBrowseHost, browsePath);
        if (cancelled) {
          return;
        }
        if (data.home_path) {
          setSshHomePaths((current) => ({ ...current, [selectedSshBrowseHost]: data.home_path as string }));
        }
        const nextSuggestions = (data.files || [])
          .filter((file: SSHBrowseEntry) => file.type === 'directory')
          .map((file: SSHBrowseEntry) => ({
            name: String(file.name),
            path: String(file.path),
          }))
          .filter((entry: WorkspaceDirectory) =>
            prefix ? entry.name.toLowerCase().startsWith(prefix.toLowerCase()) : true
          )
          .slice(0, MAX_SUGGESTIONS);
        setSshPathSuggestions(nextSuggestions);
      } catch (error) {
        if (cancelled) {
          return;
        }
        setSshPathSuggestions([]);
        setSshPathSuggestionsError(
          error instanceof Error ? error.message : 'Failed to fetch remote folders'
        );
      } finally {
        if (!cancelled) {
          setSshPathSuggestionsLoading(false);
        }
      }
    };

    loadSuggestions();
    return () => {
      cancelled = true;
    };
  }, [apiService, isOpen, isSshPanelOpen, remoteContext, remoteWorkspacePath, selectedSshBrowseHost, sshHosts.length]);

  useEffect(() => {
    if ((!isOpen && !isSshPanelOpen) || remoteContext || !focusedSshSessionKey) {
      return;
    }

    const session = sshSessions.find((entry) => entry.key === focusedSshSessionKey);
    if (!session) {
      return;
    }

    const draftValue = sshSessionPathDrafts[session.key] ?? session.remote_workspace_path ?? '';
    const { browsePath, prefix } = getSSHBrowseQuery(draftValue);
    let cancelled = false;

    const loadSessionSuggestions = async () => {
      setSshSessionSuggestionsLoading((current) => ({ ...current, [session.key]: true }));
      setSshSessionSuggestionsError((current) => ({ ...current, [session.key]: null }));
      try {
        const data = await apiService.current.browseSSHDirectory(session.host_alias, browsePath);
        if (cancelled) {
          return;
        }
        if (data.home_path) {
          setSshHomePaths((current) => ({ ...current, [session.host_alias]: data.home_path as string }));
        }
        const nextSuggestions = (data.files || [])
          .filter((file: SSHBrowseEntry) => file.type === 'directory')
          .map((file: SSHBrowseEntry) => ({
            name: String(file.name),
            path: String(file.path),
          }))
          .filter((entry: WorkspaceDirectory) =>
            prefix ? entry.name.toLowerCase().startsWith(prefix.toLowerCase()) : true
          )
          .slice(0, MAX_SUGGESTIONS);
        setSshSessionSuggestions((current) => ({ ...current, [session.key]: nextSuggestions }));
      } catch (error) {
        if (cancelled) {
          return;
        }
        setSshSessionSuggestions((current) => ({ ...current, [session.key]: [] }));
        setSshSessionSuggestionsError((current) => ({
          ...current,
          [session.key]:
            error instanceof Error ? error.message : 'Failed to fetch remote folders',
        }));
      } finally {
        if (!cancelled) {
          setSshSessionSuggestionsLoading((current) => ({ ...current, [session.key]: false }));
        }
      }
    };

    loadSessionSuggestions();
    return () => {
      cancelled = true;
    };
  }, [focusedSshSessionKey, isOpen, isSshPanelOpen, remoteContext, sshSessionPathDrafts, sshSessions]);

  useEffect(() => {
    if (!isOpen && !isSshPanelOpen) {
      return undefined;
    }

    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      const inWorkspacePopover = popoverRef.current?.contains(target);
      const inTrigger = triggerRef.current?.contains(target);
      const inSshPanel = sshPanelRef.current?.contains(target);
      const inSshBtn = sshBtnRef.current?.contains(target);

      if (!inWorkspacePopover && !inTrigger) {
        setIsOpen(false);
        setSelectedIndex(-1);
        if (!inSshPanel && !inSshBtn) {
          setSwitchingState({ isSwitching: false, error: null, status: null });
          setSshFailure(null);
        }
      }
      if (!inSshPanel && !inSshBtn) {
        setIsSshPanelOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen, isSshPanelOpen]);

  useEffect(() => {
    if (!isOpen || !pathInputRef.current) {
      return;
    }
    const timer = window.setTimeout(() => {
      pathInputRef.current?.focus();
      pathInputRef.current?.select();
    }, 50);
    return () => window.clearTimeout(timer);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen || !isConnected) {
      return;
    }

    const normalizedInput = normalizePath(inputValue);
    if (!normalizedInput) {
      setSuggestions([]);
      setSuggestionsError(null);
      setSuggestionsLoading(false);
      return;
    }

    const endsWithSlash = inputValue.trim().endsWith('/');
    const parentPath = endsWithSlash ? normalizedInput : normalizePath(normalizedInput.split('/').slice(0, -1).join('/')) || '/';
    const prefix = endsWithSlash ? '' : normalizedInput.split('/').filter(Boolean).pop() || '';

    let cancelled = false;

    const loadSuggestions = async () => {
      setSuggestionsLoading(true);
      setSuggestionsError(null);

      try {
        const response = await clientFetch(`/api/workspace/browse?path=${encodeURIComponent(parentPath)}`);
        if (!response.ok) {
          const text = await response.text();
          throw new Error(text || 'Failed to fetch matching folders');
        }
        const contentType = response.headers.get('Content-Type') || '';
        if (!contentType.includes('application/json')) {
          // Remote backend returned non-JSON (e.g. old binary serving index.html).
          // Skip suggestions silently — no error displayed.
          setSuggestions([]);
          setSuggestionsLoading(false);
          return;
        }
        const data = await response.json();
        if (cancelled) {
          return;
        }

        const nextSuggestions = (data.files || [])
          .filter((file: any) => file.type === 'directory' && !String(file.name || '').startsWith('.'))
          .map((file: any) => ({
            name: String(file.name),
            path: normalizePath(String(file.path)),
          }))
          .filter((entry: WorkspaceDirectory) => {
            if (!prefix) {
              return true;
            }
            return entry.name.toLowerCase().startsWith(prefix.toLowerCase());
          })
          .sort((a: WorkspaceDirectory, b: WorkspaceDirectory) => a.name.localeCompare(b.name))
          .slice(0, MAX_SUGGESTIONS);

        setSuggestions(nextSuggestions);
        setSuggestionsLoading(false);
      } catch (error) {
        if (cancelled) {
          return;
        }
        setSuggestions([]);
        setSuggestionsLoading(false);
        setSuggestionsError(error instanceof Error ? error.message : 'Failed to fetch matching folders');
      }
    };

    loadSuggestions();

    return () => {
      cancelled = true;
    };
  }, [inputValue, isConnected, isOpen]);

  useEffect(() => {
    const suggestionCount = suggestions.length;
    const recentCount = recentWorkspaces.length;
    const maxIndex = suggestionCount + recentCount - 1;
    if (selectedIndex > maxIndex) {
      setSelectedIndex(-1);
    }
  }, [recentWorkspaces.length, selectedIndex, suggestions.length]);

  const triggerWidth = useMemo(() => {
    if (triggerRef.current) {
      return `${triggerRef.current.offsetWidth}px`;
    }
    return undefined;
  }, []);

  const truncatedWorkspaceName = useMemo(() => getPathDisplayName(workspaceRoot), [workspaceRoot]);
  const triggerWorkspaceName = useMemo(() => {
    if (!remoteContext?.homePath) {
      return truncatedWorkspaceName;
    }
    return collapseHomePath(workspaceRoot, remoteContext.homePath);
  }, [remoteContext?.homePath, truncatedWorkspaceName, workspaceRoot]);

  const showText = !sidebarCollapsed;

  const recentWorkspaceItems = useMemo(() => {
    const source = remoteContext?.hostAlias
      ? (remoteRecentWorkspaces[remoteContext.hostAlias] || [])
      : recentWorkspaces;
    return source
      .filter((path) => path !== workspaceRoot)
      .slice(0, MAX_RECENT_WORKSPACES);
  }, [recentWorkspaces, remoteContext?.hostAlias, remoteRecentWorkspaces, workspaceRoot]);

  const selectedHostFavorites = useMemo(() => {
    if (!selectedSshBrowseHost) {
      return [];
    }
    return sshFavoriteWorkspaces[selectedSshBrowseHost] || [];
  }, [selectedSshBrowseHost, sshFavoriteWorkspaces]);

  const remoteHostFavorites = useMemo(() => {
    if (!remoteContext?.hostAlias) {
      return [];
    }
    return (sshFavoriteWorkspaces[remoteContext.hostAlias] || []).filter((path) => path !== workspaceRoot);
  }, [remoteContext?.hostAlias, sshFavoriteWorkspaces, workspaceRoot]);

  const submitWorkspaceChange = useCallback(async (targetPath: string) => {
    const normalizedTarget = normalizePath(targetPath);
    if (!normalizedTarget || normalizedTarget === workspaceRoot) {
      setInputValue(workspaceRoot);
      return;
    }

    setSwitchingState({ isSwitching: true, error: null, status: 'Switching workspace…' });

    try {
      try {
        const sessionCount = await apiService.current.getTerminalSessionCount();
        if (sessionCount > 0) {
          const confirmed = await showThemedConfirm(
            `${sessionCount} terminal session${sessionCount === 1 ? ' is' : 's are'} active. Switching workspace will close ${sessionCount === 1 ? 'it' : 'them'}. Continue?`,
            { title: 'Active Terminal Sessions', type: 'warning' }
          );
          if (!confirmed) {
            setSwitchingState({ isSwitching: false, error: null, status: null });
            return;
          }
        }
      } catch {
        // Continue if session count cannot be checked.
      }

      const response = await apiService.current.setWorkspace(normalizedTarget);
      const nextWorkspaceRoot = normalizePath(response.workspace_root || normalizedTarget);
      setWorkspaceRoot(nextWorkspaceRoot);
      setInputValue(nextWorkspaceRoot);
      if (response.ssh_context?.is_remote && response.ssh_context.host_alias) {
        const nextRemoteContext = {
          hostAlias: response.ssh_context.host_alias,
          sessionKey: response.ssh_context.session_key,
          launcherUrl: response.ssh_context.launcher_url,
          homePath: response.ssh_context.home_path,
        };
        setRemoteContext(nextRemoteContext);
        if (nextRemoteContext.homePath) {
          setSshHomePaths((current) => ({ ...current, [nextRemoteContext.hostAlias]: nextRemoteContext.homePath as string }));
        }
        addRemoteRecentWorkspace(nextRemoteContext.hostAlias, nextWorkspaceRoot);
      } else if (remoteContext?.hostAlias) {
        addRemoteRecentWorkspace(remoteContext.hostAlias, nextWorkspaceRoot);
      } else {
        addRecentWorkspace(nextWorkspaceRoot);
      }
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : 'Failed to switch to this folder';

      if (errorMessage.includes('HTML response')) {
        setSwitchingState({
          isSwitching: false,
          error: 'Remote workspace API is unavailable on this backend. Update the remote ledit binary.',
          status: null,
        });
        return;
      }

      if (
        errorMessage.toLowerCase().includes('query') &&
        errorMessage.toLowerCase().includes('progress')
      ) {
        setSwitchingState({
          isSwitching: false,
          error: 'Cannot switch while a query is running',
          status: null,
        });
      } else {
        setSwitchingState({ isSwitching: false, error: errorMessage, status: null });
      }
      return;
    }

    setSwitchingState({ isSwitching: false, error: null, status: null });
    setIsOpen(false);
    setSelectedIndex(-1);
  }, [addRecentWorkspace, addRemoteRecentWorkspace, remoteContext?.hostAlias, workspaceRoot]);

  const handleInputSubmit = useCallback(async () => {
    if (selectedIndex >= 0 && selectedIndex < suggestions.length) {
      await submitWorkspaceChange(suggestions[selectedIndex].path);
      return;
    }

    const recentIndex = selectedIndex - suggestions.length;
    if (recentIndex >= 0 && recentIndex < recentWorkspaceItems.length) {
      await submitWorkspaceChange(recentWorkspaceItems[recentIndex]);
      return;
    }

    await submitWorkspaceChange(inputValue);
  }, [inputValue, recentWorkspaceItems, selectedIndex, submitWorkspaceChange, suggestions]);

  const handleRefresh = useCallback(async () => {
    if (!isConnected) {
      return;
    }

    setIsLoading(true);
    try {
      const workspace = await apiService.current.getWorkspace();
      const nextWorkspaceRoot = normalizePath(workspace.workspace_root || '');
      const nextDaemonRoot = normalizePath(workspace.daemon_root || '');
      setWorkspaceRoot(nextWorkspaceRoot);
      setDaemonRoot(nextDaemonRoot);
      setInputValue(nextWorkspaceRoot);
      if (workspace.ssh_context?.is_remote && workspace.ssh_context.host_alias) {
        const nextRemoteContext = {
          hostAlias: workspace.ssh_context.host_alias,
          sessionKey: workspace.ssh_context.session_key,
          launcherUrl: workspace.ssh_context.launcher_url,
          homePath: workspace.ssh_context.home_path,
        };
        setRemoteContext(nextRemoteContext);
        if (nextRemoteContext.homePath) {
          setSshHomePaths((current) => ({ ...current, [nextRemoteContext.hostAlias]: nextRemoteContext.homePath as string }));
        }
        addRemoteRecentWorkspace(nextRemoteContext.hostAlias, nextWorkspaceRoot);
      } else {
        const proxyCtx = getSSHProxyContext();
        if (proxyCtx) {
          const nextRemoteContext = { hostAlias: proxyCtx.hostAlias };
          setRemoteContext(nextRemoteContext);
          addRemoteRecentWorkspace(proxyCtx.hostAlias, nextWorkspaceRoot);
        } else {
          addRecentWorkspace(nextWorkspaceRoot);
        }
      }
      setSuggestionsError(null);
      setSshFailure(null);
    } catch (error) {
      console.error('Failed to refresh workspace data:', error);
      setSwitchingState({
        isSwitching: false,
        error: 'Failed to refresh workspace data',
        status: null,
      });
    } finally {
      setIsLoading(false);
    }
  }, [addRecentWorkspace, addRemoteRecentWorkspace, isConnected]);

  const togglePopover = useCallback(() => {
    setIsOpen((prev) => !prev);
    setIsSshPanelOpen(false);
    if (!isOpen) {
      setSelectedIndex(-1);
      setInputValue(workspaceRoot);
      setSwitchingState({ isSwitching: false, error: null, status: null });
      setSuggestionsError(null);
      setSshFailure(null);
      setFocusedSshSessionKey(null);
    }
  }, [isOpen, workspaceRoot]);

  const toggleSshPanel = useCallback(() => {
    setIsSshPanelOpen((prev) => !prev);
    setIsOpen(false);
    setSwitchingState({ isSwitching: false, error: null, status: null });
    setSshFailure(null);
  }, []);

  const handleReloadWithoutSSHPath = useCallback(() => {
    const { origin, pathname } = window.location;
    if (pathname.startsWith('/ssh/')) {
      window.location.assign(`${origin}/`);
      return;
    }
    window.location.reload();
  }, []);

  const showExpiredSessionRecovery = useMemo(() => {
    const message = switchingState.error?.toLowerCase() || '';
    return message.includes('ssh session not found or expired');
  }, [switchingState.error]);

  const handleOpenSshHost = useCallback(async (hostAlias: string, explicitRemotePath?: string) => {
    const desktopBridge = (window as any).leditDesktop;
    if (!hostAlias) {
      return;
    }
    const targetRemotePath = explicitRemotePath?.trim() || remoteWorkspacePath.trim() || undefined;

    setIsOpeningSshHost(hostAlias);
    setSshFailure(null);
    setSwitchingState({ isSwitching: false, error: null, status: `Connecting to ${hostAlias}…` });
    let statusPollCancelled = false;
    const pollLaunchStatus = async () => {
      try {
        const launchStatus = await apiService.current.getSSHLaunchStatus(hostAlias, targetRemotePath);
        if (!statusPollCancelled && launchStatus?.status) {
          setSwitchingState((prev) => ({ ...prev, status: launchStatus.status }));
        }
      } catch {
        // Ignore transient status polling failures while launch is in-flight.
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
        const response = await apiService.current.openSSHWorkspace(
          hostAlias,
          targetRemotePath
        );
        // Prefer the same-origin proxy URL so the browser stays on the same
        // port/origin, preserving PWA installation and service-worker scope.
        const targetUrl = response.proxy_url || response.url;
        if (!targetUrl) {
          throw new Error('SSH workspace did not return a local URL');
        }
        // Navigate the current tab to the proxy URL — same origin, no new tab.
        window.location.assign(targetUrl);
      }
      setIsOpen(false);
      setSshSessions(await apiService.current.getSSHSessions().catch(() => []));
      setSwitchingState({ isSwitching: false, error: null, status: `SSH workspace ready: ${hostAlias}` });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to open SSH host';
      if (error instanceof SSHWorkspaceOpenError) {
        setSshFailure({
          step: error.step,
          details: error.details,
          logPath: error.logPath,
        });
      } else {
        setSshFailure(null);
      }
      setSwitchingState({ isSwitching: false, error: message, status: null });
    } finally {
      statusPollCancelled = true;
      window.clearInterval(statusPollTimer);
      setIsOpeningSshHost(null);
    }
  }, [remoteWorkspacePath]);

  const handleCloseSshSession = useCallback(async (sessionKey: string) => {
    if (!sessionKey) {
      return;
    }
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
  }, []);

  const updateSshSessionPathDraft = useCallback((sessionKey: string, value: string) => {
    setSshSessionPathDrafts((current) => ({
      ...current,
      [sessionKey]: value,
    }));
  }, []);

  const getSshSessionTargetPath = useCallback((session: SSHSessionEntry): string | undefined => {
    const draftValue = sshSessionPathDrafts[session.key];
    const trimmedDraft = typeof draftValue === 'string' ? draftValue.trim() : '';
    if (trimmedDraft) {
      return trimmedDraft;
    }
    const savedPath = (session.remote_workspace_path || '').trim();
    return savedPath || undefined;
  }, [sshSessionPathDrafts]);

  const totalWorkspaceRows = suggestions.length + recentWorkspaceItems.length;

  useEffect(() => {
    if (!isOpen) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (document.activeElement !== pathInputRef.current) {
        return;
      }

      switch (event.key) {
        case 'Escape':
          event.preventDefault();
          setIsOpen(false);
          setSelectedIndex(-1);
          break;
        case 'ArrowDown':
          event.preventDefault();
          if (totalWorkspaceRows === 0) {
            return;
          }
          setSelectedIndex((prev) => (prev < totalWorkspaceRows - 1 ? prev + 1 : 0));
          break;
        case 'ArrowUp':
          event.preventDefault();
          if (totalWorkspaceRows === 0) {
            return;
          }
          setSelectedIndex((prev) => (prev <= 0 ? totalWorkspaceRows - 1 : prev - 1));
          break;
        case 'Enter':
          event.preventDefault();
          handleInputSubmit();
          break;
        default:
          break;
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [handleInputSubmit, isOpen, totalWorkspaceRows]);

  return (
    <div
      className={`location-switcher ${sidebarCollapsed ? 'collapsed' : ''} ${
        switchingState.isSwitching || isLoading ? 'loading' : ''
      }`}
    >
      {/* ── Host / connection indicator button (icon only) ── */}
      <button
        ref={sshBtnRef}
        type="button"
        className={`location-host-btn ${remoteContext ? 'ssh-active' : ''}`}
        onClick={toggleSshPanel}
        aria-expanded={isSshPanelOpen}
        aria-haspopup="listbox"
        disabled={!isConnected}
        title={remoteContext ? `SSH: ${remoteContext.hostAlias} — click to manage` : 'Local — click to connect via SSH'}
      >
        {remoteContext ? (
          <Server size={13} className="location-host-btn-icon" />
        ) : (
          <Monitor size={13} className="location-host-btn-icon" />
        )}
      </button>

      {/* ── Workspace picker button ── */}
      <button
        ref={triggerRef}
        type="button"
        className="location-switcher-trigger"
        onClick={togglePopover}
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        disabled={!isConnected || (isLoading && !isOpen)}
        title={workspaceRoot || 'No workspace'}
      >
        <FolderOpen size={14} className="location-switcher-trigger-icon" />
        {showText && (
          <span className="location-switcher-trigger-text">{triggerWorkspaceName}</span>
        )}
        {switchingState.isSwitching ? (
          <Loader2 size={12} className="spin" />
        ) : (
          <span className="location-switcher-trigger-chevron" />
        )}
      </button>

      {/* ── SSH panel popover ── */}
      {isSshPanelOpen ? (
        <div
          ref={sshPanelRef}
          className="location-switcher-popover location-ssh-panel"
          role="listbox"
          aria-label="SSH connection panel"
          tabIndex={0}
        >
          {switchingState.error ? (
            <div className="location-switcher-error" role="alert">
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
                    <>SSH — <strong>{sshHosts[0].alias}</strong></>
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
                  <input
                    type="text"
                    className="location-switcher-ssh-input"
                    value={remoteWorkspacePath}
                    onChange={(event) => setRemoteWorkspacePath(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && selectedSshBrowseHost) {
                        handleOpenSshHost(selectedSshBrowseHost);
                      }
                    }}
                    placeholder="Remote path (default: $HOME)"
                    disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                    title="Optional remote working directory"
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <button
                    type="button"
                    className="location-switcher-session-btn primary"
                    onClick={() => handleOpenSshHost(selectedSshBrowseHost)}
                    disabled={!selectedSshBrowseHost || Boolean(isOpeningSshHost) || switchingState.isSwitching}
                  >
                    {isOpeningSshHost ? <Loader2 size={12} className="spin" /> : 'Connect'}
                  </button>
                  <button
                    type="button"
                    className="location-switcher-session-btn"
                    onClick={() => addSSHFavoriteWorkspace(selectedSshBrowseHost, remoteWorkspacePath)}
                    disabled={
                      !selectedSshBrowseHost ||
                      !normalizePath(remoteWorkspacePath) ||
                      Boolean(isOpeningSshHost) ||
                      switchingState.isSwitching
                    }
                    title="Save this path as a favorite"
                  >
                    Save
                  </button>
                </div>
                {selectedHostFavorites.length > 0 ? (
                  <>
                    <div className="location-switcher-section-header" role="presentation">
                      SSH Favorites
                    </div>
                    <div className="location-switcher-directory-list">
                      {selectedHostFavorites.map((path) => (
                        <div
                          key={`ssh-favorite-${selectedSshBrowseHost}-${path}`}
                          className="location-switcher-item location-switcher-item-session"
                        >
                          <span className="location-switcher-item-text">{getPathDisplayName(path)}</span>
                          <span className="location-switcher-item-meta">
                            {collapseHomePath(path, sshHomePaths[selectedSshBrowseHost])}
                          </span>
                          <div className="location-switcher-session-actions">
                            <button
                              type="button"
                              className="location-switcher-session-btn"
                              onClick={() => handleOpenSshHost(selectedSshBrowseHost, path)}
                              disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                            >
                              Connect
                            </button>
                            <button
                              type="button"
                              className="location-switcher-session-btn"
                              onClick={() => setRemoteWorkspacePath(path)}
                              disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                            >
                              Fill
                            </button>
                            <button
                              type="button"
                              className="location-switcher-session-btn danger"
                              onClick={() => removeSSHFavoriteWorkspace(selectedSshBrowseHost, path)}
                              disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                            >
                              Remove
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  </>
                ) : null}
                {sshPathSuggestionsLoading ? (
                  <div className="location-switcher-directory-loading">
                    <Loader2 size={14} className="spin" />
                    <span>Finding remote folders on {selectedSshBrowseHost}...</span>
                  </div>
                ) : null}
                {sshPathSuggestionsError ? (
                  <div className="location-switcher-directory-error">
                    <span>{sshPathSuggestionsError}</span>
                  </div>
                ) : null}
                {!sshPathSuggestionsLoading && sshPathSuggestions.length > 0 ? (
                  <div className="location-switcher-directory-list location-switcher-ssh-suggestions">
                    {sshPathSuggestions.map((dir) => (
                      <button
                        key={`ssh-suggestion-${selectedSshBrowseHost}-${dir.path}`}
                        type="button"
                        className="location-switcher-item"
                        onClick={() => setRemoteWorkspacePath(dir.path)}
                      >
                        <span className="location-switcher-item-text">{dir.name}</span>
                        <span className="location-switcher-item-meta">
                          {collapseHomePath(dir.path, sshHomePaths[selectedSshBrowseHost])}
                        </span>
                      </button>
                    ))}
                  </div>
                ) : null}
              </>
            ) : (
              !remoteContext ? (
                <div className="location-switcher-item location-switcher-item-empty" role="option" aria-selected={false}>
                  <span className="location-switcher-item-text">No SSH hosts found in ~/.ssh/config</span>
                </div>
              ) : null
            )}

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
                      {collapseHomePath(
                        session.remote_workspace_path || '$HOME',
                        sshHomePaths[session.host_alias]
                      )}
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
                        <div className="location-switcher-session-error">
                          {sshSessionSuggestionsError[session.key]}
                        </div>
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
                          Boolean(isOpeningSshHost) ||
                          Boolean(isClosingSshSession) ||
                          !getSshSessionTargetPath(session)
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
                          Boolean(isOpeningSshHost) ||
                          Boolean(isClosingSshSession) ||
                          !getSshSessionTargetPath(session)
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
      ) : null}

      {isOpen ? (
        <div
          ref={popoverRef}
          className="location-switcher-popover"
          style={{
            ['--trigger-width' as any]: triggerWidth,
          }}
          role="listbox"
          aria-label="Location switcher"
          tabIndex={0}
        >
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

            {!remoteContext ? (
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
                        onClick={async () => {
                          if (!onInstanceChange || !instance.pid) return;
                          const confirmed = await showThemedConfirm(
                            `Switch to instance ${instance.pid}?\n\n` +
                            `Workspace: ${instance.working_dir}\n` +
                            `Port: ${instance.port}\n\n` +
                            `This will navigate this tab to a different ledit instance. ` +
                            `Chat history and open files will not transfer.`,
                            { title: 'Switch Instance', type: 'info' }
                          );
                          if (confirmed) {
                            onInstanceChange(instance.pid);
                          }
                        }}
                        role="option"
                        aria-selected={instance.pid === selectedInstancePID}
                        aria-label={`Switch to instance ${label}`}
                        disabled={
                          switchingState.isSwitching ||
                          isSwitchingInstance ||
                          !onInstanceChange
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
      ) : null}
    </div>
  );
};

export default LocationSwitcher;
