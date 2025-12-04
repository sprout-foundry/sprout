import React, { useState, useEffect } from 'react';
import './Sidebar.css';

// Provider and model options
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
  currentView?: 'chat' | 'editor' | 'git';
  onViewChange?: (view: 'chat' | 'editor' | 'git') => void;
  stats?: {
    queryCount: number;
    filesModified: number;
  };
  recentFiles?: Array<{ path: string; modified: boolean }>;
  recentLogs?: string[];
  isMobileMenuOpen?: boolean;
  onMobileMenuToggle?: () => void;
  sidebarCollapsed?: boolean;
  onSidebarToggle?: () => void;
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

const Sidebar: React.FC<SidebarProps> = ({
  isConnected,
  selectedModel,
  onModelChange,
  availableModels,
  currentView = 'chat',
  onViewChange,
  stats,
  recentFiles,
  recentLogs,
  isMobileMenuOpen,
  onMobileMenuToggle,
  sidebarCollapsed,
  onSidebarToggle,
  // Legacy props for backward compatibility
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
  const [availableModelsState, setAvailableModelsState] = useState<string[]>(availableModels || []);

  // Use new props if available, otherwise fall back to legacy props
  const finalSelectedModel = selectedModel || selectedModelState;
  const finalAvailableModels = availableModels || availableModelsState;
  const finalStats = stats || { queryCount: queryCount || 0, filesModified: files?.filter(f => f.modified).length || 0 };
  const finalRecentFiles = recentFiles || files || [];
  const finalRecentLogs = recentLogs || logs || [];
  const finalIsMobileMenuOpen = isMobileMenuOpen !== undefined ? isMobileMenuOpen : isOpen;
  const finalOnMobileMenuToggle = onMobileMenuToggle || onClose;

  // Update available models when provider changes
  useEffect(() => {
    const providerData = PROVIDERS.find(p => p.id === selectedProvider);
    if (providerData) {
      setAvailableModelsState(providerData.models);
      // Reset model if current model is not in the new provider's models
      if (!providerData.models.includes(finalSelectedModel)) {
        const newModel = providerData.models[0];
        setSelectedModelState(newModel);
        onModelChange?.(newModel);
      }
    }
  }, [selectedProvider, finalSelectedModel, onModelChange]);

  // Update local state when props change
  useEffect(() => {
    if (provider) setSelectedProvider(provider);
  }, [provider]);

  useEffect(() => {
    if (model) setSelectedModelState(model);
  }, [model]);

  const handleProviderChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newProvider = e.target.value;
    setSelectedProvider(newProvider);
    onProviderChange?.(newProvider);
  };

  const handleModelChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newModel = e.target.value;
    setSelectedModelState(newModel);
    onModelChange?.(newModel);
  };

  const getProviderDisplayName = (providerId: string) => {
    const providerData = PROVIDERS.find(p => p.id === providerId);
    return providerData ? providerData.name : providerId;
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
            <h3>🤖 ledit</h3>
            <div className="connection-indicator">
              <div className={`status-dot ${isConnected ? 'connected' : 'disconnected'}`}></div>
              <span className="status-text">
                {isConnected ? 'Connected' : 'Disconnected'}
              </span>
            </div>
          </div>

          {/* Main Navigation - 3 Icon View Switcher */}
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
                disabled={!isConnected}
                className="styled-select"
              >
                {PROVIDERS.map(p => (
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

          {/* Stats Section - Simplified for mobile */}
          <div className="stats">
            <h4>📊 Stats</h4>
            <div className="stat-item">
              <span className="label">Queries:</span>
              <span className="value query-count">{finalStats.queryCount}</span>
            </div>
            <div className="stat-item">
              <span className="label">Status:</span>
              <span className={`value status ${isConnected ? 'connected' : 'disconnected'}`}>
                {isConnected ? '🟢' : '🔴'}
              </span>
            </div>
          </div>

          {/* Files Section - Collapsible on mobile */}
          <div className="section">
            <h4>📁 Files ({finalRecentFiles.length})</h4>
            <div className="files-list">
              {finalRecentFiles.length === 0 ? (
                <span className="empty">No files</span>
              ) : (
                finalRecentFiles.slice(isMobile ? 3 : 5).map((file, index) => (
                  <div key={index} className="file-item">
                    <span className={`file-path ${file.modified ? 'modified' : ''}`}>
                      {file.path.split('/').pop()}
                    </span>
                    {file.modified && <span className="badge">✓</span>}
                  </div>
                ))
              )}
            </div>
          </div>

          {/* Logs Section - Hidden on mobile to save space */}
          {!isMobile && (
            <div className="section">
              <h4>📋 Recent Logs</h4>
              <div className="logs-list">
                {finalRecentLogs.length === 0 ? (
                  <span className="empty">No logs yet</span>
                ) : (
                  finalRecentLogs.slice(-5).map((log, index) => (
                    <div key={index} className="log-item">
                      {log}
                    </div>
                  ))
                )}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default Sidebar;