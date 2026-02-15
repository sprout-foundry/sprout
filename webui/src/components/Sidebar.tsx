import React, { useState, useEffect } from 'react';
import './Sidebar.css';
import FileTree from './FileTree';

// FileInfo interface (matching FileTree component)
interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
  children?: FileInfo[];
}

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
  // Props for FileTree when in editor view
  onFileSelect?: (file: FileInfo) => void;
  selectedFile?: string;
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
  // Props for FileTree when in editor view
  onFileSelect,
  selectedFile,
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

          {/* Context-Aware Content Section */}
          {currentView === 'editor' ? (
            /* Editor View - Show File Tree */
            <div className="context-content">
              <FileTree
                onFileSelect={onFileSelect || (() => {})}
                selectedFile={selectedFile}
              />
            </div>
          ) : currentView === 'chat' ? (
            /* Chat View - Show Chat-specific content */
            <div className="context-content">
              {/* Chat Stats */}
              <div className="stats">
                <h4>💬 Chat Stats</h4>
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

              {/* Recent Files in Chat */}
              <div className="section">
                <h4>📁 Recent Files ({finalRecentFiles.length})</h4>
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

              {/* Chat Logs */}
              <div className="section">
                <h4>📋 Chat Activity</h4>
                <div className="logs-list">
                  {finalRecentLogs.length === 0 ? (
                    <span className="empty">No activity yet</span>
                  ) : (
                    (finalRecentLogs as any[])
                      .filter((log: any) => {
                        // Filter out webpack dev server events
                        let parsedLog = log;

                        // If log is a string, try to parse it as JSON
                        if (typeof log === 'string') {
                          try {
                            parsedLog = JSON.parse(log);
                          } catch {
                            // Not valid JSON, keep as is
                            return true;
                          }
                        }

                        // Check if it's a webpack event
                        if (parsedLog && typeof parsedLog === 'object' && parsedLog.type) {
                          const webpackEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'invalid', 'warnings', 'errors', 'still-ok'];
                          return !webpackEvents.includes(parsedLog.type);
                        }
                        return true;
                      })
                      .slice(-5)
                      .map((originalLog: any, index: number) => {
                        // Parse log if it's a string
                        let log = originalLog;
                        if (typeof originalLog === 'string') {
                          try {
                            log = JSON.parse(originalLog);
                          } catch {
                            // Not valid JSON, display as is
                            return (
                              <div key={index} className="log-item">
                                {originalLog}
                              </div>
                            );
                          }
                        }

                        // At this point, log should be an object
                        const getLogIcon = (level: string) => {
                          switch (level) {
                            case 'success': return '✅';
                            case 'error': return '❌';
                            case 'warning': return '⚠️';
                            case 'info': return 'ℹ️';
                            default: return '📝';
                          }
                        };

                        const getLogSummary = (logEntry: any) => {
                          try {
                            switch (logEntry.type) {
                              case 'query_started':
                                return `Query: ${logEntry.data?.query?.substring(0, 30) || 'No query'}...`;
                              case 'tool_execution':
                                return `${logEntry.data?.tool || 'Unknown'}: ${logEntry.data?.status || 'Unknown'}`;
                              case 'file_changed':
                                return `File: ${logEntry.data?.path?.split('/').pop() || 'Unknown'}`;
                              case 'stream_chunk':
                                return `Stream: ${logEntry.data?.chunk?.substring(0, 30) || 'No chunk'}...`;
                              case 'error':
                                return `Error: ${logEntry.data?.message?.substring(0, 30) || 'Unknown error'}...`;
                              case 'connection_status':
                                return logEntry.data?.connected ? 'Connected' : 'Disconnected';
                              default:
                                return `${logEntry.type}`;
                            }
                          } catch {
                            return `${logEntry.type}`;
                          }
                        };

                        return (
                          <div key={log.id || index} className="log-item">
                            <span className="log-icon">{getLogIcon(log.level)}</span>
                            <span className="log-text">{getLogSummary(log)}</span>
                          </div>
                        );
                      })
                  )}
                </div>
              </div>
            </div>
          ) : currentView === 'logs' ? (
            /* Logs View - Show detailed logs */
            <div className="context-content">
              <div className="section">
                <h4>📋 System Logs</h4>
                <div className="logs-list logs-expanded">
                  {finalRecentLogs.length === 0 ? (
                    <span className="empty">No logs yet</span>
                  ) : (
                    (finalRecentLogs as any[])
                      .filter((log: any) => {
                        // Filter out webpack dev server events
                        let logObj = log;

                        // If log is a string, try to parse it as JSON
                        if (typeof log === 'string') {
                          try {
                            logObj = JSON.parse(log);
                          } catch {
                            // Not valid JSON, keep as is
                            return true;
                          }
                        }

                        // Check if it's a webpack event
                        if (logObj && typeof logObj === 'object' && logObj.type) {
                          const webpackEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'invalid', 'warnings', 'errors', 'still-ok'];
                          return !webpackEvents.includes(logObj.type);
                        }
                        return true;
                      })
                      .slice(-10)
                      .map((log: any, index: number) => {
                      // Handle both string and LogEntry formats
                      let parsedLog = log;
                      let isString = false;

                      if (typeof log === 'string') {
                        try {
                          parsedLog = JSON.parse(log);
                        } catch {
                          // Not valid JSON, display as is
                          isString = true;
                        }
                      }

                      if (isString) {
                        return (
                          <div key={index} className="log-item">
                            {log}
                          </div>
                        );
                      } else {
                        // New LogEntry format
                        const getLogIcon = (level: string) => {
                          switch (level) {
                            case 'success': return '✅';
                            case 'error': return '❌';
                            case 'warning': return '⚠️';
                            case 'info': return 'ℹ️';
                            default: return '📝';
                          }
                        };

                        const getLogSummary = (logEntry: any) => {
                          try {
                            switch (logEntry.type) {
                              case 'query_started':
                                return `Query: ${logEntry.data?.query?.substring(0, 50) || 'No query'}...`;
                              case 'tool_execution':
                                return `${logEntry.data?.tool || 'Unknown'}: ${logEntry.data?.status || 'Unknown'}`;
                              case 'file_changed':
                                return `File: ${logEntry.data?.path?.split('/').pop() || 'Unknown'}`;
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

                        return (
                          <div key={parsedLog.id || index} className="log-item">
                            <span className="log-icon">{getLogIcon(parsedLog.level)}</span>
                            <span className="log-text">{getLogSummary(parsedLog)}</span>
                          </div>
                        );
                      }
                    })
                  )}
                </div>
              </div>
            </div>
          ) : (
            /* Git View - Show git-related content */
            <div className="context-content">
              <div className="section">
                <h4>🔀 Git Status</h4>
                <div className="files-list">
                  <span className="empty">Git functionality coming soon</span>
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default Sidebar;