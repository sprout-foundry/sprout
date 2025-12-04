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
  provider: string;
  model: string;
  queryCount: number;
  logs: string[];
  files: Array<{ path: string; modified: boolean }>;
  onProviderChange?: (provider: string) => void;
  onModelChange?: (model: string) => void;
}

const Sidebar: React.FC<SidebarProps> = ({
  isConnected,
  provider,
  model,
  queryCount,
  logs,
  files,
  onProviderChange,
  onModelChange
}) => {
  const [selectedProvider, setSelectedProvider] = useState(provider);
  const [selectedModel, setSelectedModel] = useState(model);
  const [availableModels, setAvailableModels] = useState<string[]>([]);

  // Update available models when provider changes
  useEffect(() => {
    const providerData = PROVIDERS.find(p => p.id === selectedProvider);
    if (providerData) {
      setAvailableModels(providerData.models);
      // Reset model if current model is not in the new provider's models
      if (!providerData.models.includes(selectedModel)) {
        const newModel = providerData.models[0];
        setSelectedModel(newModel);
        onModelChange?.(newModel);
      }
    }
  }, [selectedProvider, selectedModel, onModelChange]);

  // Update local state when props change
  useEffect(() => {
    setSelectedProvider(provider);
  }, [provider]);

  useEffect(() => {
    setSelectedModel(model);
  }, [model]);

  const handleProviderChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newProvider = e.target.value;
    setSelectedProvider(newProvider);
    onProviderChange?.(newProvider);
  };

  const handleModelChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newModel = e.target.value;
    setSelectedModel(newModel);
    onModelChange?.(newModel);
  };

  const getProviderDisplayName = (providerId: string) => {
    const providerData = PROVIDERS.find(p => p.id === providerId);
    return providerData ? providerData.name : providerId;
  };

  return (
    <div className="sidebar">
      <div className="sidebar-header">
        <h3>🤖 ledit Web UI</h3>
        <div className="connection-indicator">
          <div className={`status-dot ${isConnected ? 'connected' : 'disconnected'}`}></div>
          <span className="status-text">
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>

      {/* Configuration Section */}
      <div className="config-section">
        <h4>⚙️ Configuration</h4>
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
            value={selectedModel}
            onChange={handleModelChange}
            disabled={!isConnected || availableModels.length === 0}
            className="styled-select"
          >
            {availableModels.map(m => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>
        </div>
      </div>

      {/* Stats Section */}
      <div className="stats">
        <h4>📊 Statistics</h4>
        <div className="stat-item">
          <span className="label">Provider:</span>
          <span className="value">{getProviderDisplayName(selectedProvider)}</span>
        </div>
        <div className="stat-item">
          <span className="label">Model:</span>
          <span className="value">{selectedModel}</span>
        </div>
        <div className="stat-item">
          <span className="label">Queries:</span>
          <span className="value query-count">{queryCount}</span>
        </div>
        <div className="stat-item">
          <span className="label">Status:</span>
          <span className={`value status ${isConnected ? 'connected' : 'disconnected'}`}>
            {isConnected ? '🟢 Online' : '🔴 Offline'}
          </span>
        </div>
      </div>

      {/* Files Section */}
      <div className="section">
        <h4>📁 Files ({files.length})</h4>
        <div className="files-list">
          {files.length === 0 ? (
            <span className="empty">No files tracked yet</span>
          ) : (
            files.map((file, index) => (
              <div key={index} className="file-item">
                <span className={`file-path ${file.modified ? 'modified' : ''}`}>
                  {file.path}
                </span>
                {file.modified && <span className="badge">Modified</span>}
              </div>
            ))
          )}
        </div>
      </div>

      {/* Logs Section */}
      <div className="section">
        <h4>📋 Recent Logs</h4>
        <div className="logs-list">
          {logs.length === 0 ? (
            <span className="empty">No logs yet</span>
          ) : (
            logs.slice(-5).map((log, index) => (
              <div key={index} className="log-item">
                {log}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
};

export default Sidebar;