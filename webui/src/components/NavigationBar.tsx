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
    { id: 'chat' as ViewType, label: 'Chat' },
    { id: 'editor' as ViewType, label: 'Editor' },
    { id: 'git' as ViewType, label: 'Git' },
    { id: 'logs' as ViewType, label: 'Logs' }
  ];

  return (
    <div className="navigation-bar">
      <div className="nav-tabs">
        <div className="tab-container">
          {tabs.map(tab => (
            <button
              key={tab.id}
              className={`nav-tab ${currentView === tab.id ? 'active' : ''}`}
              onClick={() => onViewChange(tab.id)}
              aria-label={`${tab.label} view`}
            >
              <span className="tab-label">{tab.label}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};

export default NavigationBar;
