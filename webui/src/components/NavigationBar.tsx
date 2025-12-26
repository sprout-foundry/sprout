import React from 'react';
import './NavigationBar.css';

type ViewType = 'chat' | 'editor' | 'git' | 'logs';

interface NavigationBarProps {
  currentView: ViewType;
  onViewChange: (view: ViewType) => void;
}

const NavigationBar: React.FC<NavigationBarProps> = ({
  currentView,
  onViewChange
}) => {
  const tabs = [
    { id: 'chat' as ViewType, icon: '💬', label: 'Chat' },
    { id: 'editor' as ViewType, icon: '📝', label: 'Editor' },
    { id: 'git' as ViewType, icon: '🔀', label: 'Git' },
    { id: 'logs' as ViewType, icon: '📋', label: 'Logs' }
  ];

  return (
    <div className="navigation-bar">
      <div className="nav-tabs">
        <div className="app-logo">🤖 ledit</div>
        <div className="tab-container">
          {tabs.map(tab => (
            <button
              key={tab.id}
              className={`nav-tab ${currentView === tab.id ? 'active' : ''}`}
              onClick={() => onViewChange(tab.id)}
              aria-label={`${tab.label} view`}
            >
              <span className="tab-icon">{tab.icon}</span>
              <span className="tab-label">{tab.label}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};

export default NavigationBar;
