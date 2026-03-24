import React, { useEffect, useState, useMemo } from 'react';
import './Sidebar.css';
import { ApiService, ProviderOption, LeditInstance, LeditSettings } from '../services/api';
import SettingsPanel from './SettingsPanel';
import { viewRegistry, ProviderContext, SidebarSection, ProviderLogEntry } from '../providers';
import { useTheme } from '../contexts/ThemeContext';
import { HotkeyPreset, useHotkeys } from '../contexts/HotkeyContext';
import {
  LayoutList,
  ScrollText,
  FolderCog,
  Settings,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Info,
  Dot,
  ChevronLeft,
  ChevronRight,
  X,
  type LucideIcon,
} from 'lucide-react';
import FileTree from './FileTree';

type SectionTab = 'views' | 'logs' | 'files' | 'settings';

interface SidebarProps {
  isConnected: boolean;
  selectedModel?: string;
  onModelChange?: (model: string) => void;
  availableModels?: string[];
  currentView?: 'chat' | 'editor' | 'git' | 'logs';
  onViewChange?: (view: 'chat' | 'editor' | 'git' | 'logs') => void;
  stats?: {
    queryCount: number;
    filesModified: number;
  };
  recentFiles?: Array<{ path: string; modified: boolean }>;
  recentLogs?: string[] | Array<{ id: string; type: string; timestamp: Date; data: any; level: string; category: string }>;
  isMobileMenuOpen?: boolean;
  onMobileMenuToggle?: () => void;
  sidebarCollapsed?: boolean;
  onSidebarToggle?: () => void;
  onFileClick?: (filePath: string) => void;
  // Legacy props for backward compatibility
  provider?: string;
  model?: string;
  queryCount?: number;
  logs?: string[];
  files?: Array<{ path: string; modified: boolean }>;
  onProviderChange?: (provider: string) => void;
  isOpen?: boolean;
  onClose?: () => void;
  isMobile?: boolean;
}

interface SectionData {
  section: SidebarSection;
  data: any;
  loading: boolean;
  error: string | null;
}

/** Section tab definitions */
const SECTION_TABS: { id: SectionTab; icon: LucideIcon; label: string }[] = [
  { id: 'views', icon: LayoutList, label: 'Views' },
  { id: 'logs', icon: ScrollText, label: 'Logs' },
  { id: 'files', icon: FolderCog, label: 'Files' },
  { id: 'settings', icon: Settings, label: 'Settings' },
];

