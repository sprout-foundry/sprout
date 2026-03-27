import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import './LocationSwitcher.css';
import { FolderOpen, Monitor, RefreshCw, Loader2 } from 'lucide-react';
import { ApiService, LeditInstance } from '../services/api';

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

interface SSHHostEntry {
  alias: string;
  hostname?: string;
  user?: string;
  port?: string;
}

interface SwitchingState {
  isSwitching: boolean;
  error: string | null;
}

const RECENT_WORKSPACES_KEY = 'ledit.recentWorkspaces';
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
  });
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [isLoading, setIsLoading] = useState(false);
  const [suggestions, setSuggestions] = useState<WorkspaceDirectory[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [suggestionsError, setSuggestionsError] = useState<string | null>(null);
  const [recentWorkspaces, setRecentWorkspaces] = useState<string[]>(() => readRecentWorkspaces());
  const [sshHosts, setSshHosts] = useState<SSHHostEntry[]>([]);
  const [isOpeningSshHost, setIsOpeningSshHost] = useState<string | null>(null);

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
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

  useEffect(() => {
    if (!isConnected) {
      setWorkspaceRoot('');
      setDaemonRoot('');
      setInputValue('');
      setSuggestions([]);
      setSuggestionsError(null);
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
        addRecentWorkspace(nextWorkspaceRoot);
      } catch (error) {
        console.error('Failed to load workspace data:', error);
      }
    };

    loadData();

    return () => {
      cancelled = true;
    };
  }, [addRecentWorkspace, isConnected]);

  useEffect(() => {
    if (!switchingState.error) {
      return undefined;
    }
    const timer = window.setTimeout(() => {
      setSwitchingState((prev) => ({ ...prev, error: null }));
    }, 3000);
    return () => window.clearTimeout(timer);
  }, [switchingState.error]);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const desktopBridge = (window as any).leditDesktop;
    if (!desktopBridge?.listSshHosts) {
      setSshHosts([]);
      return;
    }

    let cancelled = false;
    desktopBridge.listSshHosts()
      .then((hosts: SSHHostEntry[]) => {
        if (!cancelled) {
          setSshHosts(Array.isArray(hosts) ? hosts : []);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          console.error('Failed to load SSH hosts:', error);
          setSshHosts([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) {
      return undefined;
    }

    const handleClickOutside = (event: MouseEvent) => {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(event.target as Node) &&
        triggerRef.current &&
        !triggerRef.current.contains(event.target as Node)
      ) {
        setIsOpen(false);
        setSelectedIndex(-1);
        setSwitchingState({ isSwitching: false, error: null });
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen]);

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
        const response = await fetch(`/api/workspace/browse?path=${encodeURIComponent(parentPath)}`);
        if (!response.ok) {
          throw new Error('Failed to fetch matching folders');
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

  const showText = !sidebarCollapsed;

  const recentWorkspaceItems = useMemo(() => {
    return recentWorkspaces
      .filter((path) => path !== workspaceRoot)
      .slice(0, MAX_RECENT_WORKSPACES);
  }, [recentWorkspaces, workspaceRoot]);

  const submitWorkspaceChange = useCallback(async (targetPath: string) => {
    const normalizedTarget = normalizePath(targetPath);
    if (!normalizedTarget || normalizedTarget === workspaceRoot) {
      setInputValue(workspaceRoot);
      return;
    }

    setSwitchingState({ isSwitching: true, error: null });

    try {
      try {
        const sessionCount = await apiService.current.getTerminalSessionCount();
        if (sessionCount > 0) {
          const confirmed = window.confirm(
            `${sessionCount} terminal session${sessionCount === 1 ? ' is' : 's are'} active. Switching workspace will close ${sessionCount === 1 ? 'it' : 'them'}. Continue?`
          );
          if (!confirmed) {
            setSwitchingState({ isSwitching: false, error: null });
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
      addRecentWorkspace(nextWorkspaceRoot);

      window.setTimeout(() => {
        window.location.reload();
      }, 300);
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : 'Failed to switch to this folder';

      if (
        errorMessage.toLowerCase().includes('query') &&
        errorMessage.toLowerCase().includes('progress')
      ) {
        setSwitchingState({
          isSwitching: false,
          error: 'Cannot switch while a query is running',
        });
      } else {
        setSwitchingState({ isSwitching: false, error: errorMessage });
      }
      return;
    }

    setSwitchingState({ isSwitching: false, error: null });
    setIsOpen(false);
    setSelectedIndex(-1);
  }, [addRecentWorkspace, workspaceRoot]);

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
      addRecentWorkspace(nextWorkspaceRoot);
      setSuggestionsError(null);
    } catch (error) {
      console.error('Failed to refresh workspace data:', error);
      setSwitchingState({
        isSwitching: false,
        error: 'Failed to refresh workspace data',
      });
    } finally {
      setIsLoading(false);
    }
  }, [addRecentWorkspace, isConnected]);

  const togglePopover = useCallback(() => {
    setIsOpen((prev) => !prev);
    if (!isOpen) {
      setSelectedIndex(-1);
      setInputValue(workspaceRoot);
    }
  }, [isOpen, workspaceRoot]);

  const handleOpenSshHost = useCallback(async (hostAlias: string) => {
    const desktopBridge = (window as any).leditDesktop;
    if (!desktopBridge?.openSshWorkspace || !hostAlias) {
      return;
    }

    setIsOpeningSshHost(hostAlias);
    setSwitchingState({ isSwitching: false, error: null });

    try {
      await desktopBridge.openSshWorkspace({ hostAlias, forceNewWindow: true });
      setIsOpen(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to open SSH host';
      setSwitchingState({ isSwitching: false, error: message });
    } finally {
      setIsOpeningSshHost(null);
    }
  }, []);

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
        <FolderOpen size={16} className="location-switcher-trigger-icon" />
        {showText && (
          <>
            <span className="location-switcher-trigger-text">{truncatedWorkspaceName}</span>
            {selectedInstancePID ? (
              <span className="location-switcher-trigger-pid">pid:{selectedInstancePID}</span>
            ) : null}
          </>
        )}
        {switchingState.isSwitching ? (
          <Loader2 size={12} className="spin" />
        ) : (
          <span className="location-switcher-trigger-chevron" />
        )}
      </button>

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
              {switchingState.error}
            </div>
          ) : null}

          <div className="location-switcher-content">
            <div className="location-switcher-section-header" role="presentation">
              <FolderOpen size={12} className="location-switcher-section-icon" />
              Workspace
            </div>

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
              Press Enter to switch. Arrow keys select a suggestion or recent workspace.
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
                      <span className="location-switcher-item-meta">{dir.path}</span>
                    </button>
                  ))}
                </div>
              </>
            ) : null}

            <div className="location-switcher-section-header" role="presentation">
              Recent Workspaces
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
                      <span className="location-switcher-item-meta">{path}</span>
                    </button>
                  );
                })
              )}
            </div>

            <div className="location-switcher-divider" role="separator" />

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
                    onClick={() => {
                      if (onInstanceChange && instance.pid) {
                        onInstanceChange(instance.pid);
                      }
                    }}
                    role="option"
                    aria-selected={instance.pid === selectedInstancePID}
                    aria-label={`Switch to instance ${label}`}
                    disabled={
                      switchingState.isSwitching ||
                      isSwitchingInstance ||
                      !onInstanceChange ||
                      instance.is_host
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

            {sshHosts.length > 0 ? (
              <>
                <div className="location-switcher-section-header" role="presentation">
                  SSH Hosts
                </div>
                {sshHosts.map((host) => {
                  const metaParts = [
                    host.user ? `${host.user}@${host.hostname || host.alias}` : (host.hostname || host.alias),
                    host.port ? `:${host.port}` : '',
                  ].filter(Boolean);
                  return (
                    <button
                      key={`ssh-${host.alias}`}
                      type="button"
                      className="location-switcher-item"
                      onClick={() => handleOpenSshHost(host.alias)}
                      role="option"
                      aria-selected={false}
                      disabled={Boolean(isOpeningSshHost) || switchingState.isSwitching}
                    >
                      <span className="location-switcher-item-text">{host.alias}</span>
                      <span className="location-switcher-item-meta">
                        {metaParts.join('')}
                      </span>
                      {isOpeningSshHost === host.alias ? (
                        <span className="location-switcher-item-indicator">
                          <Loader2 size={10} className="spin" />
                        </span>
                      ) : null}
                    </button>
                  );
                })}
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
