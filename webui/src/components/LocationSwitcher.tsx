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

interface SwitchingState {
  isSwitching: boolean;
  error: string | null;
}

interface BrowseState {
  currentPath: string;
  directories: WorkspaceDirectory[];
  isLoading: boolean;
  error: string | null;
}

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
  const [switchingState, setSwitchingState] = useState<SwitchingState>({
    isSwitching: false,
    error: null,
  });
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [isLoading, setIsLoading] = useState(false);

  // Browse state for file browser
  const [browseState, setBrowseState] = useState<BrowseState>({
    currentPath: '',
    directories: [],
    isLoading: false,
    error: null,
  });

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const pathInputRef = useRef<HTMLInputElement>(null);
  const apiService = useRef(ApiService.getInstance());

  // Load workspace info and initialize browse path on mount or when connected
  useEffect(() => {
    if (!isConnected) {
      setWorkspaceRoot('');
      setBrowseState({
        currentPath: '',
        directories: [],
        isLoading: false,
        error: null,
      });
      return;
    }

    let cancelled = false;

    const loadData = async () => {
      try {
        // Get workspace info
        const workspace = await apiService.current.getWorkspace();
        if (!cancelled) {
          setWorkspaceRoot(workspace.workspace_root || '');
          // Initialize browse path to current workspace root
          setBrowseState(prev => ({
            ...prev,
            currentPath: workspace.workspace_root || '',
            error: null,
          }));
        }
      } catch (error) {
        console.error('Failed to load workspace data:', error);
      }
    };

    loadData();

    return () => {
      cancelled = true;
    };
  }, [isConnected]);

  // Load directories for current browse path
  useEffect(() => {
    if (!browseState.currentPath || !isConnected) return;

    let cancelled = false;

    const loadDirectories = async () => {
      setBrowseState(prev => ({ ...prev, isLoading: true, error: null }));

      try {
        const response = await fetch(
          `/api/workspace/browse?path=${encodeURIComponent(browseState.currentPath)}`
        );
        if (!response.ok) {
          throw new Error('Failed to fetch directory contents');
        }
        const data = await response.json();
        if (!cancelled) {
          const directories: WorkspaceDirectory[] = (data.files || [])
            .filter(
              (file: any) =>
                file.type === 'directory' &&
                !file.name.startsWith('.')
            )
            .map((file: any) => ({
              name: file.name,
              path: file.path,
            }));
          setBrowseState(prev => ({
            ...prev,
            directories,
            isLoading: false,
          }));
        }
      } catch (error) {
        if (!cancelled) {
          setBrowseState(prev => ({
            ...prev,
            isLoading: false,
            error: error instanceof Error ? error.message : 'Failed to load directory',
          }));
        }
      }
    };

    loadDirectories();

    return () => {
      cancelled = true;
    };
  }, [browseState.currentPath, isConnected]);

  // Click-outside handler
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        isOpen &&
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

  // Keyboard navigation for directory list
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      // Don't intercept if focus is in path input
      if (document.activeElement === pathInputRef.current) {
        if (event.key === 'Enter') {
          event.preventDefault();
          handlePathInputChange();
        }
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
          setSelectedIndex((prev) => {
            if (browseState.directories.length === 0) return -1;
            return prev < browseState.directories.length - 1 ? prev + 1 : 0;
          });
          break;

        case 'ArrowUp':
          event.preventDefault();
          setSelectedIndex((prev) => {
            if (browseState.directories.length === 0) return -1;
            if (prev <= 0) {
              return browseState.directories.length - 1;
            }
            return prev - 1;
          });
          break;

        case 'Enter':
          event.preventDefault();
          if (selectedIndex >= 0 && selectedIndex < browseState.directories.length) {
            handleDirectoryNavigate(selectedIndex);
          }
          break;
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, selectedIndex, browseState.directories]);

  // Reset selected index when directories change
  useEffect(() => {
    if (selectedIndex >= browseState.directories.length) {
      setSelectedIndex(-1);
    }
  }, [selectedIndex, browseState.directories.length]);

  // Auto-dismiss error after 3 seconds
  useEffect(() => {
    if (switchingState.error) {
      const timer = setTimeout(() => {
        setSwitchingState((prev) => ({ ...prev, error: null }));
      }, 3000);
      return () => clearTimeout(timer);
    }
  }, [switchingState.error]);

  // Auto-focus path input when popover opens
  useEffect(() => {
    if (isOpen && pathInputRef.current) {
      setTimeout(() => {
        pathInputRef.current?.focus();
      }, 50);
    }
  }, [isOpen]);

  // Get trigger width for popover
  const triggerWidth = useMemo(() => {
    if (triggerRef.current) {
      return `${triggerRef.current.offsetWidth}px`;
    }
    return undefined;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Get breadcrumb segments from current path
  const breadcrumbSegments = useMemo(() => {
    if (!browseState.currentPath) return [];
    const parts = browseState.currentPath.split('/').filter(Boolean);
    // Build cumulative paths for each segment
    const segments: Array<{ label: string; path: string }> = [];
    let currentPath = '';
    for (let i = 0; i < parts.length; i++) {
      currentPath += '/' + parts[i];
      segments.push({
        label: parts[i],
        path: currentPath,
      });
    }
    // Truncate to last 4 segments if too deep
    if (segments.length > 4) {
      const first = segments[0];
      return [
        first,
        { label: '...', path: first.path },
        ...segments.slice(-3),
      ];
    }
    return segments;
  }, [browseState.currentPath]);

  // Navigate to a specific path
  const navigateToPath = useCallback((path: string) => {
    setBrowseState(prev => ({
      ...prev,
      currentPath: path,
      error: null,
    }));
    setSelectedIndex(-1);
  }, []);

  // Navigate into a directory
  const handleDirectoryNavigate = useCallback(
    (index: number) => {
      const dir = browseState.directories[index];
      if (dir) {
        navigateToPath(dir.path);
      }
    },
    [browseState.directories, navigateToPath]
  );

  // Handle path input change
  const handlePathInputChange = useCallback(async () => {
    const input = pathInputRef.current;
    if (!input || !input.value.trim()) return;

    let path = input.value.trim();

    // Handle ~ prefix (client-side expansion)
    if (path.startsWith('~/')) {
      try {
        const homeDir = await apiService.current.getWorkspace();
        if (homeDir.daemon_root) {
          path = homeDir.daemon_root + path.substring(1);
        }
      } catch {
        // Just use ~/ as-is, API will handle it
      }
    }

    // Normalize path
    path = path.replace(/\/+/g, '/');
    if (!path.startsWith('/')) {
      path = '/' + path;
    }

    // Check if path exists and is a directory
    try {
      const response = await fetch(
        `/api/workspace/browse?path=${encodeURIComponent(path)}`
      );
      if (response.ok) {
        navigateToPath(path);
      } else {
        // Path doesn't exist, try to switch to it anyway
        navigateToPath(path);
      }
    } catch {
      // Navigate anyway, API will handle validation
      navigateToPath(path);
    }
  }, [navigateToPath]);

  // Handle path input blur - resolve and update display
  const handlePathInputBlur = useCallback(() => {
    const input = pathInputRef.current;
    if (!input || !input.value.trim()) return;

    let path = input.value.trim();
    if (path.startsWith('~/')) {
      // Keep ~/ for now, API will resolve
    }
    path = path.replace(/\/+/g, '/');
    if (!path.startsWith('/')) {
      path = '/' + path;
    }

    // Update input value to normalized path
    input.value = path;
  }, []);

  // Switch to current browse path as workspace
  const handleSwitchToBrowsePath = useCallback(async () => {
    if (browseState.currentPath === workspaceRoot) {
      // Already at this workspace, nothing to do
      return;
    }

    setSwitchingState({ isSwitching: true, error: null });

    try {
      // Check for active terminal sessions before switching
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
        // If we can't check session count, proceed with switch
      }

      // Switch workspace
      const response = await apiService.current.setWorkspace(browseState.currentPath);
      setWorkspaceRoot(response.workspace_root || browseState.currentPath);

      // Brief switching state, then reload
      setTimeout(() => {
        window.location.reload();
      }, 300);
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : 'Failed to switch to this folder';

      // Check for query in progress error
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
    }
  }, [browseState.currentPath, workspaceRoot]);

  // Handle refresh
  const handleRefresh = useCallback(async () => {
    if (!isConnected) return;

    setIsLoading(true);
    try {
      // Re-fetch workspace info
      const workspace = await apiService.current.getWorkspace();
      setWorkspaceRoot(workspace.workspace_root || '');
      setBrowseState(prev => ({
        ...prev,
        currentPath: workspace.workspace_root || '',
        error: null,
      }));

      // Re-fetch workspace directories
      if (workspace.daemon_root) {
        const response = await fetch(
          `/api/workspace/browse?path=${encodeURIComponent(workspace.daemon_root)}`
        );
        if (!response.ok) {
          throw new Error('Failed to fetch workspace directories');
        }
        const data = await response.json();
        const directories: WorkspaceDirectory[] = (data.files || [])
          .filter(
            (file: any) =>
              file.type === 'directory' &&
              !file.name.startsWith('.')
          )
          .map((file: any) => ({
            name: file.name,
            path: file.path,
          }));
        setBrowseState(prev => ({
          ...prev,
          directories,
        }));
      }
    } catch (error) {
      console.error('Failed to refresh workspace data:', error);
      setSwitchingState({
        isSwitching: false,
        error: 'Failed to refresh workspace data',
      });
    } finally {
      setIsLoading(false);
    }
  }, [isConnected]);

  const togglePopover = useCallback(() => {
    setIsOpen((prev) => !prev);
    if (!isOpen) {
      setSelectedIndex(-1);
    }
  }, [isOpen]);

  // Get truncated workspace name (last 2 path segments)
  const getTruncatedWorkspaceName = useCallback(
    (path: string): string => {
      if (!path) return 'No workspace';
      const segments = path.split('/').filter(Boolean);
      if (segments.length <= 2) {
        return segments.join('/') || 'No workspace';
      }
      return segments.slice(-2).join('/');
    },
    []
  );

  const truncatedWorkspaceName = getTruncatedWorkspaceName(workspaceRoot);

  // Determine if we should show text (not collapsed)
  const showText = !sidebarCollapsed;

  // Check if browse path equals workspace root
  const isBrowsePathActive = browseState.currentPath === workspaceRoot;

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
        <FolderOpen
          size={16}
          className="location-switcher-trigger-icon"
        />
        {showText && (
          <>
            <span className="location-switcher-trigger-text">
              {truncatedWorkspaceName}
            </span>
            {selectedInstancePID && (
              <span className="location-switcher-trigger-pid">
                pid:{selectedInstancePID}
              </span>
            )}
          </>
        )}
        {switchingState.isSwitching ? (
          <Loader2 size={12} className="spin" />
        ) : (
          <span className="location-switcher-trigger-chevron" />
        )}
      </button>

      {isOpen && (
        <div
          ref={popoverRef}
          className="location-switcher-popover"
          style={{
            ['--trigger-width' as any]: triggerWidth,
          }}
          role="listbox"
          aria-label="Location switcher"
          aria-activedescendant={selectedIndex >= 0 ? `location-item-${selectedIndex}` : undefined}
          tabIndex={0}
        >
          {/* Error Bar */}
          {switchingState.error && (
            <div id="location-switcher-error" className="location-switcher-error" role="alert">
              {switchingState.error}
            </div>
          )}

          {/* Content */}
          <div className="location-switcher-content">
            {/* Workspaces Section */}
            <div
              className="location-switcher-section-header"
              role="presentation"
            >
              <FolderOpen size={12} className="location-switcher-section-icon" />
              Workspaces
            </div>

            {/* Path Input Bar */}
            <div className="location-switcher-path-input-container">
              <input
                ref={pathInputRef}
                type="text"
                className="location-switcher-path-input"
                value={browseState.currentPath}
                onChange={(e) =>
                  setBrowseState(prev => ({ ...prev, currentPath: e.target.value }))
                }
                onBlur={handlePathInputBlur}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handlePathInputChange();
                  }
                }}
                placeholder="Open path..."
                disabled={!isConnected}
                title="Type or paste a path, then press Enter"
              />
              <button
                type="button"
                className="location-switcher-path-input-refresh"
                onClick={() => {
                  setBrowseState(prev => ({ ...prev, currentPath: workspaceRoot }));
                }}
                disabled={!isConnected || browseState.currentPath === workspaceRoot}
                title="Reset to current workspace"
              >
                <RefreshCw size={12} className={isLoading ? 'spin' : ''} />
              </button>
            </div>

            {/* Breadcrumb Trail */}
            <nav
              className="location-switcher-breadcrumb"
              aria-label="Breadcrumb"
            >
              {breadcrumbSegments.map((segment, index) => (
                <React.Fragment key={`breadcrumb-${index}`}>
                  <button
                    type="button"
                    className={`location-switcher-breadcrumb-item ${
                      index === breadcrumbSegments.length - 1 ? 'current' : ''
                    }`}
                    onClick={() => navigateToPath(segment.path)}
                    disabled={index === breadcrumbSegments.length - 1}
                    title={`Navigate to ${segment.path}`}
                  >
                    {segment.label === '...' ? (
                      <span className="location-switcher-breadcrumb-ellipsis">...</span>
                    ) : (
                      <span className="location-switcher-breadcrumb-label">{segment.label}</span>
                    )}
                  </button>
                  {index < breadcrumbSegments.length - 1 && (
                    <span className="location-switcher-breadcrumb-separator" aria-hidden="true">
                      /
                    </span>
                  )}
                </React.Fragment>
              ))}
            </nav>

            {/* Directory Loading/Error State */}
            {browseState.isLoading && (
              <div className="location-switcher-directory-loading">
                <Loader2 size={16} className="spin" />
                <span>Loading directory...</span>
              </div>
            )}

            {browseState.error && (
              <div className="location-switcher-directory-error">
                <span>{browseState.error}</span>
              </div>
            )}

            {/* Directory Listing */}
            {!browseState.isLoading && !browseState.error && (
              <div className="location-switcher-directory-list">
                {browseState.directories.length === 0 ? (
                  <div
                    className="location-switcher-item"
                    style={{ color: 'var(--text-tertiary)' }}
                    role="option"
                    aria-selected={false}
                  >
                    <span className="location-switcher-item-text">
                      No subdirectories
                    </span>
                  </div>
                ) : (
                  browseState.directories.map((dir, index) => (
                    <button
                      key={`dir-${index}`}
                      type="button"
                      className={`location-switcher-item location-switcher-dir-item ${
                        dir.path === workspaceRoot ? 'active' : ''
                      } ${index === selectedIndex ? 'selected' : ''}`}
                      onClick={() => handleDirectoryNavigate(index)}
                      role="option"
                      aria-selected={dir.path === workspaceRoot}
                      aria-label={`Navigate to ${dir.name}`}
                    >
                      <span className="location-switcher-item-text">{dir.name}</span>
                      {dir.path === workspaceRoot && (
                        <span className="location-switcher-item-indicator">●</span>
                      )}
                    </button>
                  ))
                )}
              </div>
            )}

            {/* Switch Button */}
            <button
              type="button"
              className={`location-switcher-switch-btn ${
                isBrowsePathActive ? 'disabled' : ''
              }`}
              onClick={handleSwitchToBrowsePath}
              disabled={isBrowsePathActive || switchingState.isSwitching || !isConnected}
              title={
                isBrowsePathActive
                  ? 'Already selected'
                  : 'Switch workspace to this folder'
              }
            >
              {isBrowsePathActive ? (
                <>
                  <span className="location-switcher-switch-btn-icon">✓</span>
                  <span>Current workspace</span>
                </>
              ) : (
                <>
                  <span className="location-switcher-switch-btn-icon">→</span>
                  <span>Switch to this folder</span>
                </>
              )}
            </button>

            <div className="location-switcher-divider" role="separator" />

            {/* Instances Section */}
            <div
              className="location-switcher-section-header"
              role="presentation"
            >
              <Monitor size={12} className="location-switcher-section-icon" />
              Instances
            </div>

            {instances.length === 0 ? (
              <div
                className="location-switcher-item"
                style={{ color: 'var(--text-tertiary)' }}
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">
                  No instances available
                </span>
              </div>
            ) : (
              instances.map((instance, index) => {
                const offset = browseState.directories.length;
                const name = instance.working_dir
                  .split('/')
                  .filter(Boolean)
                  .slice(-2)
                  .join('/');
                const label = `${name} · pid:${instance.pid}`;

                return (
                  <button
                    key={`instance-${instance.id}`}
                    id={`location-item-${offset + index}`}
                    type="button"
                    className={`location-switcher-item ${
                      instance.pid === selectedInstancePID ? 'active' : ''
                    } ${offset + index === selectedIndex ? 'selected' : ''}`}
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
                      !onInstanceChange ||
                      instance.is_host
                    }
                  >
                    <span className="location-switcher-item-text">{label}</span>
                    {instance.pid === selectedInstancePID && (
                      <span className="location-switcher-item-indicator">●</span>
                    )}
                  </button>
                );
              })
            )}
          </div>

          {/* Footer */}
          <div className="location-switcher-footer">
            <button
              type="button"
              className="location-switcher-footer-refresh"
              onClick={handleRefresh}
              disabled={isLoading || !isConnected}
              title="Refresh workspace list"
            >
              <RefreshCw
                size={14}
                className={isLoading ? 'spin' : ''}
              />
              Refresh
            </button>
            <span className="location-switcher-footer-esc">Esc</span>
          </div>
        </div>
      )}
    </div>
  );
};

export default LocationSwitcher;
