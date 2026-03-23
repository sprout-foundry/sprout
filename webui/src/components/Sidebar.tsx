import React, { useEffect, useState, useMemo } from 'react';
import './Sidebar.css';
import { ApiService, ProviderOption, LeditInstance } from '../services/api';
import { viewRegistry, ProviderContext, SidebarSection, ProviderLogEntry } from '../providers';
import { useTheme } from '../contexts/ThemeContext';

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
  const { theme, setTheme } = useTheme();
  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [instances, setInstances] = useState<LeditInstance[]>([]);
  const [selectedInstancePID, setSelectedInstancePID] = useState<number>(0);
  const [isSwitchingInstance, setIsSwitchingInstance] = useState(false);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const [sectionsData, setSectionsData] = useState<Map<string, SectionData>>(new Map());
  const [refreshTrigger, setRefreshTrigger] = useState(0);
  const apiService = ApiService.getInstance();

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

  const finalRecentFiles = useMemo(() =>
    recentFiles.length > 0 ? recentFiles : (files || []),
    [recentFiles, files]
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
      recentFiles: finalRecentFiles,
      recentLogs: normalizedRecentLogs,
      stats: finalStats
    };

    viewRegistry.setContext(context);
  }, [isConnected, currentView, onFileClick, onModelChange, finalRecentFiles, normalizedRecentLogs, finalStats]);

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

  const handleInstanceChange = async (e: React.ChangeEvent<HTMLSelectElement>) => {
    const pid = Number(e.target.value);
    if (!Number.isFinite(pid) || pid <= 0 || pid === selectedInstancePID) {
      return;
    }

    setIsSwitchingInstance(true);
    try {
      await apiService.selectInstance(pid);
      setSelectedInstancePID(pid);
      setTimeout(() => {
        window.location.reload();
      }, 1200);
    } catch (error) {
      console.error('Failed to switch instance:', error);
      setIsSwitchingInstance(false);
    }
  };

  // Render sidebar content sections from providers
  const renderContentSections = () => {
    if (sectionsData.size === 0) {
      return <div className="empty">No content available</div>;
    }

    // Sort sections by order
    const sortedSections = Array.from(sectionsData.values()).sort((a, b) =>
      (a.section.order || 0) - (b.section.order || 0)
    );

    return sortedSections.map(({ section, data, loading, error }) => {
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

      return (
        <div key={section.id} className="section">
          <h4>{section.title?.(data) || section.id}</h4>
          {section.renderItem(data, context!)}
        </div>
      );
    });
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
          {sidebarCollapsed ? '→' : '←'}
        </button>
      )}

      {isMobile && (
        <button
          className="mobile-close-btn"
          onClick={finalOnMobileMenuToggle}
          aria-label="Close sidebar"
        >
          ✕
        </button>
      )}

      {!sidebarCollapsed && (
        <>
          <div className="sidebar-header">
            <div className="connection-indicator">
              <div className={`status-dot ${isConnected ? 'connected' : 'disconnected'}`}></div>
              <span className="status-text">
                {isConnected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
          </div>

          {/* Main Navigation - 4 Icon View Switcher */}
          <div className="main-nav-section">
            <div className="nav-icons">
              <button
                className={`nav-icon ${currentView === 'chat' ? 'active' : ''}`}
                onClick={() => {
                  onViewChange?.('chat');
                  if (isMobile && finalOnMobileMenuToggle) finalOnMobileMenuToggle();
                }}
                disabled={!onViewChange}
                title="Chat View"
              >
                <span className="nav-icon-label">Chat</span>
              </button>
              <button
                className={`nav-icon ${currentView === 'editor' ? 'active' : ''}`}
                onClick={() => {
                  onViewChange?.('editor');
                  if (isMobile && finalOnMobileMenuToggle) finalOnMobileMenuToggle();
                }}
                disabled={!onViewChange}
                title="Editor View"
              >
                <span className="nav-icon-label">Editor</span>
              </button>
              <button
                className={`nav-icon ${currentView === 'git' ? 'active' : ''}`}
                onClick={() => {
                  onViewChange?.('git');
                  if (isMobile && finalOnMobileMenuToggle) finalOnMobileMenuToggle();
                }}
                disabled={!onViewChange}
                title="Git View"
              >
                <span className="nav-icon-label">Git</span>
              </button>
              <button
                className={`nav-icon ${currentView === 'logs' ? 'active' : ''}`}
                onClick={() => {
                  onViewChange?.('logs');
                  if (isMobile && finalOnMobileMenuToggle) finalOnMobileMenuToggle();
                }}
                disabled={!onViewChange}
                title="Logs View"
              >
                <span className="nav-icon-label">Logs</span>
              </button>
            </div>
          </div>

          <div className="appearance-section">
            <h4>Appearance</h4>
            <div className="config-item">
              <label htmlFor="theme-select">Theme:</label>
              <select
                id="theme-select"
                value={theme}
                onChange={(e) => setTheme(e.target.value as 'dark' | 'light')}
                className="styled-select"
              >
                <option value="dark">Dark</option>
                <option value="light">Light</option>
              </select>
            </div>
          </div>

          <div className="config-section">
            <h4>Instance</h4>
            <div className="config-item">
              <label htmlFor="instance-select">Active UI Host:</label>
              <select
                id="instance-select"
                value={selectedInstancePID || ''}
                onChange={handleInstanceChange}
                disabled={!isConnected || instances.length === 0 || isSwitchingInstance}
                className="styled-select"
              >
                {instances.length === 0 && (
                  <option value="">No instances detected</option>
                )}
                {instances.map((instance) => {
                  const suffix = [
                    instance.is_host ? 'host' : '',
                    instance.is_current ? 'this' : '',
                  ].filter(Boolean).join(', ');
                  const name = instance.working_dir.split('/').filter(Boolean).slice(-2).join('/');
                  const label = suffix
                    ? `${name} · pid:${instance.pid} (${suffix})`
                    : `${name} · pid:${instance.pid}`;
                  return (
                    <option key={instance.id} value={instance.pid}>
                      {label}
                    </option>
                  );
                })}
              </select>
            </div>
          </div>

          {currentView === 'chat' && (
            <div className="config-section">
              <h4>Config</h4>
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
          )}

          {/* Context-Aware Content Section (from Data-Driven Providers) */}
          <div className="context-content">
            {renderContentSections()}
          </div>
        </>
      )}
    </div>
  );
};

export default Sidebar;
