import React from 'react';
import './Sidebar.css';

interface SidebarProps {
  isConnected: boolean;
  provider: string;
  model: string;
  queryCount: number;
  logs: string[];
  files: Array<{ path: string; modified: boolean }>;
}

const Sidebar: React.FC<SidebarProps> = ({
  isConnected,
  provider,
  model,
  queryCount,
  logs,
  files
}) => {
  return (
    <div className="sidebar">
      <h3>🤖 ledit Web UI</h3>

      <div className="stats">
        <div className="stat-item">
          <span className="label">Connection:</span>
          <span className={`value ${isConnected ? 'connected' : 'disconnected'}`}>
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
        <div className="stat-item">
          <span className="label">Provider:</span>
          <span className="value">{provider}</span>
        </div>
        <div className="stat-item">
          <span className="label">Model:</span>
          <span className="value">{model}</span>
        </div>
        <div className="stat-item">
          <span className="label">Queries:</span>
          <span className="value">{queryCount}</span>
        </div>
      </div>

      <div className="status">
        {isConnected ? (
          <span className="status-connected">🟢 Connected to ledit server</span>
        ) : (
          <span className="status-disconnected">🔴 Disconnected from ledit server</span>
        )}
      </div>

      <div className="section">
        <h4>📁 Files</h4>
        <div className="files-list">
          {files.length === 0 ? (
            <span className="empty">No files tracked yet</span>
          ) : (
            files.map((file, index) => (
              <div key={index} className="file-item">
                <span className={file.modified ? 'modified' : ''}>
                  {file.path}
                </span>
                {file.modified && <span className="badge">modified</span>}
              </div>
            ))
          )}
        </div>
      </div>

      <div className="section">
        <h4>📋 Logs</h4>
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