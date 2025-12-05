import React, { useState, useEffect, useRef } from 'react';
import './Sidebar.css';

const Sidebar = ({ 
  isConnected, 
  selectedModel, 
  onModelChange, 
  availableModels, 
  currentView, 
  onViewChange,
  stats,
  recentFiles,
  recentLogs,
  isMobileMenuOpen,
  onMobileMenuToggle
}) => {
  const [isMobile, setIsMobile] = useState(false);
  const sidebarRef = useRef(null);

  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    
    checkMobile();
    window.addEventListener('resize', checkMobile);
    
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (isMobile && isMobileMenuOpen && sidebarRef.current && !sidebarRef.current.contains(event.target)) {
        onMobileMenuToggle();
      }
    };

    if (isMobile && isMobileMenuOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      document.body.style.overflow = 'hidden';
    } else {
      document.body.style.overflow = 'unset';
    }

    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.body.style.overflow = 'unset';
    };
  }, [isMobile, isMobileMenuOpen, onMobileMenuToggle]);

  const sidebarClasses = `sidebar ${isMobileMenuOpen ? 'open' : ''} ${isMobile ? 'mobile' : ''}`;

  return (
    <>
      {isMobile && isMobileMenuOpen && (
        <div className="mobile-overlay" onClick={onMobileMenuToggle} />
      )}
      <div ref={sidebarRef} className={sidebarClasses}>
        {isMobile && (
          <button 
            className="mobile-close-btn" 
            onClick={onMobileMenuToggle}
            aria-label="Close sidebar"
          >
            ×
          </button>
        )}
        
        <div className="sidebar-header">
          <h3>Ledit Control Panel</h3>
          <div className="connection-indicator">
            <div className={`status-dot ${isConnected ? 'connected' : 'disconnected'}`}></div>
            <span className="status-text">{isConnected ? 'Connected' : 'Disconnected'}</span>
          </div>
        </div>

        <div className="config-section">
          <div className="config-item">
            <label htmlFor="model-select">AI Model</label>
            <select 
              id="model-select"
              className="styled-select" 
              value={selectedModel} 
              onChange={(e) => onModelChange(e.target.value)}
              disabled={!isConnected}
            >
              {availableModels.map(model => (
                <option key={model} value={model}>{model}</option>
              ))}
            </select>
          </div>
        </div>

        <div className="view-section">
          <h4>View Mode</h4>
          <div className="view-switcher">
            <button 
              className={`view-button ${currentView === 'split' ? 'active' : ''}`}
              onClick={() => onViewChange('split')}
              disabled={!isConnected}
            >
              <span>⚖️</span> Split
            </button>
            <button 
              className={`view-button ${currentView === 'editor' ? 'active' : ''}`}
              onClick={() => onViewChange('editor')}
              disabled={!isConnected}
            >
              <span>📝</span> Editor
            </button>
            <button 
              className={`view-button ${currentView === 'chat' ? 'active' : ''}`}
              onClick={() => onViewChange('chat')}
              disabled={!isConnected}
            >
              <span>💬</span> Chat
            </button>
          </div>
        </div>

        <div className="stats">
          <h4>Session Stats</h4>
          <div className="stat-item">
            <span className="label">Queries:</span>
            <span className="value query-count">{stats.queryCount}</span>
          </div>
          <div className="stat-item">
            <span className="label">Files Modified:</span>
            <span className="value">{stats.filesModified}</span>
          </div>
          <div className="stat-item">
            <span className="label">Status:</span>
            <span className={`value status ${isConnected ? 'connected' : 'disconnected'}`}>
              {isConnected ? 'Active' : 'Inactive'}
            </span>
          </div>
        </div>

        <div className="section">
          <h4>Recent Files</h4>
          <div className="files-list">
            {recentFiles.length > 0 ? (
              recentFiles.map((file, index) => (
                <div key={index} className="file-item">
                  <span className={`file-path ${file.modified ? 'modified' : ''}`}>
                    {file.path}
                  </span>
                  {file.modified && <span className="badge">Modified</span>}
                </div>
              ))
            ) : (
              <div className="empty">No recent files</div>
            )}
          </div>
        </div>

        <div className="section">
          <h4>Activity Log</h4>
          <div className="logs-list">
            {recentLogs.length > 0 ? (
              recentLogs.map((log, index) => (
                <div key={index} className="log-item">{log}</div>
              ))
            ) : (
              <div className="empty">No recent activity</div>
            )}
          </div>
        </div>
      </div>
    </>
  );
};

export default Sidebar;