import React, { useEffect, useState, useMemo } from 'react';
import './Sidebar.css';
import { ApiService } from '../services/api';
import { viewRegistry, ProviderContext, SidebarSection } from '../providers';

// Module-level flag to track if providers have been fetched
let providersFetched = false;

// Provider and model options (kept for configuration section)
const PROVIDERS = [
  { id: 'openai', name: 'OpenAI', models: ['gpt-4', 'gpt-4-turbo', 'gpt-3.5-turbo'] },
  { id: 'anthropic', name: 'Anthropic', models: ['claude-3-sonnet', 'claude-3-haiku'] },
  { id: 'ollama', name: 'Ollama', models: ['llama2', 'codellama', 'mistral'] },
  { id: 'deepinfra', name: 'DeepInfra', models: ['mistralai/Mixtral-8x7B-Instruct-v0.1'] },
  { id: 'cerebras', name: 'Cerebras', models: ['llama3.1-70b', 'llama3.1-8b'] }
];

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
  const [selectedProvider, setSelectedProvider] = useState(provider || 'openai');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || 'gpt-4');
  const [providers, setProviders] = useState(PROVIDERS);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const [sectionsData, setSectionsData] = useState<Map<string, SectionData>>(new Map());
  const [refreshTrigger, setRefreshTrigger] = useState(0);
  const apiService = ApiService.getInstance();

  const finalSelectedModel = selectedModel || selectedModelState;
  // Compute available models from providers and selectedProvider
  const availableModelsState = useMemo(() => {
    const providerData = providers.find(p => p.id === selectedProvider);
    return providerData?.models || [];
  }, [providers, selectedProvider]);
  const finalAvailableModels = availableModels || availableModelsState;

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
      recentLogs: finalRecentLogs,
      stats: finalStats
    };

    viewRegistry.setContext(context);
  }, [isConnected, currentView, onFileClick, onModelChange, finalRecentFiles, finalRecentLogs, finalStats]);

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

    // Load data for each section
    sections.forEach(async (section) => {
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

        setSectionsData(prev => {
          const updated = new Map(prev);
          updated.set(section.id, { section, data, loading: false, error: null });
          return updated;
        });
      } catch (error) {
        console.error(`Failed to load data for section ${section.id}:`, error);
        setSectionsData(prev => {
          const updated = new Map(prev);
          updated.set(section.id, { section, data: null, loading: false, error: 'Failed to load' });
          return updated;
        });
      }
    });
  }, [currentView, isConnected, refreshTrigger]);

  // Fetch providers from API - only run once on mount, not on reconnect
  useEffect(() => {
    // Check localStorage to prevent multiple fetches across remounts
    const storedProviders = localStorage.getItem('ledit_providers');
    if (storedProviders) {
      try {
        const parsed = JSON.parse(storedProviders);
        setProviders(parsed);
        setIsLoadingProviders(false);
        return;
      } catch {
        localStorage.removeItem('ledit_providers');
      }
    }

    if (providersFetched) return;
    providersFetched = true;

    const fetchProviders = async () => {
      setIsLoadingProviders(true);
      try {
        const data = await apiService.getProviders();
        if (data.providers && data.providers.length > 0) {
          setProviders(data.providers);
          localStorage.setItem('ledit_providers', JSON.stringify(data.providers));
        } else {
          setProviders(PROVIDERS);
        }
      } catch (error) {
        console.error('Failed to fetch providers, using defaults:', error);
        setProviders(PROVIDERS);
      } finally {
        setIsLoadingProviders(false);
      }
    };

    fetchProviders();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Run ONCE on mount, never again

  // Update local state when props change (only if provider exists in fetched list)
  useEffect(() => {
    if (provider && provider !== 'unknown') {
      // Only sync if provider exists in our fetched providers list
      const providerExists = providers.some(p => p.id === provider);
      if (providerExists) {
        setSelectedProvider(provider);
      }
    }
  }, [provider, providers]);

  useEffect(() => {
    if (model && model !== 'unknown') {
      // Only sync if the model exists in the current provider's model list
      const currentProviderData = providers.find(p => p.id === selectedProvider);
      if (currentProviderData && currentProviderData.models.includes(model)) {
        setSelectedModelState(model);
      }
    }
  }, [model, providers, selectedProvider]);

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
                <span className="nav-icon-emoji">💬</span>
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
                <span className="nav-icon-emoji">📝</span>
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
                <span className="nav-icon-emoji">🔀</span>
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
                <span className="nav-icon-emoji">📋</span>
                <span className="nav-icon-label">Logs</span>
              </button>
            </div>
          </div>

          {/* Configuration Section */}
          <div className="config-section">
            <h4>⚙️ Config</h4>
            <div className="config-item">
              <label htmlFor="provider-select">Provider:</label>
              <select
                id="provider-select"
                value={selectedProvider}
                onChange={handleProviderChange}
                disabled={!isConnected || isLoadingProviders}
                className="styled-select"
              >
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
