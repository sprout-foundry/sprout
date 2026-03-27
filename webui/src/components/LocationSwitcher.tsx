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

const LocationSwitcher: React.FC<LocationSwitcherProps> = ({
  isConnected,
  instances = [],
  selectedInstancePID = 0,
  isSwitchingInstance = false,
  onInstanceChange,
  sidebarCollapsed = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [daemonRoot, setDaemonRoot] = useState('');
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [workspaceDirs, setWorkspaceDirs] = useState<WorkspaceDirectory[]>([]);
  const [switchingState, setSwitchingState] = useState<SwitchingState>({
    isSwitching: false,
    error: null,
  });
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [isLoading, setIsLoading] = useState(false);

  const popoverRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const apiService = useRef(ApiService.getInstance());

  // Load workspace info and directories on mount or when connected
  useEffect(() => {
    if (!isConnected) {
      setDaemonRoot('');
      setWorkspaceRoot('');
      setWorkspaceDirs([]);
      return;
    }

    let cancelled = false;

    const loadData = async () => {
      try {
        // Get workspace info
        const workspace = await apiService.current.getWorkspace();
        if (!cancelled) {
          setDaemonRoot(workspace.daemon_root || '');
          setWorkspaceRoot(workspace.workspace_root || '');
        }

        // Get workspace directories from daemon root
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
          if (!cancelled) {
            setWorkspaceDirs(directories);
          }
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

  // Keyboard navigation
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      switch (event.key) {
        case 'Escape':
          event.preventDefault();
          setIsOpen(false);
          setSelectedIndex(-1);
          break;

        case 'ArrowDown':
          event.preventDefault();
          setSelectedIndex((prev) => {
            const totalItems = workspaceDirs.length + instances.length;
            if (totalItems === 0) return -1;
            return prev < totalItems - 1 ? prev + 1 : 0;
          });
          break;

        case 'ArrowUp':
          event.preventDefault();
          setSelectedIndex((prev) => {
            if (prev <= 0) {
              const totalItems = workspaceDirs.length + instances.length;
              return totalItems > 0 ? totalItems - 1 : -1;
            }
            return prev - 1;
          });
          break;

        case 'Enter':
          event.preventDefault();
          if (selectedIndex >= 0) {
            handleItemSelect(selectedIndex);
          }
          break;
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, selectedIndex, workspaceDirs, instances]);

  // Reset selected index when lists change (prevents stale highlighting)
  useEffect(() => {
    if (selectedIndex >= workspaceDirs.length + instances.length) {
      setSelectedIndex(-1);
    }
  }, [selectedIndex, workspaceDirs.length, instances.length]);

  // Auto-dismiss error after 3 seconds
  useEffect(() => {
    if (switchingState.error) {
      const timer = setTimeout(() => {
        setSwitchingState((prev) => ({ ...prev, error: null }));
      }, 3000);
      return () => clearTimeout(timer);
    }
  }, [switchingState.error]);

  // Get trigger width for popover
  const triggerWidth = useMemo(() => {
    if (triggerRef.current) {
      return `${triggerRef.current.offsetWidth}px`;
    }
    return undefined;
  }, [isOpen]);

  // Build combined items list for keyboard navigation
  const allItems = useMemo(() => {
    const items: Array<{
      type: 'workspace' | 'instance';
      name: string;
      path: string;
      pid?: number;
      isActive: boolean;
    }> = [];

    // Add workspace directories
    workspaceDirs.forEach((dir) => {
      items.push({
        type: 'workspace',
        name: dir.name,
        path: dir.path,
        isActive: dir.path === workspaceRoot,
      });
    });

    // Add instances
    instances.forEach((instance) => {
      const name = instance.working_dir
        .split('/')
        .filter(Boolean)
        .slice(-2)
        .join('/');
      items.push({
        type: 'instance',
        name: `${name} · pid:${instance.pid}`,
        path: instance.working_dir,
        pid: instance.pid,
        isActive: instance.pid === selectedInstancePID,
      });
    });

    return items;
  }, [workspaceDirs, instances, workspaceRoot, selectedInstancePID]);

  const handleItemSelect = useCallback(
    async (index: number) => {
      const item = allItems[index];
      if (!item) return;

      setSwitchingState({ isSwitching: true, error: null });

      try {
        if (item.type === 'workspace') {
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
          const response = await apiService.current.setWorkspace(item.path);
          setWorkspaceRoot(response.workspace_root || workspaceRoot);

          // Brief switching state, then reload
          setTimeout(() => {
            window.location.reload();
          }, 300);
        } else if (item.type === 'instance' && onInstanceChange && item.pid) {
          // Switch instance
          onInstanceChange(item.pid);
          // Note: onInstanceChange typically triggers a full page reload
        }
      } catch (error) {
        const errorMessage =
          error instanceof Error
            ? error.message
            : 'Failed to switch location';

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
    },
    [allItems, onInstanceChange, workspaceRoot]
  );

  const handleRefresh = useCallback(async () => {
    if (!isConnected) return;

    setIsLoading(true);
    try {
      // Re-fetch workspace info
      const workspace = await apiService.current.getWorkspace();
      setDaemonRoot(workspace.daemon_root || '');
      setWorkspaceRoot(workspace.workspace_root || '');

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
        setWorkspaceDirs(directories);
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

            {workspaceDirs.length === 0 ? (
              <div
                className="location-switcher-item"
                style={{ color: 'var(--text-tertiary)' }}
                role="option"
                aria-selected={false}
              >
                <span className="location-switcher-item-text">
                  No workspaces available
                </span>
              </div>
            ) : (
              workspaceDirs.map((dir, index) => (
                <button
                  key={`workspace-${index}`}
                  id={`location-item-${index}`}
                  type="button"
                  className={`location-switcher-item ${
                    dir.path === workspaceRoot ? 'active' : ''
                  } ${index === selectedIndex ? 'selected' : ''}`}
                  onClick={() => handleItemSelect(index)}
                  role="option"
                  aria-selected={dir.path === workspaceRoot}
                  aria-label={`Switch to workspace ${dir.name}`}
                >
                  <span className="location-switcher-item-text">{dir.name}</span>
                  {dir.path === workspaceRoot && (
                    <span className="location-switcher-item-indicator">●</span>
                  )}
                </button>
              ))
            )}

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
                const offset = workspaceDirs.length;
                const itemIndex = offset + index;
                const name = instance.working_dir
                  .split('/')
                  .filter(Boolean)
                  .slice(-2)
                  .join('/');
                const label = `${name} · pid:${instance.pid}`;

                return (
                  <button
                    key={`instance-${instance.id}`}
                    id={`location-item-${itemIndex}`}
                    type="button"
                    className={`location-switcher-item ${
                      instance.pid === selectedInstancePID ? 'active' : ''
                    } ${itemIndex === selectedIndex ? 'selected' : ''}`}
                    onClick={() => handleItemSelect(itemIndex)}
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