const Sidebar: React.FC<SidebarProps> = ({
  isConnected,
  selectedModel,
  onModelChange,
  availableModels,
  currentView = 'chat',
  onViewChange,
  stats,
  recentFiles = [],
  recentLogs = [],
  isMobileMenuOpen,
  onMobileMenuToggle,
  sidebarCollapsed,
  onSidebarToggle,
  onFileClick,
  provider,
  model,
  queryCount,
  logs,
  files,
  onProviderChange,
  isOpen = true,
  onClose,
  isMobile = false
}) => {
  const { themePack, availableThemePacks, setThemePack } = useTheme();
  const { preset: hotkeyPreset, setPreset: setHotkeyPreset } = useHotkeys();
  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [instances, setInstances] = useState<LeditInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);
  const [instanceSwitchError, setInstanceSwitchError] = useState<string | null>(null);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const [sectionsData, setSectionsData] = useState<Map<string, SectionData>>(new Map());
  const [refreshTrigger, setRefreshTrigger] = useState(0);
  const [selectedSection, setSelectedSection] = useState<SectionTab>('views');
  const [settings, setSettings] = useState<LeditSettings | null>(null);
  const apiService = ApiService.getInstance();

  // Load settings on mount / connection
  useEffect(() => {
    if (!isConnected) return;
    let cancelled = false;
    apiService.getSettings().then((s) => {
      if (!cancelled) setSettings(s);
    }).catch(() => { /* silent */ });
    return () => { cancelled = true; };
  }, [isConnected]);

  const finalSelectedModel = selectedModel || selectedModelState;
  // Compute available models from providers and selectedProvider
  const availableModelsState = useMemo(() => {
    const providerData = providers.find((p) => p.id === selectedProvider);
    return providerData?.models || [];
  }, [providers, selectedProvider]);
  const finalAvailableModels = availableModels && availableModels.length > 1
    ? availableModels
    : availableModelsState;

  // Memoize computed values to prevent unnecessary re-renders
  const finalStats = useMemo(() =>
    stats || { queryCount: queryCount || 0, filesModified: files?.filter(f => f.modified).length || 0 },
    [stats, queryCount, files]
  );

  const finalRecentLogs = useMemo(() =>
    recentLogs.length > 0 ? recentLogs : (logs || []),
    [recentLogs, logs]
  );
  const normalizedRecentLogs = useMemo<ProviderLogEntry[]>(
    () => (finalRecentLogs as Array<string | ProviderLogEntry>)
      .filter((log): log is ProviderLogEntry => typeof log !== 'string'),
    [finalRecentLogs]
  );

  const finalIsMobileMenuOpen = isMobileMenuOpen !== undefined ? isMobileMenuOpen : isOpen;
  const finalOnMobileMenuToggle = onMobileMenuToggle || onClose;

  // Update provider context in registry
  useEffect(() => {
    const context: ProviderContext = {
      isConnected,
      currentView,
      onFileClick,
      onModelChange,
      recentFiles: recentFiles.length > 0 ? recentFiles : (files || []),
      recentLogs: normalizedRecentLogs,
      stats: finalStats
    };

    viewRegistry.setContext(context);
  }, [isConnected, currentView, onFileClick, onModelChange, recentFiles, files, normalizedRecentLogs, finalStats]);

  // Subscribe to provider updates for current view
  useEffect(() => {
    const provider = viewRegistry.getProvider(currentView);
    if (!provider || !provider.subscribe) {
      return;
    }

    // Subscribe to provider state changes
    const unsubscribe = provider.subscribe(() => {
      // Trigger a re-fetch of sections by incrementing refresh trigger
      setRefreshTrigger(prev => prev + 1);
    });

    // Cleanup subscription on unmount or view change
    return () => {
      if (unsubscribe) {
        unsubscribe();
      }
    };
  }, [currentView]);

  // Fetch and render sections for current view
  useEffect(() => {
    const sections = viewRegistry.getSections(currentView);
    let cancelled = false;
    const validSectionIds = new Set(sections.map(section => section.id));

    setSectionsData(prev => {
      const next = new Map<string, SectionData>();
      validSectionIds.forEach((id) => {
        const existing = prev.get(id);
        if (existing) {
          next.set(id, existing);
        }
      });
      return next;
    });

    // Load data for each section
    sections.forEach(async (section) => {
      if (cancelled) return;
      setSectionsData(prev => {
        const updated = new Map(prev);
        updated.set(section.id, { section, data: null, loading: true, error: null });
        return updated;
      });

      try {
        let data: any;

        // Fetch data based on data source type
        switch (section.dataSource.type) {
          case 'state':
            // Transform context data
            const context = viewRegistry.getContext();
            if (!context) {
              console.warn('Provider context not set, skipping section', section.id);
              return;
            }
            data = section.dataSource.transform?.(context) || null;
            break;

          case 'api':
            // Fetch from API endpoint
            if (section.dataSource.endpoint) {
              const response = await fetch(section.dataSource.endpoint);
              if (!response.ok) {
                throw new Error(`API ${response.status}: ${response.statusText}`);
              }
              data = await response.json();
              data = section.dataSource.transform?.(data) || data;
            }
            break;

          case 'websocket':
            // Data will come from websocket events
            // For now, use state data
            const wsContext = viewRegistry.getContext();
            if (!wsContext) {
              console.warn('Provider context not set, skipping section', section.id);
              return;
            }
            data = section.dataSource.transform?.(wsContext) || null;
            break;
        }

        if (cancelled) return;
        setSectionsData(prev => {
          const updated = new Map(prev);
          updated.set(section.id, { section, data, loading: false, error: null });
          return updated;
        });
      } catch (error) {
        console.error(`Failed to load data for section ${section.id}:`, error);
        if (cancelled) return;
        setSectionsData(prev => {
          const updated = new Map(prev);
          updated.set(section.id, { section, data: null, loading: false, error: 'Failed to load' });
          return updated;
        });
      }
    });

    return () => {
      cancelled = true;
    };
  }, [currentView, isConnected, refreshTrigger]);

  useEffect(() => {
    const fetchProviders = async () => {
      setIsLoadingProviders(true);
      try {
        const data = await apiService.getProviders();
        if (data.providers && data.providers.length > 0) {
          setProviders(data.providers);
          if (data.current_provider) {
            setSelectedProvider(data.current_provider);
          }
          if (data.current_model) {
            setSelectedModelState(data.current_model);
          }
        }
      } catch (error) {
        console.error('Failed to fetch providers:', error);
      } finally {
        setIsLoadingProviders(false);
      }
    };

    fetchProviders();
  }, [apiService, isConnected]);

  useEffect(() => {
    if (!isConnected) {
      return;
    }

    let cancelled = false;
    let timer: NodeJS.Timeout | null = null;

    const loadInstances = async () => {
      try {
        const data = await apiService.getInstances();
        if (cancelled) {
          return;
        }
        setInstances(data.instances || []);
        if (data.desired_host_pid && data.desired_host_pid > 0) {
          setSelectedInstancePID(data.desired_host_pid);
        } else if (data.active_host_pid && data.active_host_pid > 0) {
          setSelectedInstancePID(data.active_host_pid);
        }
      } catch (error) {
        if (!cancelled) {
          console.error('Failed to fetch instances:', error);
        }
      }
      if (!cancelled) {
        timer = setTimeout(loadInstances, 2000);
      }
    };

    loadInstances();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [apiService, isConnected]);

  useEffect(() => {
    if (!isSwitchingInstance || selectedInstancePID <= 0) {
      return;
    }
    const selected = instances.find((instance) => instance.pid === selectedInstancePID);
    if (selected?.is_host) {
      setIsSwitchingInstance(false);
    }
  }, [instances, isSwitchingInstance, selectedInstancePID]);

  useEffect(() => {
    if (!provider || provider === 'unknown') {
      return;
    }
    setSelectedProvider(provider);
  }, [provider, providers]);

  useEffect(() => {
    if (!model || model === 'unknown') {
      return;
    }
    setSelectedModelState(model);
  }, [model, providers, selectedProvider]);

  useEffect(() => {
    if (!selectedProvider) {
      if (providers.length > 0) {
        setSelectedProvider(providers[0].id);
      }
      return;
    }

    const providerExists = providers.some((item) => item.id === selectedProvider);
    if (!providerExists && providers.length > 0) {
      setSelectedProvider(providers[0].id);
    }
  }, [providers, selectedProvider]);

  useEffect(() => {
    if (!selectedProvider) {
      return;
    }

    const providerData = providers.find((item) => item.id === selectedProvider);
    if (!providerData || providerData.models.length === 0) {
      return;
    }

    if (!providerData.models.includes(finalSelectedModel)) {
      setSelectedModelState(providerData.models[0]);
    }
  }, [providers, selectedProvider, finalSelectedModel]);

  const handleProviderChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newProvider = e.target.value;
    setSelectedProvider(newProvider);
    onProviderChange?.(newProvider);
  };

  const handleModelChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newModel = e.target.value;
    // Only update if the model actually changed
    if (newModel !== finalSelectedModel) {
      setSelectedModelState(newModel);
      onModelChange?.(newModel);
    }
  };

  const handleHotkeyPresetChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    setHotkeyPreset(e.target.value as HotkeyPreset);
  };

  const handleInstanceChange = async (e: React.ChangeEvent<HTMLSelectElement>) => {
    const pid = Number(e.target.value);
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setInstanceSwitchError(null);
    setIsSwitchingInstance(true);
    try {
      await apiService.selectInstance(pid);
      setSelectedInstancePID(pid);
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setInstanceSwitchError('Failed to switch instance');
      setIsSwitchingInstance(false);
    }
  };

  const handleSectionTabClick = (tab: SectionTab) => {
    if (sidebarCollapsed) {
      // If collapsed, expand sidebar and switch to the section
      setSelectedSection(tab);
      onSidebarToggle?.();
    } else {
      setSelectedSection(tab);
    }
  };

  // ─── Section Renderers ───────────────────────────────────────────────

  /** Views section: primary app mode switcher + view-specific provider content */
  const renderViewsSection = () => {
    const viewButtons: Array<{ id: 'chat' | 'editor' | 'git' | 'logs'; label: string }> = [
      { id: 'chat', label: 'Chat' },
      { id: 'editor', label: 'Editor' },
      { id: 'git', label: 'Git' },
      { id: 'logs', label: 'Logs' }
    ];

    if (sectionsData.size === 0) {
      return (
        <>
          <div className="section">
            <h4>Views</h4>
            <div className="view-switcher-grid">
              {viewButtons.map((view) => (
                <button
                  key={view.id}
                  type="button"
                  className={`view-switch-btn ${currentView === view.id ? 'active' : ''}`}
                  onClick={() => onViewChange?.(view.id)}
                >
                  {view.label}
                </button>
              ))}
            </div>
          </div>
          <div className="empty">No content available</div>
        </>
      );
    }

    // Sort sections by order
    const sortedSections = Array.from(sectionsData.values()).sort((a, b) =>
      (a.section.order || 0) - (b.section.order || 0)
    );

    const renderedContext = sortedSections.map(({ section, data, loading, error }) => {
      if (loading) {
        return (
          <div key={section.id} className="section">
            <h4>{section.title?.(data) || section.id}</h4>
            <div className="empty">Loading...</div>
          </div>
        );
      }

      if (error) {
        return (
          <div key={section.id} className="section">
            <h4>{section.title?.(data) || section.id}</h4>
            <div className="empty" style={{ color: 'var(--error)' }}>{error}</div>
          </div>
        );
      }

      const context = viewRegistry.getContext();

      if (!context) {
        return (
          <div key={section.id} className="section">
            <h4>{section.title?.(data) || section.id}</h4>
            <div className="empty">Loading context...</div>
          </div>
        );
      }

      return (
        <div key={section.id} className="section">
          <h4>{section.title?.(data) || section.id}</h4>
          {section.renderItem(data, context)}
        </div>
      );
    });

    const shouldRenderViewContext = currentView !== 'git';

    return (
      <>
        <div className="section">
          <h4>Views</h4>
          <div className="view-switcher-grid">
            {viewButtons.map((view) => (
              <button
                key={view.id}
                type="button"
                className={`view-switch-btn ${currentView === view.id ? 'active' : ''}`}
                onClick={() => onViewChange?.(view.id)}
              >
                {view.label}
              </button>
            ))}
          </div>
        </div>
        {shouldRenderViewContext ? renderedContext : null}
      </>
    );
  };

  /** Logs section: full event/log stream */
  const renderLogsSection = () => {
    if (normalizedRecentLogs.length === 0) {
      return <div className="empty">No logs yet</div>;
    }

    const getLogIcon = (level: string) => {
      switch (level) {
        case 'success': return <CheckCircle2 size={12} />;
        case 'error': return <XCircle size={12} />;
        case 'warning': return <AlertTriangle size={12} />;
        case 'info': return <Info size={12} />;
        default: return <Dot size={12} />;
      }
    };

    const extractFilePath = (data: any): string | null => {
      let payload = data;
      if (typeof payload === 'string') {
        try { payload = JSON.parse(payload); } catch { payload = {}; }
      }
      if (!payload || typeof payload !== 'object') return null;
      const candidates = [
        payload.path, payload.file_path, payload.filePath,
        payload.target_path, payload.targetPath,
        payload.file?.path, payload.file?.name, payload.name
      ];
      for (const value of candidates) {
        if (typeof value === 'string' && value.trim() !== '') return value;
      }
      return null;
    };

    const getLogSummary = (logEntry: any) => {
      try {
        switch (logEntry.type) {
          case 'query_started':
            return `Query: ${logEntry.data?.query?.substring(0, 50) || 'No query'}...`;
          case 'tool_execution':
            return `${logEntry.data?.tool || 'Unknown'}: ${logEntry.data?.status || 'Unknown'}`;
          case 'file_changed': {
            const filePath = extractFilePath(logEntry.data);
            if (!filePath) return 'File changed';
            return `File: ${filePath.split('/').filter(Boolean).pop() || filePath}`;
          }
          case 'stream_chunk':
            return `Stream: ${logEntry.data?.chunk?.substring(0, 50) || 'No chunk'}...`;
          case 'error':
            return `Error: ${logEntry.data?.message?.substring(0, 50) || 'Unknown error'}...`;
          case 'connection_status':
            return logEntry.data?.connected ? 'Connected' : 'Disconnected';
          default:
            return `${logEntry.type}`;
        }
      } catch {
        return `${logEntry.type}`;
      }
    };

    const displayLogs = [...normalizedRecentLogs].reverse();

    return (
      <div className="logs-list logs-expanded">
        {displayLogs.map((logEntry, index) => (
          <div key={logEntry.id || index} className="log-item">
            <span className="log-icon">{getLogIcon(logEntry.level)}</span>
            <span className="log-text">
              <strong>{logEntry.type}</strong>
              <span className="log-meta">
                {new Date(logEntry.timestamp).toLocaleTimeString()}
              </span>
              <span>{getLogSummary(logEntry)}</span>
            </span>
          </div>
        ))}
      </div>
    );
  };

  /** Files section: unified file tree across all views */
  const renderFilesSection = () => {
    return (
      <FileTree
        rootPath="."
        onFileSelect={(file) => onFileClick?.(file.path)}
      />
    );
  };

  /** Settings section: appearance + agent config */
  const renderSettingsSection = () => {
    return (
      <>
        <div className="section">
          <h4>Appearance</h4>
          <div className="config-item">
            <label htmlFor="theme-select">Theme Pack:</label>
            <select
              id="theme-select"
              value={themePack.id}
              onChange={(e) => setThemePack(e.target.value)}
              className="styled-select"
            >
              {availableThemePacks.map((pack) => (
                <option key={pack.id} value={pack.id}>
                  {pack.name}
                </option>
              ))}
            </select>
          </div>
          <div className="config-item">
            <label htmlFor="hotkey-preset-select">Editor Hotkeys:</label>
            <select
              id="hotkey-preset-select"
              value={hotkeyPreset}
              onChange={handleHotkeyPresetChange}
              className="styled-select"
            >
              <option value="vscode">VS Code</option>
              <option value="webstorm">WebStorm</option>
              <option value="ledit">Ledit (Legacy)</option>
            </select>
          </div>
        </div>

        <div className="section">
          <h4>Agent Config</h4>
          <div className="config-item">
            <label htmlFor="provider-select">Provider:</label>
            <select
              id="provider-select"
              value={selectedProvider}
              onChange={handleProviderChange}
              disabled={!isConnected || isLoadingProviders}
              className="styled-select"
            >
              {providers.length === 0 && (
                <option value="">
                  {isLoadingProviders ? 'Loading providers...' : 'No providers available'}
                </option>
              )}
              {providers.map(p => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>
          <div className="config-item">
            <label htmlFor="model-select">Model:</label>
            <select
              id="model-select"
              value={finalSelectedModel}
              onChange={handleModelChange}
              disabled={!isConnected || finalAvailableModels.length === 0}
              className="styled-select"
            >
              {finalAvailableModels.map(m => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </div>
        </div>

        <SettingsPanel
          settings={settings}
          onSettingsChanged={(s) => setSettings(s)}
        />
      </>
    );
  };

  /** Render the content pane based on selected section */
  const renderContentPane = () => {
    switch (selectedSection) {
      case 'views':
        return renderViewsSection();
      case 'logs':
        return renderLogsSection();
      case 'files':
        return renderFilesSection();
      case 'settings':
        return renderSettingsSection();
      default:
        return null;
    }
  };

  return (
    <div className={`sidebar ${isMobile ? 'mobile' : ''} ${finalIsMobileMenuOpen ? 'open' : 'closed'} ${sidebarCollapsed ? 'collapsed' : ''}`}>
      {/* Desktop collapse button */}
      {!isMobile && (
        <button
          className="desktop-collapse-btn"
          onClick={onSidebarToggle}
          aria-label="Toggle sidebar"
        >
          {sidebarCollapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
        </button>
      )}

      {isMobile && (
        <button
          className="mobile-close-btn"
          onClick={finalOnMobileMenuToggle}
          aria-label="Close sidebar"
        >
          <X size={14} />
        </button>
      )}

      {/* Pinned global header: status dot + instance selector */}
      <div className="sidebar-pinned-header">
        <div
          className={`status-dot status-dot-floating ${isConnected ? 'connected' : 'disconnected'}`}
          aria-label={isConnected ? 'Connected' : 'Disconnected'}
          title={isConnected ? 'Connected' : 'Disconnected'}
        />
        {!sidebarCollapsed && (
          <div className="instance-selector">
            <select
              id="instance-select"
              value={selectedInstancePID || ''}
              onChange={handleInstanceChange}
              disabled={!isConnected || instances.length === 0 || isSwitchingInstance}
              className="styled-select instance-select"
              title={instances.find(i => i.pid === selectedInstancePID)?.working_dir || ''}
            >
              {instances.length === 0 && (
                <option value="">No instances</option>
              )}
              {instances.map((instance) => {
                const suffix = [
                  instance.is_host ? 'host' : '',
                  instance.is_current ? 'this' : '',
                ].filter(Boolean).join(', ');
                const name = instance.working_dir.split('/').filter(Boolean).slice(-2).join('/');
                const label = suffix
                  ? `${name} (${suffix})`
                  : `${name}`;
                const fullLabel = sidebarCollapsed
                  ? label
                  : `${name} · pid:${instance.pid}${suffix ? ` (${suffix})` : ''}`;
                return (
                  <option key={instance.id} value={instance.pid}>
                    {fullLabel}
                  </option>
                );
              })}
            </select>
          </div>
        )}
        {isSwitchingInstance && (
          <div className="instance-switch-status">Switching UI host...</div>
        )}
        {instanceSwitchError && (
          <div className="instance-switch-error">{instanceSwitchError}</div>
        )}
      </div>

      {/* Icon rail (always visible) + Content pane (only when expanded) */}
      <div className="sidebar-body">
        {/* Icon Rail */}
        <div className="sidebar-icon-rail" role="tablist" aria-orientation="vertical">
          {SECTION_TABS.map((tab) => (
            <button
              key={tab.id}
              role="tab"
              aria-selected={selectedSection === tab.id}
              aria-controls="sidebar-tabpanel"
              className={`rail-icon ${selectedSection === tab.id ? 'active' : ''}`}
              onClick={() => handleSectionTabClick(tab.id)}
              title={tab.label}
              aria-label={tab.label}
            >
              <tab.icon size={18} strokeWidth={1.5} />
            </button>
          ))}
        </div>

        {/* Content Pane (only when expanded) */}
        {!sidebarCollapsed && (
          <div className="sidebar-content-pane" role="tabpanel" id="sidebar-tabpanel">
            <div className="content-pane-header">
              <span className="content-pane-title">
                {SECTION_TABS.find(t => t.id === selectedSection)?.label || ''}
              </span>
            </div>
            <div className="content-pane-scroll">
              {renderContentPane()}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default Sidebar;
